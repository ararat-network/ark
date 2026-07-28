# Treasury Liability Preblock Priming And Flat Metering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make swap-time liability valuation cost a flat, position-independent gas charge by computing the aggregate liability snapshot once per block in the preblocker, so user fees stop scaling with the oracle whitelist size.

**Architecture:** The treasury keeper gains `PrimeLiabilitySnapshot`, called by `abci/preblock` immediately after oracle price application and vote-target advancement (rates and the tobin list are final for the block at that point). Transaction-time `cachedLiabilityValue` becomes a thin metering wrapper: it charges a flat 2,000 gas, then runs the existing logic against a free (infinite) gas meter. A new block-scoped transient marker records an incomplete valuation so no transaction re-attempts the scan within the block — rates are fixed at preblock, so an intra-block retry cannot succeed. The lazy scan path is kept as a fallback (it is what keeper unit tests exercise, and it self-marks on incompleteness).

**Tech Stack:** Go, cosmos-sdk v0.54 fork (store/v2 types), gomock (`go.uber.org/mock`), testify suite.

**Why (measured baseline, 2026-07-28):** DrawRedemptionBuffer's liability portion costs 68,441 gas on a recompute vs 4,814 on a snapshot hit at 26 denoms (24,851 vs 3,137 at the 8 default denoms). Because gas simulation runs on `checkState`, whose transient store is always empty, every user is quoted — and, under charge-on-declared-limit with no refund, pays — the recompute price, which grows ~2.3k gas per listed denom. This plan moves the scan to unmetered preblock work (~30–80µs) and charges the honest marginal cost.

**Out of scope:** `RecordSupplyChange` metering (stays standard — it is real per-swap work), the `rates` map mutation cleanup and only-Market-mints-stables invariant comment (tracked separately from the original review), FundStatus query metering (queries must stay normally metered so node query gas limits keep bounding work).

---

## Invariants this plan changes

`docs/TREASURY_REDESIGN_PLAN.md` §13.6 currently states: *"Complete aggregate liability is scanned at most once per block after the first successful valuation. Its transient snapshot tracks every later successful Market stable burn/mint; incomplete valuations are not cached and may retry."*

After this plan: the scan happens exactly once per block in the preblocker; incomplete valuations ARE cached (as a block-scoped unavailability marker) and do NOT retry within the block. Task 6 rewrites the doc. The mid-block edge case: a governance proposal changing the tobin whitelist mid-block could in principle flip an incomplete valuation to complete; the marker keeps the block conservatively degraded and the next block's preblock heals it. This is deliberate.

## File Structure

- `x/treasury/keeper/liability.go` — marker keys/helpers, `PrimeLiabilitySnapshot`, flat-metering wrapper (modify)
- `x/treasury/keeper/liability_test.go` — rewritten incomplete-valuation test, priming tests, gas-equality test (modify)
- `x/treasury/keeper/liability_benchmark_test.go` — ns benchmarks for prime / hit / record paths (create)
- `abci/types/interfaces.go` — `TreasuryKeeper` interface (modify)
- `abci/types/errors.go` — `ErrTreasuryKeeper` (modify)
- `abci/testutil/interfaces_mocks.go` — regenerated (generated)
- `abci/preblock/preblock.go` — handler field + priming call (modify)
- `abci/preblock/preblock_test.go` — new-arg updates + two new tests (modify)
- `abci/preblock/README.md` — document the new step (modify)
- `app/oracle.go:56-59` — pass `app.TreasuryKeeper` (modify)
- `docs/TREASURY_REDESIGN_PLAN.md` §13.6 — invariant rewrite (modify)

---

### Task 1: Block-scoped unavailability marker

An incomplete lazy valuation currently returns `(zero, false, nil)` and the next caller rescans. Make incompleteness sticky for the block via a second transient key.

**Files:**
- Modify: `x/treasury/keeper/liability.go`
- Modify: `x/treasury/keeper/liability_test.go` (rewrite `TestLiabilitySnapshotDoesNotCacheIncompleteValuation`)

- [ ] **Step 1: Rewrite the incomplete-valuation test to expect single-scan behavior**

Replace the whole `TestLiabilitySnapshotDoesNotCacheIncompleteValuation` function in `x/treasury/keeper/liability_test.go` with:

```go
func (s *KeeperTestSuite) TestLiabilityIncompleteValuationMarksBlockUnavailable() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.USDBaseDenom},
		{Denom: chain.KRWBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.KRWBaseDenom).
		Return(nil, oracletypes.ErrStaleExchangeRate).Times(1)

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	for range 2 {
		draw, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		s.Require().False(draw.ValuationComplete)
		s.Require().True(draw.BufferPaid.IsZero())
	}

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
		rates,
	))

	// Key 0x01 is the liability snapshot, key 0x02 the unavailability marker
	// (mirrors the unexported keys in liability.go).
	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	snapshot, err := transientStore.Get([]byte{0x01})
	s.Require().NoError(err)
	s.Require().Nil(snapshot)
	marker, err := transientStore.Get([]byte{0x02})
	s.Require().NoError(err)
	s.Require().NotNil(marker)
}
```

The load-bearing change from the old test: `GetSupply` and `GetRateSet` are `Times(1)` — the second draw must NOT rescan.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./x/treasury/keeper/ -run 'TestKeeperTestSuite/TestLiabilityIncompleteValuationMarksBlockUnavailable' -v`
Expected: FAIL — gomock reports `GetSupply` / `GetRateSet` called more than expected `Times(1)` (second draw rescans), and the 0x02 marker assertion fails.

- [ ] **Step 3: Add the marker key and helpers, and mark on incomplete lazy valuation**

In `x/treasury/keeper/liability.go`, replace the key var (line 17) with:

```go
var (
	liabilitySnapshotKey = []byte{0x01}
	// liabilityUnavailableKey marks the block as having an incomplete liability
	// valuation. Rates are fixed at preblock, so retrying within the block
	// cannot succeed; the marker suppresses repeat scans until the transient
	// store resets at commit.
	liabilityUnavailableKey = []byte{0x02}
)
```

Append these helpers at the bottom of the file:

```go
func (k Keeper) markLiabilityUnavailable(ctx context.Context) error {
	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilityUnavailableKey, []byte{0x01}); err != nil {
		return fmt.Errorf("marking liability valuation unavailable: %w", err)
	}
	return nil
}

func (k Keeper) liabilityUnavailable(ctx context.Context) (bool, error) {
	store := k.transientStoreService.OpenTransientStore(ctx)
	bz, err := store.Get(liabilityUnavailableKey)
	if err != nil {
		return false, fmt.Errorf("getting liability unavailability marker: %w", err)
	}
	return bz != nil, nil
}
```

Replace the body of `cachedLiabilityValue` (currently lines 120–144) with:

```go
// cachedLiabilityValue returns the current block's aggregate stable liability.
// The preblocker primes it; the lazy scan below is the fallback. An incomplete
// valuation marks the whole block unavailable instead of retrying, because
// oracle rates cannot change until the next block's preblock. The snapshot is
// checked before the marker: the two keys are mutually exclusive by
// construction, and this order keeps the common primed path at a single
// transient read.
func (k Keeper) cachedLiabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	liability, found, err := k.loadLiabilitySnapshot(ctx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if found {
		return liability, true, nil
	}

	unavailable, err := k.liabilityUnavailable(ctx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if unavailable {
		return math.LegacyZeroDec(), false, nil
	}

	liability, complete, err := k.nominalLiabilityValue(ctx, tobinTaxes, rates)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if !complete {
		if err := k.markLiabilityUnavailable(ctx); err != nil {
			return math.LegacyDec{}, false, err
		}
		return liability, false, nil
	}
	if err := k.storeLiabilitySnapshot(ctx, liability); err != nil {
		return math.LegacyDec{}, false, err
	}
	return liability, true, nil
}
```

- [ ] **Step 4: Run the test to verify it passes, then the treasury package**

Run: `go test ./x/treasury/keeper/ -run 'TestKeeperTestSuite/TestLiabilityIncompleteValuationMarksBlockUnavailable' -v`
Expected: PASS
Run: `go test ./x/treasury/...`
Expected: PASS (no other test asserts the old retry behavior; if one fails, it is asserting call counts — align it with single-scan semantics the same way as Step 1).

- [ ] **Step 5: Commit**

```bash
git add x/treasury/keeper/liability.go x/treasury/keeper/liability_test.go
git commit -m "feat(treasury): mark incomplete liability valuations for the block

An incomplete aggregate-liability valuation previously returned uncached
and every later caller in the block rescanned bank supply and oracle
rates. Rates are fixed at preblock, so those retries can never succeed
within the block. Record incompleteness in a second transient key
(0x02) and short-circuit later valuations to the degraded path. This
also makes the upcoming flat gas charge DoS-safe: transactions cannot
trigger repeated free-metered scans. The marker resets with the
transient store at commit, so availability returns next block."
```

---

### Task 2: `PrimeLiabilitySnapshot` keeper method

**Files:**
- Modify: `x/treasury/keeper/liability.go`
- Test: `x/treasury/keeper/liability_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `x/treasury/keeper/liability_test.go`:

```go
func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotStoresCompleteValuation() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.USDBaseDenom},
		{Denom: chain.KRWBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.USDBaseDenom, chain.KRWBaseDenom).
		Return(oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
			chain.KRWBaseDenom:  math.LegacyOneDec(),
		}, nil).Times(1)
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(1)

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// GetSupply/GetRateSet expectations are exhausted by priming: the draw
	// below must reuse the primed snapshot without rescanning.
	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().True(draw.ValuationComplete)
	s.Require().Equal(math.LegacyNewDec(200), draw.AggregateLiabilityNoah)
}

func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotMarksUnavailableValuation() {
	tobinTaxes := []oracletypes.TobinTax{{Denom: chain.USDBaseDenom}}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.USDBaseDenom).
		Return(nil, oracletypes.ErrStaleExchangeRate).Times(1)

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// The rest of the block reuses the marker without rescanning.
	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(draw.ValuationComplete)
	s.Require().True(draw.BufferPaid.IsZero())
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./x/treasury/keeper/ -run 'TestKeeperTestSuite/TestPrimeLiabilitySnapshot' -v`
Expected: FAIL to compile — `s.keeper.PrimeLiabilitySnapshot undefined`.

- [ ] **Step 3: Implement `PrimeLiabilitySnapshot`**

Add to `x/treasury/keeper/liability.go`, directly after `RecordSupplyChange`:

```go
// PrimeLiabilitySnapshot values the aggregate stable liability once for the
// block. The preblocker calls it immediately after oracle price application
// and vote-target advancement, so transaction-time callers always find either
// the snapshot or the unavailability marker and never rescan. Hard state
// errors propagate and fail the block; an incomplete valuation (stale or
// missing rates) is an expected degraded mode and only sets the marker.
func (k Keeper) PrimeLiabilitySnapshot(ctx context.Context) error {
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return fmt.Errorf("getting Tobin taxes: %w", err)
	}
	liability, complete, err := k.nominalLiabilityValue(ctx, tobinTaxes, nil)
	if err != nil {
		return err
	}
	if !complete {
		return k.markLiabilityUnavailable(ctx)
	}
	return k.storeLiabilitySnapshot(ctx, liability)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./x/treasury/keeper/ -run 'TestKeeperTestSuite/TestPrimeLiabilitySnapshot' -v`
Expected: PASS (both tests)

- [ ] **Step 5: Commit**

```bash
git add x/treasury/keeper/liability.go x/treasury/keeper/liability_test.go
git commit -m "feat(treasury): add preblock liability snapshot priming

PrimeLiabilitySnapshot computes the block's aggregate stable liability
once, storing either the snapshot or the block-scoped unavailability
marker. It will be wired into the ABCI preblocker after oracle price
application, moving the per-block supply-and-rates scan off
transaction gas and onto unmetered block processing. The lazy scan in
cachedLiabilityValue remains as a fallback and for keeper unit tests."
```

---

### Task 3: Flat metering of transaction-time valuation

**Files:**
- Modify: `x/treasury/keeper/liability.go`
- Test: `x/treasury/keeper/liability_test.go`

- [ ] **Step 1: Write the failing gas-equality test**

Append to `x/treasury/keeper/liability_test.go`:

```go
func (s *KeeperTestSuite) TestLiabilityValuationGasIsPositionIndependent() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.USDBaseDenom},
		{Denom: chain.KRWBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	// Quote rates cover every listed denom, so the lazy scan never calls
	// GetRateSet and the map is never mutated; sharing it across draws is safe.
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.KRWBaseDenom:  math.LegacyOneDec(),
	}
	draw := func() uint64 {
		before := uint64(sdk.UnwrapSDKContext(s.ctx).GasMeter().GasConsumed())
		_, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		return uint64(sdk.UnwrapSDKContext(s.ctx).GasMeter().GasConsumed()) - before
	}

	scanGas := draw() // first call performs the full lazy scan
	hitGas := draw()  // second call reads the snapshot
	s.Require().Equal(scanGas, hitGas)
	// 2_000 mirrors liabilityValuationGas in liability.go.
	s.Require().GreaterOrEqual(hitGas, uint64(2_000))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./x/treasury/keeper/ -run 'TestKeeperTestSuite/TestLiabilityValuationGasIsPositionIndependent' -v`
Expected: FAIL on the `Equal` assertion — the scan draw meters a transient read-miss plus snapshot write, the hit draw only a transient read, so the two totals differ.

- [ ] **Step 3: Split `cachedLiabilityValue` into a flat-charging wrapper and a free-metered body**

In `x/treasury/keeper/liability.go`:

Add to the import block:

```go
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
```

Add the constant above the key vars:

```go
// liabilityValuationGas is the flat gas charged for one transaction-time
// aggregate-liability valuation, hit or miss. The preblocker performs the real
// scan as unmetered block work, so the honest marginal cost is the primed
// path's single transient snapshot read: at the current transient gas config
// (read 1,000 flat + 3 per key/value byte) that is ~1,105 gas for a typical
// ~34-byte LegacyDec value and ~1,183 gas at the maximum ~60-byte magnitude,
// so 2,000 covers the worst case with ~40% headroom. Recalibrate if the app
// ever customises store gas configs. Charging a constant keeps swap gas
// position-independent within a block and independent of the oracle whitelist
// size, and simulation (which always runs with an empty transient store)
// quotes exactly what execution consumes.
const liabilityValuationGas = 2_000
```

Rename the function produced by Task 1 from `cachedLiabilityValue` to `liabilityValue` (only the name changes; body stays exactly as Task 1 wrote it), and add the wrapper above it:

```go
// cachedLiabilityValue charges a flat fee for the block-local liability lookup
// and runs the lookup itself against a free meter. Do not free-meter the query
// path: FundStatus calls nominalLiabilityValue directly so node query gas
// limits keep bounding its work.
func (k Keeper) cachedLiabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.GasMeter().ConsumeGas(liabilityValuationGas, "treasury liability valuation")
	return k.liabilityValue(sdkCtx.WithGasMeter(storetypes.NewInfiniteGasMeter()), tobinTaxes, rates)
}
```

`RouteExpansion` and `DrawRedemptionBuffer` keep calling `cachedLiabilityValue` — no caller changes.

- [ ] **Step 4: Run the test to verify it passes, then the treasury package**

Run: `go test ./x/treasury/keeper/ -run 'TestKeeperTestSuite/TestLiabilityValuationGasIsPositionIndependent' -v`
Expected: PASS
Run: `go test ./x/treasury/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add x/treasury/keeper/liability.go x/treasury/keeper/liability_test.go
git commit -m "feat(treasury): charge flat gas for liability valuation

cachedLiabilityValue now consumes a constant 2,000 gas and evaluates
the block-local lookup against an infinite meter. With the preblocker
priming the snapshot, the true marginal cost of a swap's liability
read is the transient snapshot get; metering the internals made the
first NOAH-leg swap of each block pay the full supply-and-rates scan
(~2.3k gas per listed denom) and, because simulation never sees a
transient snapshot, made every swapper budget that worst case. The
flat charge makes quoted gas equal executed gas at any position and
keeps swap fees flat as governance grows the whitelist. The
unavailability marker from the previous commit bounds free-metered
scans to at most one per block. Query-path valuation (FundStatus)
stays normally metered."
```

---

### Task 4: Preblock wiring — interface, error, handler, mocks

**Files:**
- Modify: `abci/types/interfaces.go`
- Modify: `abci/types/errors.go`
- Modify: `abci/preblock/preblock.go`
- Modify: `abci/preblock/preblock_test.go`
- Generated: `abci/testutil/interfaces_mocks.go`

- [ ] **Step 1: Add the interface and error**

In `abci/types/interfaces.go`, after the `OracleKeeper` interface:

```go
// TreasuryKeeper exposes the treasury state primed during preblock processing.
type TreasuryKeeper interface {
	PrimeLiabilitySnapshot(ctx context.Context) error
}
```

In `abci/types/errors.go`, append to the var block:

```go
	// ErrTreasuryKeeper identifies a treasury keeper state access or mutation failure.
	ErrTreasuryKeeper = errors.New("treasury keeper error")
```

- [ ] **Step 2: Regenerate the abci mocks**

Run: `go run go.uber.org/mock/mockgen -source=abci/types/interfaces.go -package testutil -destination abci/testutil/interfaces_mocks.go`
Expected: exit 0; `git diff --stat abci/testutil/` shows `interfaces_mocks.go` gained a `MockTreasuryKeeper`.

- [ ] **Step 3: Write the failing handler tests**

Append to `abci/preblock/preblock_test.go` (it already imports `preblock`, `codec`, `abcitestutil`, `arkabcitypes`, `cometabci`, `sdk`, `gomock`, `require`, `errors`, and defines `managerWith`/`fakeModule` — reuse them):

```go
func TestWrappedPreBlockerPrimesTreasuryLiability(t *testing.T) {
	ctrl := gomock.NewController(t)
	oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	gomock.InOrder(
		oracleKeeper.EXPECT().AdvanceVoteTargets(gomock.Any()).Return(nil),
		treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(nil),
	)
	handler := preblock.NewHandler(oracleKeeper, treasuryKeeper, codec.NewVoteExtensionCodec())

	_, err := handler.WrappedPreBlocker(managerWith(&fakeModule{name: "fake"}))(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.NoError(t, err)
}

func TestWrappedPreBlockerWrapsTreasuryPrimeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().AdvanceVoteTargets(gomock.Any()).Return(nil)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	primeErr := errors.New("prime failed")
	treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(primeErr)
	handler := preblock.NewHandler(oracleKeeper, treasuryKeeper, codec.NewVoteExtensionCodec())

	_, err := handler.WrappedPreBlocker(managerWith(&fakeModule{name: "fake"}))(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.ErrorIs(t, err, arkabcitypes.ErrTreasuryKeeper)
	require.ErrorIs(t, err, primeErr)
}
```

If the existing tests construct `NewSDKContext` with different arguments for the height-1 skip-vote-extensions path, copy the argument pattern from `TestWrappedPreBlockerSkipsVoteExtensionsWithoutPreviousCommit` — the new tests must reach `AdvanceVoteTargets` without vote-extension processing.

- [ ] **Step 4: Run to verify failure**

Run: `go test ./abci/preblock/ -run 'TestWrappedPreBlockerPrimesTreasuryLiability|TestWrappedPreBlockerWrapsTreasuryPrimeError' -v`
Expected: FAIL to compile — `NewHandler` takes 2 arguments, and `MockTreasuryKeeper` is unused until the handler accepts it.

- [ ] **Step 5: Thread the treasury keeper through the handler**

In `abci/preblock/preblock.go`, update the struct and constructor:

```go
// Handler is responsible for aggregating oracle data from each
// validator and writing the oracle data into the store before any transactions
// are executed/finalised for a given block.
type Handler struct {
	// oracleKeeper provides the oracle state used during preblock processing.
	oracleKeeper arkabcitypes.OracleKeeper

	// treasuryKeeper primes block-local treasury valuations once oracle prices
	// for the block are final.
	treasuryKeeper arkabcitypes.TreasuryKeeper

	// codec owns reusable vote-extension decompression state.
	codec *codec.VoteExtensionCodec
}

// NewHandler returns a new Handler. The handler
// is responsible for writing oracle data included in vote extensions to state.
func NewHandler(
	oracleKeeper arkabcitypes.OracleKeeper,
	treasuryKeeper arkabcitypes.TreasuryKeeper,
	voteExtensionCodec *codec.VoteExtensionCodec,
) *Handler {
	return &Handler{
		oracleKeeper:   oracleKeeper,
		treasuryKeeper: treasuryKeeper,
		codec:          voteExtensionCodec,
	}
}
```

In `WrappedPreBlocker`, after the `AdvanceVoteTargets` block and before `return response, nil`:

```go
		// Prices and vote targets for the block are final here; prime the
		// treasury liability snapshot so transactions never rescan.
		if err = h.treasuryKeeper.PrimeLiabilitySnapshot(ctx); err != nil {
			return response, fmt.Errorf(
				"%w: prime liability snapshot for height %d: %w",
				arkabcitypes.ErrTreasuryKeeper,
				req.Height,
				err,
			)
		}
```

Use `err =` (not `:=`): the deferred metrics closure reads the named `err`. `preblockStatus` needs no change — `ErrTreasuryKeeper` maps to the default `StatusFailure`, matching how non-oracle failures are already reported.

- [ ] **Step 6: Update every existing `NewHandler` call site in `preblock_test.go`**

Mechanical rule for each of the existing call sites (lines 30, 45, 67, 88, 112, and any others `grep -n "preblock.NewHandler" abci/preblock/preblock_test.go` reports): insert a `MockTreasuryKeeper` as the new second argument.

- Tests that error before reaching `AdvanceVoteTargets` (nil request, module-manager error, vote-extension decode failures): pass `abcitestutil.NewMockTreasuryKeeper(ctrl)` with no expectations — the strict mock then also asserts priming is NOT reached on early failure.
- Tests that reach `AdvanceVoteTargets` successfully: additionally add `treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(nil)`.

Run the package test; any site still missing an expectation fails with gomock's "missing call" or "unexpected call" naming the exact test to fix.

- [ ] **Step 7: Run the package tests to verify they pass**

Run: `go test ./abci/preblock/ -v`
Expected: PASS (all tests, old and new)

- [ ] **Step 8: Commit**

```bash
git add abci/types/interfaces.go abci/types/errors.go abci/testutil/interfaces_mocks.go abci/preblock/preblock.go abci/preblock/preblock_test.go
git commit -m "feat(abci): prime treasury liability snapshot in preblock

The preblocker now calls Treasury's PrimeLiabilitySnapshot immediately
after oracle price application and vote-target advancement, the point
where the block's rates and tobin whitelist are final. This is the
companion to Treasury's flat-metered valuation: the per-block
supply-and-rates scan runs here as unmetered block work, so
transaction-time liability reads always hit the snapshot or the
unavailability marker. The hook stays thin per the abci layering
rules: one interface method, errors wrapped with ErrTreasuryKeeper."
```

---

### Task 5: App wiring

**Files:**
- Modify: `app/oracle.go:56-59`

- [ ] **Step 1: Pass the treasury keeper to the preblock handler**

In `app/oracle.go`, update the constructor call:

```go
	preBlockHandler := preblock.NewHandler(
		app.OracleKeeper,
		app.TreasuryKeeper,
		voteExtensionCodec,
	)
```

`app.TreasuryKeeper` is `*treasurykeeper.Keeper` (`app/app.go:98`); its value-receiver `PrimeLiabilitySnapshot` satisfies `arkabcitypes.TreasuryKeeper`.

- [ ] **Step 2: Build the repo and run the affected packages**

Run: `go build ./...`
Expected: exit 0
Run: `go test ./x/treasury/... ./abci/... ./app/...`
Expected: PASS. The `app/` integration tests (`treasury_test.go`, `treasury_multisig_test.go`) now execute priming inside every finalized block; with default genesis (listed denoms, zero stable supply) priming stores a zero snapshot, which `storeLiabilitySnapshot` accepts.

- [ ] **Step 3: Commit**

```bash
git add app/oracle.go
git commit -m "feat(app): wire treasury keeper into the preblock handler

Completes the preblock liability priming path: the app-level handler
now receives the treasury keeper so every finalized block primes the
liability snapshot after oracle prices are written."
```

---

### Task 6: Documentation

**Files:**
- Modify: `docs/TREASURY_REDESIGN_PLAN.md` (§13.6, the liability-scan invariant bullet, near line 2332)
- Modify: `abci/preblock/README.md`

- [ ] **Step 1: Rewrite the design-doc invariant**

In `docs/TREASURY_REDESIGN_PLAN.md` §13.6, replace the bullet:

> Complete aggregate liability is scanned at most once per block after the first successful valuation. Its transient snapshot tracks every later successful Market stable burn/mint; incomplete valuations are not cached and may retry.

with:

> Aggregate liability is scanned exactly once per block, by the preblocker immediately after oracle price application and vote-target advancement; transactions never rescan. The transient snapshot tracks every later successful Market stable burn/mint. An incomplete preblock valuation marks liability unavailable for the entire block: rates are fixed at preblock, so an intra-block retry cannot succeed, and availability returns at the next block's preblock. A mid-block governance change to the tobin whitelist does not lift the marker early; the block stays conservatively degraded. Transaction-time liability valuation charges a flat 2,000 gas whether it reads the snapshot, the marker, or falls back to a lazy scan — swap gas is position-independent within a block and independent of whitelist size, and simulation (whose transient store is always empty) quotes exactly what execution consumes. The lazy scan remains only as a fallback and itself marks the block on incompleteness, bounding free-metered scans to one per block.

- [ ] **Step 2: Document the preblock step**

In `abci/preblock/README.md`, find the section describing the preblock sequence (module-manager PreBlock, vote-extension processing/price application, vote-target advancement) and append this paragraph after the vote-target advancement description:

> After prices and vote targets are written, the handler calls Treasury's `PrimeLiabilitySnapshot`. This values the aggregate stable liability once for the block (or records that valuation is unavailable) so that transaction-time treasury reads are block-position-independent and flat-metered. The call is intentionally thin: all valuation logic lives in `x/treasury/keeper`; failures are wrapped with `ErrTreasuryKeeper` and fail the block like any other preblock state error.

- [ ] **Step 3: Commit**

```bash
git add docs/TREASURY_REDESIGN_PLAN.md abci/preblock/README.md
git commit -m "docs(treasury): record preblock priming and flat-metered liability

Update the section 13.6 invariant: the liability scan now runs exactly
once per block in the preblocker, incomplete valuations mark the whole
block unavailable instead of retrying, and transaction-time valuation
charges a flat 2,000 gas so quoted gas equals executed gas at any
block position and any whitelist size. Document the new preblock step
and its layering in the abci/preblock README."
```

---

### Task 7: Benchmark the primed paths

Anchors the compute claim that motivated the cache (recompute measured at 30.6µs/8 denoms and 78.6µs/26 denoms on Apple M5 Pro; snapshot hit ~8–10.7µs; `RecordSupplyChange` ~1.7µs).

**Files:**
- Create: `x/treasury/keeper/liability_benchmark_test.go`

- [ ] **Step 1: Create the benchmark file**

```go
package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"go.uber.org/mock/gomock"

	chain "ark/pkg/chain"
	oraclekeeper "ark/x/oracle/keeper"
	oracletestutil "ark/x/oracle/testutil"
	oracletypes "ark/x/oracle/types"
	treasurykeeper "ark/x/treasury/keeper"
	treasurytestutil "ark/x/treasury/testutil"
	treasurytypes "ark/x/treasury/types"
)

// Transient keys mirroring the unexported values in liability.go.
var (
	benchLiabilitySnapshotKey    = []byte{0x01}
	benchLiabilityUnavailableKey = []byte{0x02}
)

type liabilityBenchFixture struct {
	keeper           *treasurykeeper.Keeper
	ctx              sdk.Context
	transientService corestore.TransientStoreService
	denoms           []string
}

func (f *liabilityBenchFixture) quoteRates() oracletypes.RateSet {
	return oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		f.denoms[0]:         math.LegacyOneDec(),
	}
}

func (f *liabilityBenchFixture) draw() error {
	_, err := f.keeper.DrawRedemptionBuffer(
		f.ctx,
		sdk.NewInt64Coin(f.denoms[0], 1000),
		math.NewInt(500),
		f.quoteRates(),
	)
	return err
}

func (f *liabilityBenchFixture) resetSnapshot(tb testing.TB) {
	tb.Helper()
	store := f.transientService.OpenTransientStore(f.ctx)
	if err := store.Delete(benchLiabilitySnapshotKey); err != nil {
		tb.Fatal(err)
	}
	if err := store.Delete(benchLiabilityUnavailableKey); err != nil {
		tb.Fatal(err)
	}
}

func newLiabilityBenchFixture(tb testing.TB, denomCount int) *liabilityBenchFixture {
	tb.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	oracletypes.RegisterInterfaces(interfaceRegistry)
	treasurytypes.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	treasuryKey := storetypes.NewKVStoreKey(treasurytypes.StoreKey)
	oracleKey := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	benchBankKey := storetypes.NewKVStoreKey("bench_bank_supply")
	transientKey := storetypes.NewTransientStoreKey("liability_benchmark_transient")
	ctx := sdktestutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			treasurytypes.StoreKey: treasuryKey,
			oracletypes.StoreKey:   oracleKey,
			"bench_bank_supply":    benchBankKey,
		},
		map[string]*storetypes.TransientStoreKey{
			transientKey.Name(): transientKey,
		},
		nil,
	).WithBlockHeight(2).WithBlockTime(time.Unix(1, 0))

	ctrl := gomock.NewController(tb)
	oracleAccountKeeper := oracletestutil.NewMockAccountKeeper(ctrl)
	oracleAccountKeeper.EXPECT().GetModuleAddress(gomock.Any()).
		DoAndReturn(func(name string) sdk.AccAddress {
			return authtypes.NewModuleAddress(name)
		}).AnyTimes()
	oracleKeeper := oraclekeeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(oracleKey),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		distrtypes.ModuleName,
		oracleAccountKeeper,
		oracletestutil.NewMockBankKeeper(ctrl),
		oracletestutil.NewMockDistributionKeeper(ctrl),
		oracletestutil.NewMockStakingKeeper(ctrl),
	)

	denoms := make([]string, denomCount)
	tobinTaxes := make([]oracletypes.TobinTax, denomCount)
	for i := range denomCount {
		denoms[i] = fmt.Sprintf("uasset%03d", i)
		tobinTaxes[i] = oracletypes.TobinTax{
			Denom:    denoms[i],
			TobinTax: oracletypes.DefaultTobinTax,
		}
	}
	oracleParams := oracletypes.DefaultParams()
	oracleParams.TobinTaxes = tobinTaxes
	if err := oracleKeeper.Params.Set(ctx, oracleParams); err != nil {
		tb.Fatal(err)
	}
	for _, denom := range denoms {
		if err := oracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
		}); err != nil {
			tb.Fatal(err)
		}
	}

	// Seed per-denom supply into a real mounted store so GetSupply pays a
	// metered read equivalent to Bank's own lookup.
	supplyStore := ctx.KVStore(benchBankKey)
	for _, denom := range denoms {
		supplyStore.Set([]byte("supply/"+denom), []byte("1000000000000000"))
	}

	treasuryAccountKeeper := treasurytestutil.NewMockAccountKeeper(ctrl)
	treasuryAccountKeeper.EXPECT().GetModuleAddress(gomock.Any()).
		DoAndReturn(func(name string) sdk.AccAddress {
			return authtypes.NewModuleAddress(name)
		}).AnyTimes()
	treasuryBankKeeper := treasurytestutil.NewMockBankKeeper(ctrl)
	treasuryBankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		DoAndReturn(func(c context.Context, denom string) sdk.Coin {
			bz := sdk.UnwrapSDKContext(c).KVStore(benchBankKey).Get([]byte("supply/" + denom))
			amount, ok := math.NewIntFromString(string(bz))
			if !ok {
				tb.Fatalf("bad seeded supply for %s", denom)
			}
			return sdk.NewCoin(denom, amount)
		}).AnyTimes()
	treasuryBankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()

	transientService := runtime.NewTransientStoreService(transientKey)
	keeper := treasurykeeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(treasuryKey),
		transientService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		treasuryAccountKeeper,
		treasuryBankKeeper,
		oracleKeeper,
	)

	return &liabilityBenchFixture{
		keeper:           keeper,
		ctx:              ctx,
		transientService: transientService,
		denoms:           denoms,
	}
}

func BenchmarkLiabilityValuation(b *testing.B) {
	for _, denomCount := range []int{len(oracletypes.DefaultTobinTaxes), 26} {
		// The once-per-block scan performed by the preblocker; includes two
		// transient deletes per iteration to reset the block.
		b.Run(fmt.Sprintf("denoms_%d/preblock_prime", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				fix.resetSnapshot(b)
				if err := fix.keeper.PrimeLiabilitySnapshot(fix.ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
		// The per-swap steady state: valuation via the primed snapshot.
		b.Run(fmt.Sprintf("denoms_%d/snapshot_hit", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			if err := fix.keeper.PrimeLiabilitySnapshot(fix.ctx); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := fix.draw(); err != nil {
					b.Fatal(err)
				}
			}
		})
		// The per-swap snapshot maintenance after burns/mints.
		b.Run(fmt.Sprintf("denoms_%d/record_supply_change", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			if err := fix.keeper.PrimeLiabilitySnapshot(fix.ctx); err != nil {
				b.Fatal(err)
			}
			rates := fix.quoteRates()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := fix.keeper.RecordSupplyChange(
					fix.ctx,
					sdk.NewInt64Coin(fix.denoms[0], 1),
					sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
					rates,
				); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run the benchmarks**

Run: `go test ./x/treasury/keeper/ -run '^$' -bench BenchmarkLiabilityValuation -benchtime 300ms`
Expected: all six sub-benchmarks report; `preblock_prime` lands in the tens of µs (~30µs at 8 denoms, ~80µs at 26), `snapshot_hit` and `record_supply_change` in single-digit µs.

- [ ] **Step 3: Verify the whole repo one final time**

Run: `go build ./... && go test ./x/treasury/... ./abci/... ./app/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add x/treasury/keeper/liability_benchmark_test.go
git commit -m "test(perf): benchmark treasury liability valuation paths

Measure the three liability paths that define the flat-metering
design: the once-per-block preblock prime (the real scan, ~30-80us at
8-26 denoms), the per-swap snapshot hit, and RecordSupplyChange
maintenance. Bank supply reads go through a real mounted store so the
prime numbers reflect metered-read work rather than free mock calls.
These anchor the measured baseline recorded in the 2026-07-28
flat-metering plan."
```

---

## Self-Review

- **Spec coverage:** priming after price application (Task 4 Step 5 placement + Task 2), flat charge with simulation equivalence (Task 3), DoS-safe marker (Task 1), docs/invariant update (Task 6), compute anchoring (Task 7), app wiring (Task 5). The "users pay cached price" goal is delivered by Tasks 3+4+5 jointly.
- **Type consistency:** `liabilityValue` (renamed body) is only called by `cachedLiabilityValue`; `PrimeLiabilitySnapshot(ctx context.Context) error` matches the `arkabcitypes.TreasuryKeeper` interface and the mock; marker helpers are used in Tasks 1 and 2 with identical signatures; benchmark uses only exported keeper API plus hardcoded transient keys documented as mirrors.
- **Known judgment calls locked in:** query path stays metered; `RecordSupplyChange` stays metered; `preblockStatus` deliberately unmodified; flat constant is a code constant (2,000), not a param.
