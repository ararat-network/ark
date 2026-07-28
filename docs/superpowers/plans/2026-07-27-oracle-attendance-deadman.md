# Oracle Attendance Deadman Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the oracle's per-block miss-counting/slash system with a relative attendance deadman: per-validator (eligible, attended) counters accumulated only on functioning blocks, settled once per window with jail-only consequences.

**Architecture:** Aggregation (abci/oracle) computes per-validator `participated` (valid report with ≥1 positive rate) and a per-block `functioningBlock` flag (participating power ≥ ½ commit power). The keeper accumulates an `Attendance{eligible, attended}` record per validator; at each attendance-window boundary it jails (never slashes) validators whose attended/eligible ratio is below `MinAttendancePerWindow`, given a minimum eligible sample. All per-block fault taxonomy is deleted: no out-of-band miss, no omission miss, no unavailability quorum, no full-abstention backstop, no masquerade bookkeeping. Rewards (band-gated, per-target) are untouched. The sidecar zero-fill from this session is rolled back (omission and abstention are now equivalent on-chain).

**Tech Stack:** Cosmos SDK v0.54.x, collections, gogo+pulsar dual proto codegen (`make proto-gen`, Docker), gomock (`mockgen`), testify.

---

## Amendments (2026-07-27, from Phase A reviews — these override the task bodies below)

1. **RecordVoteAccounting has no `attended && !eligible` guard.** The Task 3 body's guard would halt consensus on a non-functioning block with participants (a legitimate, expected state). The final signature is `RecordVoteAccounting(ctx, consAddr, rewardWeight math.Int, eligible bool, participated bool)`: the keeper composes attendance internally (credited only when `eligible && participated`); participation on a non-functioning block is silently ignored, never an error.
2. **Task 4's `TestRecordVoteAccountingRejectsAttendedWithoutEligible` is replaced** by a test asserting the ignore semantics: `(eligible=false, participated=true)` returns nil and leaves the attendance record untouched.
3. **Task 6's recording test double must mirror the keeper's composition**: increment `attendedCounts` only when `eligible && participated`, so the double cannot certify behavior the real keeper rejects.
4. **Task 5 additionally covers**: `MinAttendancePerWindow = 1.0` (attended == eligible passes; one absence jails), `AttendanceWindow = 1` (exercises the minimumSample zero→1 fallback), and rewrites `x/oracle/keeper/accounting_benchmark_test.go` (still seeds `seedMissCount`).
5. **Task 8's sweep is case-insensitive** and additionally renames the slash-era vocabulary in `app/oracle_benchmark_test.go` (`slashWindow`/`slashEligible`/`missValidatorCount` locals, `slash/miss_records_*` sub-benchmark names, `miss_records/op` metric label) and fixes the `abci/preblock/README.md` "validator score/miss" straggler.
6. **Proto deltas applied in review fixes**: `Attendance` carries no `gogoproto.equal`; `EventOracleJail` field order is validator(1), eligible_blocks(2), attended_blocks(3), attendance_window(4). Genesis validation additionally enforces `eligible_blocks ≤ Accounting.attendance_window`.
7. `abci/testutil/interfaces_mocks.go` regeneration happens with Phase A (the interface change owns its mock), not Task 6.
8. **The `minimumSample == 0 → 1` fallback is deleted as dead code** (2026-07-28 review): an eligible=0 record trivially passes the ratio either way, and RecordVoteAccounting only creates records with eligible ≥ 1, so the fallback was behaviorally unobservable — amendment 4's "exercises the zero→1 fallback" requirement is unsatisfiable and replaced by "a one-block window judges its single eligible block". The keeper test suite additionally pins walk continuation after a jail (two-absentee case) and both settlement abort-error branches.
9. **Both hardcoded design constants are superseded** (2026-07-28, post-implementation review). This replaces the "Design constants" bullets under Execution notes below.
   - **The functioning-block fraction is now the `functioning_block_threshold` governance parameter** (`Params` field 9), floored at 50% by `Validate` and defaulted to it, evaluated as `participatingPower >= ceil(threshold × totalPower)`. That form is exactly equivalent to the old `participatingPower * 2 >= totalPower` at the default, so the change ships behaviourally inert. It is deliberately not `vote_threshold`: reusing the price-quorum parameter would let a coalition of `1 - vote_threshold` switch attendance accounting off by going dark, and would retune jailing whenever price quorum is tuned for price safety. The floor is the load-bearing half — below a majority the deadman charges validators for blocks a majority could not price.
   - **`attendanceMinimumSampleDivisor` is deleted outright, with no replacement.** Its stated rationale did not survive review: non-functioning blocks increment neither attendance counter, so the functioning-block gate already absorbs fleet-wide outages entirely, and the minimum sample only added a loophole where a validator dark through every healthy block of a degraded window escaped judgement. Late joiners are covered by the ratio itself, which is scale-free by design. Every record is now judged, however few eligible blocks it holds; `MinAttendancePerWindow` is the whole grace, and an absolute grace allowance was considered and rejected because it would silently soften the ratio governance set.

## Execution notes (read first)

- **No commits.** The working tree contains unrelated in-progress work (x/asset, x/treasury claims). Do not run `git add` or `git commit` for any task unless the user explicitly asks. Never touch files outside the lists below.
- **No worktree.** This plan modifies uncommitted session work already in the main tree (it partially reverts the sidecar zero-fill). Execute in place.
- **Compile-restoration phase.** Task 1 (protos + codegen) breaks compilation until Tasks 2–3 land. Run no tests between Task 1 and the end of Task 3; Task 3 ends with a compile checkpoint. TDD resumes from Task 4 onward.
- **Known pre-existing failures to ignore:** `TestNewRuntimeDerivesComponentLogger`, `TestOracleAndServerDeriveComponentLoggers`, `TestNewValidatorDerivesComponentLogger` fail on this machine due to ANSI-colored log assertions (separate task chip exists). Append `-skip 'TestNewRuntimeDerivesComponentLogger|TestOracleAndServerDeriveComponentLoggers|TestNewValidatorDerivesComponentLogger'` when running `./oracle/...`.
- **Design constants (hardcoded, documented, not params):**
  - Functioning block: `participatingPower * 2 >= totalPower` and `totalPower > 0` (F = ½).
  - Minimum sample: `attendanceWindow / 2` eligible blocks (floor; min 1) before a validator is judged.
  - `MinAttendancePerWindow = 0` disables the deadman entirely (nobody ever jailed) — deliberate governance off-switch.

---

### Task 1: Proto redefinition and codegen

**Files:**
- Modify: `proto/ark/oracle/v1/oracle.proto`
- Modify: `proto/ark/oracle/v1/genesis.proto`
- Modify: `proto/ark/oracle/v1/event.proto`
- Modify: `proto/ark/oracle/v1/query.proto`
- Regenerated: `x/oracle/types/*.pb.go`, `api/ark/oracle/**`

- [ ] **Step 1: Rewrite `Params` in `proto/ark/oracle/v1/oracle.proto`**

Replace fields 6–9 (delete `slash_fraction`, rename and renumber the rest — compact launch-only numbering, per repo convention):

```protobuf
  repeated TobinTax tobin_taxes = 5
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
  // Window changes take effect after the active attendance period settles.
  uint64 attendance_window = 6;
  // min_attendance_per_window is the minimum attended/eligible ratio over one
  // attendance window before a validator is jailed. Zero disables jailing.
  string min_attendance_per_window = 7 [
    (cosmos_proto.scalar) = "cosmos.Dec",
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable) = false,
    (amino.dont_omitempty) = true
  ];
  // max_exchange_rate_age is the maximum elapsed block time since an exchange
  // rate was last written before it is considered stale.
  google.protobuf.Duration max_exchange_rate_age = 8 [
    (gogoproto.nullable) = false,
    (gogoproto.stdduration) = true,
    (amino.dont_omitempty) = true
  ];
```

Then append a new state-value message at the end of `oracle.proto`:

```protobuf
// Attendance counts a validator's functioning-block presence within the
// active attendance window. A block is eligible for a validator when it was in
// the commit and fleet participating power reached the functioning threshold;
// it is attended when the validator also submitted a valid report containing
// at least one positive rate.
message Attendance {
  option (gogoproto.equal) = true;

  uint64 eligible_blocks = 1;
  uint64 attended_blocks = 2;
}
```

- [ ] **Step 2: Rewrite accounting/genesis rows in `proto/ark/oracle/v1/genesis.proto`**

In `GenesisState`, replace field 4:

```protobuf
  repeated AttendanceRecord attendance_records = 4
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
```

In `Accounting`, replace fields 4–5:

```protobuf
  uint64 attendance_window = 4;
  uint64 attendance_window_start_height = 5;
```

Replace the whole `MissCount` message with:

```protobuf
// AttendanceRecord pairs a validator address with its in-progress attendance
// counters for genesis import/export.
message AttendanceRecord {
  string validator_address = 1
      [ (cosmos_proto.scalar) = "cosmos.ValidatorAddressString" ];
  Attendance attendance = 2
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
}
```

- [ ] **Step 3: Replace `EventOracleSlash` in `proto/ark/oracle/v1/event.proto`**

Delete `EventOracleSlash` and add (the `cosmos/base/v1beta1/coin.proto` import stays — `EventOracleReward` still uses it):

```protobuf
// EventOracleJail is emitted after a validator is jailed for insufficient
// oracle attendance over one attendance window.
message EventOracleJail {
  string validator = 1
      [ (cosmos_proto.scalar) = "cosmos.ValidatorAddressString" ];
  uint64 attended_blocks = 2 [ (amino.dont_omitempty) = true ];
  uint64 eligible_blocks = 3 [ (amino.dont_omitempty) = true ];
  uint64 attendance_window = 4 [ (amino.dont_omitempty) = true ];
}
```

- [ ] **Step 4: Replace the MissCount query in `proto/ark/oracle/v1/query.proto`**

Replace the rpc (keep `module_query_safe`):

```protobuf
  // Attendance returns a validator's in-progress oracle attendance counters.
  rpc Attendance(QueryAttendanceRequest) returns (QueryAttendanceResponse) {
    option (cosmos.query.v1.module_query_safe) = true;
    option (google.api.http).get =
        "/ark/oracle/v1/validators/{validator_addr}/attendance";
  }
```

Replace `QueryMissCountRequest`/`QueryMissCountResponse` with:

```protobuf
// QueryAttendanceRequest is the request type for the Query/Attendance RPC
// method.
message QueryAttendanceRequest {
  // validator defines the validator address to query for.
  string validator_addr = 1
      [ (cosmos_proto.scalar) = "cosmos.ValidatorAddressString" ];
}

// QueryAttendanceResponse is response type for the Query/Attendance RPC
// method.
message QueryAttendanceResponse {
  Attendance attendance = 1
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
}
```

If `query.proto` does not already import `ark/oracle/v1/oracle.proto`, add the import.

- [ ] **Step 5: Format, lint, generate**

Run:

```bash
make proto-format && make proto-lint && make proto-gen
```

Expected: generation succeeds; `git status` shows regenerated `x/oracle/types/*.pb.go` and `api/ark/oracle/**`. The Go build is now broken (references to removed identifiers) — expected until Task 3.

---

### Task 2: x/oracle/types — params, keys, genesis types

**Files:**
- Modify: `x/oracle/types/params.go`
- Modify: `x/oracle/types/keys.go:17`
- Modify: `x/oracle/types/genesis.go`
- Modify: `x/oracle/types/params_test.go`, `x/oracle/types/genesis_test.go` (compile-level renames; behavior assertions in Task 4+)

- [ ] **Step 1: Update `x/oracle/types/params.go`**

Delete `DefaultSlashFraction` (line 38) entirely. Rename constants/fields:

```go
	DefaultAttendanceWindow         = chain.BlocksPerWeek // window for a week
	DefaultMinAttendancePerWindow   = math.LegacyNewDecWithPrec(5, 2) // 5%
```

In `DefaultParams()` replace the three old assignments with:

```go
		AttendanceWindow:       DefaultAttendanceWindow,
		MinAttendancePerWindow: DefaultMinAttendancePerWindow,
```

In `Validate()` delete the whole `SlashFraction` block (old lines 80–85) and rename the remaining checks:

```go
	if p.AttendanceWindow == 0 {
		return fmt.Errorf("oracle parameter AttendanceWindow must be > 0, is %d", p.AttendanceWindow)
	}
	if p.MinAttendancePerWindow.IsNil() {
		return errors.New("oracle parameter MinAttendancePerWindow must be set")
	}
	if p.MinAttendancePerWindow.GT(math.LegacyOneDec()) || p.MinAttendancePerWindow.IsNegative() {
		return errors.New("oracle parameter MinAttendancePerWindow must be between [0, 1]")
	}
```

- [ ] **Step 2: Rename the storage key in `x/oracle/types/keys.go`**

```go
	AttendanceKey   = collections.NewPrefix(3)
```

(same prefix number — clean-genesis chain, no migration).

- [ ] **Step 3: Update `x/oracle/types/genesis.go`**

Rename the constructor parameter and field (`missCounts []MissCount` → `attendanceRecords []AttendanceRecord`, `MissCounts:` → `AttendanceRecords:`), the empty-slice literal (`[]MissCount{}` → `[]AttendanceRecord{}`), and the validation loop. New validation body for the loop (mirror the existing ordered-unique-address pattern; keep whatever address ordering checks exist, adapting names):

```go
	// AttendanceRecords: ordered unique validator-address storage keys and
	// internally consistent counters.
	for i, record := range gs.AttendanceRecords {
		// ... keep the existing address parse/ordering checks, renamed ...
		if record.Attendance.AttendedBlocks > record.Attendance.EligibleBlocks {
			return fmt.Errorf(
				"attendance record %d: attended blocks %d exceed eligible blocks %d",
				i,
				record.Attendance.AttendedBlocks,
				record.Attendance.EligibleBlocks,
			)
		}
	}
```

Also rename `NewAccounting(params)` internals: `SlashWindow` → `AttendanceWindow`, `SlashWindowStartHeight` → `AttendanceWindowStartHeight` (grep `types/genesis.go` and any `accounting.go` in types for these).

- [ ] **Step 4: Mechanically rename in `x/oracle/types/params_test.go` and `x/oracle/types/genesis_test.go`**

`SlashWindow` → `AttendanceWindow`, `MinValidPerWindow` → `MinAttendancePerWindow`; delete `SlashFraction` mutate cases; add one mutate case per new validation rule:

```go
		{
			name: "attended above eligible",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{{
					ValidatorAddress: validAddr,
					Attendance:       types.Attendance{EligibleBlocks: 1, AttendedBlocks: 2},
				}}
			},
			wantErr: "exceed eligible",
		},
```

(match the file's existing mutate-pattern table shape and address fixtures exactly).

- [ ] **Step 5: Update `x/oracle/simulation/genesis.go` and `x/oracle/simulation/genesis_test.go`**

Grep for `SlashFraction`, `SlashWindow`, `MinValidPerWindow`, `MissCount` in `x/oracle/simulation/` and `msg_factory.go`; delete the `SlashFraction` randomization and rename the others to the new fields, following each file's existing randomization pattern. No behavior change.

---

### Task 3: Keeper state swap and compile restoration

**Files:**
- Modify: `x/oracle/keeper/keeper.go:42,103-109`
- Modify: `x/oracle/keeper/accounting.go`
- Modify: `x/oracle/keeper/abci.go`
- Modify: `x/oracle/keeper/genesis.go`
- Modify: `x/oracle/keeper/grpc_query.go:140-155`
- Modify: `x/oracle/types/expected_keepers.go`
- Modify: `abci/types/interfaces.go:21`

This task makes the repo compile again with the new semantics stubbed in correctly (full bodies below are final — Tasks 4–6 add the tests that pin them).

- [ ] **Step 1: Swap the collection in `x/oracle/keeper/keeper.go`**

Field (line 42):

```go
	Attendance   collections.Map[sdk.ValAddress, types.Attendance]
```

Constructor (lines 103–109):

```go
		Attendance: collections.NewMap(
			sb,
			types.AttendanceKey,
			"attendance",
			sdk.ValAddressKey,
			codec.CollValue[types.Attendance](cdc),
		),
```

- [ ] **Step 2: Rewrite `RecordVoteAccounting` in `x/oracle/keeper/accounting.go`**

Replace the whole function (keep the doc-comment style):

```go
// RecordVoteAccounting records reward weight and attendance for the validator
// resolved from a consensus address. eligible is true when the block reached
// the functioning threshold with this validator in the commit; attended is
// true when the validator also participated with at least one positive rate.
// If the validator no longer resolves, accounting is skipped.
func (k Keeper) RecordVoteAccounting(
	ctx context.Context,
	consAddr sdk.ConsAddress,
	rewardWeight math.Int,
	eligible bool,
	attended bool,
) error {
	if rewardWeight.IsNil() {
		return errors.New("reward weight must be set")
	}
	if rewardWeight.IsNegative() {
		return fmt.Errorf("reward weight must not be negative: %s", rewardWeight)
	}
	if attended && !eligible {
		return errors.New("attended vote accounting requires an eligible block")
	}
	if rewardWeight.IsZero() && !eligible {
		return nil
	}

	validator, err := k.stakingKeeper.ValidatorByConsAddr(ctx, consAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return nil
		}
		return fmt.Errorf("getting validator by consensus address %s: %w", consAddr, err)
	}
	if validator == nil {
		return nil
	}

	valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
	if err != nil {
		return fmt.Errorf("parsing validator operator address %q: %w", validator.GetOperator(), err)
	}

	if rewardWeight.IsPositive() {
		currentRewardWeight, err := k.RewardWeight.Get(ctx, valAddr)
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return fmt.Errorf("getting reward weight: %w", err)
			}
			currentRewardWeight = math.ZeroInt()
		}
		if err := k.RewardWeight.Set(ctx, valAddr, currentRewardWeight.Add(rewardWeight)); err != nil {
			return fmt.Errorf("setting reward weight for validator %s: %w", validator, err)
		}
	}

	if eligible {
		attendance, err := k.Attendance.Get(ctx, valAddr)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting attendance: %w", err)
		}
		attendance.EligibleBlocks++
		if attended {
			attendance.AttendedBlocks++
		}
		if err := k.Attendance.Set(ctx, valAddr, attendance); err != nil {
			return fmt.Errorf("setting attendance for validator %s: %w", validator, err)
		}
	}

	return nil
}
```

- [ ] **Step 3: Replace `SettleSlash` with `SettleAttendance` in `x/oracle/keeper/accounting.go`**

Delete `SettleSlash` entirely and add:

```go
// attendanceMinimumSampleDivisor derives the minimum eligible-block sample
// (attendance window / divisor) below which a validator is not judged at a
// settlement. Late joiners and long correlated outages therefore never jail.
const attendanceMinimumSampleDivisor = 2

// SettleAttendance jails validators whose attended share of eligible blocks
// fell below the minimum attendance ratio. It never slashes stake: attendance
// is set hygiene, not a safety fault.
func (k Keeper) SettleAttendance(ctx context.Context, attendanceWindowBlocks uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	if params.MinAttendancePerWindow.IsZero() {
		return nil
	}

	minimumSample := attendanceWindowBlocks / attendanceMinimumSampleDivisor
	if minimumSample == 0 {
		minimumSample = 1
	}

	return k.Attendance.Walk(ctx, nil, func(valAddr sdk.ValAddress, attendance types.Attendance) (bool, error) {
		if attendance.EligibleBlocks < minimumSample {
			return false, nil
		}
		attended := math.LegacyNewDec(int64(attendance.AttendedBlocks))
		required := params.MinAttendancePerWindow.MulInt64(int64(attendance.EligibleBlocks))
		if attended.GTE(required) {
			return false, nil
		}

		validator, err := k.stakingKeeper.Validator(ctx, valAddr)
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return false, nil
		}
		if err != nil {
			return true, fmt.Errorf("getting validator %s: %w", valAddr, err)
		}
		if validator == nil || validator.IsUnbonded() || validator.IsJailed() {
			return false, nil
		}
		consAddr, err := validator.GetConsAddr()
		if err != nil {
			return true, fmt.Errorf("getting consensus address for validator %s: %w", valAddr, err)
		}
		if err := k.stakingKeeper.Jail(ctx, consAddr); err != nil {
			return true, fmt.Errorf("jailing validator %s for oracle attendance: %w", valAddr, err)
		}
		if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventOracleJail{
			Validator:        valAddr.String(),
			AttendedBlocks:   attendance.AttendedBlocks,
			EligibleBlocks:   attendance.EligibleBlocks,
			AttendanceWindow: attendanceWindowBlocks,
		}); err != nil {
			return true, fmt.Errorf("emitting oracle jail event: %w", err)
		}
		return false, nil
	})
}
```

Delete the now-unused `distributionHeight`, `powerReduction`, `bondDenom`, and slash-rate locals along with `SettleSlash`. Remove imports that become unused.

- [ ] **Step 4: Update `x/oracle/keeper/abci.go` EndBlocker**

Replace the slash-window block (lines 47–62) with:

```go
	if chain.IsPeriodLastBlockFrom(ctx, accounting.AttendanceWindowStartHeight, accounting.AttendanceWindow) {
		if err := k.SettleAttendance(ctx, accounting.AttendanceWindow); err != nil {
			return err
		}

		// Clear attendance records after settlement.
		if err := k.Attendance.Clear(ctx, nil); err != nil {
			return fmt.Errorf("clearing attendance records: %w", err)
		}

		if accounting.AttendanceWindow != params.AttendanceWindow {
			accounting.AttendanceWindow = params.AttendanceWindow
			accounting.AttendanceWindowStartHeight = uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()) + 1
			accountingChanged = true
		}
	}
```

Update the EndBlocker doc comment: `// EndBlocker settles periodic oracle rewards and attendance.`

- [ ] **Step 5: Update keeper genesis import/export in `x/oracle/keeper/genesis.go`**

Import loop (old lines 62–69):

```go
	for _, record := range data.AttendanceRecords {
		operator, err := sdk.ValAddressFromBech32(record.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing attendance validator address %q: %w", record.ValidatorAddress, err)
		}
		if err := k.Attendance.Set(ctx, operator, record.Attendance); err != nil {
			return fmt.Errorf("setting attendance: %w", err)
		}
	}
```

Export loop (old lines 123–128):

```go
	attendanceRecords := []types.AttendanceRecord{}
	if err := k.Attendance.Walk(ctx, nil, func(operator sdk.ValAddress, attendance types.Attendance) (bool, error) {
		attendanceRecords = append(attendanceRecords, types.AttendanceRecord{
			ValidatorAddress: operator.String(),
			Attendance:       attendance,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("exporting attendance records: %w", err)
	}
```

Thread the renamed slice through the surrounding `NewGenesisState` call, preserving argument order (position of old `missCounts`).

- [ ] **Step 6: Replace the query in `x/oracle/keeper/grpc_query.go`**

```go
// Attendance queries a validator's in-progress oracle attendance counters.
func (q queryServer) Attendance(ctx context.Context, req *types.QueryAttendanceRequest) (*types.QueryAttendanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	attendance, err := q.k.Attendance.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryAttendanceResponse{Attendance: attendance}, nil
}
```

Match the file's existing error/status conventions exactly (copy them from the old `MissCount` body — keep whatever not-found behavior it had; if the old body returned the zero value on not-found, preserve that, which the code above does via the zero `attendance`).

- [ ] **Step 7: Shrink `x/oracle/types/expected_keepers.go` and regenerate mocks**

Delete the `Slash(...)` method from `StakingKeeper` (line 16). Then grep for `PowerReduction` and `BondDenom` under `x/oracle/`; if their only callers were `SettleSlash`, delete them from the interface too. Regenerate:

```bash
mockgen -source=x/oracle/types/expected_keepers.go -package testutil -destination x/oracle/testutil/expected_keepers_mocks.go
```

- [ ] **Step 8: Update the ABCI-side interface `abci/types/interfaces.go:21`**

```go
	RecordVoteAccounting(ctx context.Context, validator sdk.ConsAddress, rewardWeight math.Int, eligible bool, attended bool) error
```

- [ ] **Step 9: Compile checkpoint (tests still expected red)**

Run:

```bash
go build ./x/oracle/... ./abci/... 2>&1 | head -30
```

Expected: only test files and `abci/oracle`/`abci/preblock` production code may still fail (aggregation still sets `missed`); fix nothing in `abci/oracle` yet — that is Task 6. If `x/oracle` production code fails, fix residual renames until `go build ./x/oracle/...` is clean.

---

### Task 4: RecordVoteAccounting tests (TDD against new semantics)

**Files:**
- Modify: `x/oracle/keeper/accounting_test.go`

- [ ] **Step 1: Rewrite the RecordVoteAccounting test block**

Update every existing call for the new signature. Table of behaviors to cover (adapt to the file's existing suite/mocking pattern — `s.keeper`, `s.stakingKeeper` mocks):

```go
func (s *KeeperTestSuite) TestRecordVoteAccountingAccumulatesAttendance() {
	valAddr, consAddr := s.registerValidator() // reuse the file's existing fixture helper for ValidatorByConsAddr

	// Eligible + attended increments both counters.
	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), true, true))
	// Eligible only increments eligible.
	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), true, false))
	// Non-eligible zero-weight call is a no-op.
	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), false, false))

	attendance, err := s.keeper.Attendance.Get(s.ctx, valAddr)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), attendance.EligibleBlocks)
	s.Require().Equal(uint64(1), attendance.AttendedBlocks)
}

func (s *KeeperTestSuite) TestRecordVoteAccountingRejectsAttendedWithoutEligible() {
	err := s.keeper.RecordVoteAccounting(s.ctx, sdk.ConsAddress("v"), math.ZeroInt(), false, true)
	s.Require().ErrorContains(err, "eligible")
}
```

Keep/adapt the existing reward-weight accumulation and unknown-validator-skip tests with the new signature (`..., math.NewInt(10), true, true`). Delete miss-specific tests.

- [ ] **Step 2: Run and verify**

```bash
go test ./x/oracle/keeper/ -run 'TestKeeperTestSuite' -v 2>&1 | grep -E 'RecordVoteAccounting|FAIL|ok' | head
```

Expected: new tests pass; unrelated suite tests may still fail until Tasks 5–6 (note which, ensure they are attendance/abci ones).

---

### Task 5: SettleAttendance tests

**Files:**
- Modify: `x/oracle/keeper/abci_test.go` (the settlement sub-tests around lines 80–160)
- Modify: `x/oracle/keeper/accounting_test.go` (if SettleSlash tests live there)
- Check: `x/oracle/keeper/accounting_benchmark_test.go` for stale references

- [ ] **Step 1: Rewrite the settlement sub-test**

Replace the "slash window settles slash and clears miss counts" sub-test with (same fixtures, no `Slash`/`PowerReduction`/`BondDenom` expectations):

```go
	s.Run("attendance window jails absentees and clears records", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.RewardWindow = 30
		params.AttendanceWindow = 20
		params.MinAttendancePerWindow = math.LegacyNewDecWithPrec(90, 2)
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))
		// valAddr1: 0/20 attended over a full-window sample -> jailed.
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))
		// valAddr2: 20/20 attended -> untouched.
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr2, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 20}))
		// valAddr3: below the minimum sample (20/2 = 10) -> never judged.
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr3, types.Attendance{EligibleBlocks: 9, AttendedBlocks: 0}))

		pubKey := ed25519.GenPrivKey().PubKey()
		validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
		s.Require().NoError(err)
		validator.Status = stakingtypes.Bonded
		consAddr, err := validator.GetConsAddr()
		s.Require().NoError(err)

		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)
		s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		_, err = s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected attendance to be cleared, got %v", err)
	})
```

(If the suite has no `valAddr3`, add one alongside the existing fixtures.) Note: no `Validator` expectation for valAddr2/valAddr3 — the walk must skip them before any staking lookup; gomock enforces this by failing on unexpected calls.

- [ ] **Step 2: Add the disable-switch and jailed-skip sub-tests**

```go
	s.Run("zero minimum attendance disables jailing", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.AttendanceWindow = 20
		params.MinAttendancePerWindow = math.LegacyZeroDec()
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	})

	s.Run("already jailed validators are not re-jailed", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.AttendanceWindow = 20
		params.MinAttendancePerWindow = math.LegacyNewDecWithPrec(90, 2)
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))

		pubKey := ed25519.GenPrivKey().PubKey()
		validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
		s.Require().NoError(err)
		validator.Status = stakingtypes.Bonded
		validator.Jailed = true

		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	})
```

- [ ] **Step 3: Rename the window-change sub-test fields**

In "window changes activate after the current period settles": `SlashWindow` → `AttendanceWindow`, `MinValidPerWindow` → `MinAttendancePerWindow`, delete `SlashFraction`, replace the `MissCount.Set` seed with an `Attendance.Set` seed that stays below the minimum sample (`types.Attendance{EligibleBlocks: 4, AttendedBlocks: 4}`), and delete the `Slash`/`PowerReduction`/`BondDenom` expectations (keep/adjust `Validator`+`Jail` only if the seeded record can trigger them — with 4/4 attended it cannot, so delete those expectations too).

- [ ] **Step 4: Run and verify**

```bash
go test ./x/oracle/keeper/ 2>&1 | tail -5
```

Expected: PASS (grpc_query/genesis tests may still reference old names — fix compile-level renames as encountered; behavior tests for those come in the same run).

---

### Task 6: ABCI pipeline — aggregation, vote processor, tests

**Files:**
- Modify: `abci/oracle/aggregation.go`
- Modify: `abci/oracle/vote_processor.go:78-88`
- Modify: `abci/oracle/aggregation_test.go`
- Modify: `abci/oracle/aggregation_internal_test.go`
- Modify: `abci/preblock/target_transition_test.go`
- Check: `abci/oracle/oracle_votes.go:38-41` (comment), `abci/oracle/vote_processor_test.go`

- [ ] **Step 1: Rewrite `aggregation.go` structures and the vote loops**

New score/result types:

```go
// aggregationResult contains the internal output of one complete oracle aggregation.
type aggregationResult struct {
	prices map[string]math.LegacyDec
	scores []validatorScore
	// functioningBlock is true when participating power reached the attendance
	// threshold, making the block eligible for every commit validator.
	functioningBlock bool
}

// validatorScore directs oracle rewards and attendance accounting to a validator.
type validatorScore struct {
	recipient       sdk.ConsAddress
	votingPower     int64
	rewardedTargets int64
	rewardWeight    math.Int
	// participated is true for a valid report containing at least one positive
	// rate; abstentions, omissions, and invalid reports do not participate.
	participated bool
}
```

Add the constant near the top of the file:

```go
// functioningBlockDivisor sets the participation threshold for an eligible
// block: participating power must reach 1/functioningBlockDivisor of total
// commit power. Attendance is only graded on functioning blocks, so correlated
// outages judge no one.
const functioningBlockDivisor = 2
```

Replace the counting pass (everything between the first per-vote loop and the ballot fill) with:

```go
	// Count first so each ballot can allocate exactly enough space for its
	// positive reports without geometric slice growth. A non-positive submitted
	// rate is an abstention: it adds no ballot power and earns no score weight.
	positiveRateCounts := make([]int, len(targetDenoms))
	var participatingPower int64
	for validatorIndex, vote := range votes {
		if !vote.ValidReport {
			continue
		}
		for _, submittedRate := range vote.Rates {
			if !submittedRate.Value.IsPositive() {
				continue
			}
			result.scores[validatorIndex].participated = true
			positiveRateCounts[submittedRate.TargetIndex]++
		}
		if result.scores[validatorIndex].participated {
			participatingPower += vote.Validator.Power
		}
	}
	result.functioningBlock = totalPower > 0 &&
		participatingPower*functioningBlockDivisor >= totalPower
```

Delete entirely: `reportedPowers`, `validReportPower`, the `unavailableTargets`/`unavailableTargetCount` block inside the threshold computation (keep only the price-quorum `passingTargets` append), and the whole `requiredReportCount` loop. The threshold block becomes:

```go
	passingTargets := make([]int, 0, len(ballots))
	var thresholdPower int64
	if totalPower > 0 {
		thresholdPower = params.VoteThreshold.
			MulInt64(totalPower).
			Ceil().
			TruncateInt64()

		for targetIndex := range ballots {
			if ballots[targetIndex].power >= thresholdPower {
				passingTargets = append(passingTargets, targetIndex)
			}
		}
	}
```

In `computePricesAndScores`, delete the `else { scores[vote.validator].missed = true }` branch — out-of-band votes simply earn nothing:

```go
			for _, vote := range tally.votes {
				if vote.rate.GTE(lowerBound) && vote.rate.LTE(upperBound) {
					scores[vote.validator].rewardedTargets++
				}
			}
```

Note the early-return paths (`len(targetDenoms) == 0`, `totalPower <= 0 || len(passingTargets) == 0`) must still return `result` with `functioningBlock` already computed where the counting pass ran; the `len(targetDenoms) == 0` early return happens before the counting pass and leaves `functioningBlock` false — correct, since participation requires a positive rate on a target.

- [ ] **Step 2: Update `vote_processor.go` accounting loop**

```go
	// Update rewards and attendance in oracle.
	for _, score := range result.scores {
		if err := oracleKeeper.RecordVoteAccounting(
			ctx,
			score.recipient,
			score.rewardWeight,
			result.functioningBlock,
			score.participated,
		); err != nil {
			return nil, fmt.Errorf(
				"%w: record vote accounting for %s: %w",
				arkabcitypes.ErrOracleKeeper,
				score.recipient.String(),
				err,
			)
		}
	}
```

- [ ] **Step 3: Update the `Vote.Rates` comment in `oracle_votes.go:38-41`**

```go
	// Rates contains validated domain rates keyed by canonical target index.
	// Non-positive values are retained as abstentions: they never enter ballots
	// and do not count as participation. It is nil when ValidReport is false.
	Rates []VoteRate
```

- [ ] **Step 4: Rewrite the recording keeper and tests in `aggregation_test.go`**

Recorder changes:

```go
type recordingOracleKeeper struct {
	params            oracletypes.Params
	voteTargets       []string
	exchangeRate      map[string]math.LegacyDec
	exchangeRateOrder []string
	scoreWeights      map[string]math.Int
	scoreOrder        []string
	eligibleCounts    map[string]uint64
	attendedCounts    map[string]uint64
}
```

(initialize both maps in `newRecordingOracleKeeper`), and:

```go
func (k *recordingOracleKeeper) RecordVoteAccounting(
	_ context.Context,
	validator sdk.ConsAddress,
	scoreWeight math.Int,
	eligible bool,
	attended bool,
) error {
	validatorKey := validator.String()
	currentWeight, ok := k.scoreWeights[validatorKey]
	if !ok {
		currentWeight = math.ZeroInt()
	}
	k.scoreWeights[validatorKey] = currentWeight.Add(scoreWeight)
	k.scoreOrder = append(k.scoreOrder, validatorKey)
	if eligible {
		k.eligibleCounts[validatorKey]++
	}
	if attended {
		k.attendedCounts[validatorKey]++
	}
	return nil
}
```

Per-test disposition (every `missCounts` assertion is replaced; `keeper.missCounts[...]` no longer exists):

1. `TestAggregateOracleVotesRecognizesUnavailableTargetQuorum` → rename `TestAggregateOracleVotesLeavesOmittedTargetUnpriced`. Same votes/targets. New assertions: `akrw` not priced, `ausd` priced, both voters `eligibleCounts == 1` and `attendedCounts == 1`, weights unchanged.
2. `TestAggregateOracleVotesUsesCeilingForUnavailableTargetThreshold` → delete (no unavailability threshold exists).
3. `TestAggregateOracleVotesPriceQuorumPrecedesUnavailableQuorum` → delete.
4. `TestAggregateOracleVotesKeepsFailedQuorumTargetAccountable` → rename `TestAggregateOracleVotesDoesNotPunishFailedQuorumVotes`. All three rate cases (positive/zero/negative) now expect: voter attended (they price `ausd`), omitting voter also attended (prices `ausd`), no punishment dimension at all — assert `attendedCounts == 1` for both voters in every case.
5. `TestAggregateOracleVotesSkipsCrossRateDenomWithoutReferenceOverlap`, `...ChoosesReferenceWithBestOverlapCoverage`, `...SkipsCrossRateDenomBelowOverlapQuorum`, `...UsesCeilingForVoteThreshold`, `...UsesCeilingForOverlapThreshold`, `...UsesMedianOfValidatorCrossRates` (drop its missCounts asserts; keep weights), `...SkipsUnrepresentableCrossRateObservation` (extreme voter: weight zero, `attendedCounts == 1` — they participated with positive rates), `...UsesDeterministicWriteOrder` → keep, update only signatures/assertions.
6. `TestAggregateOracleVotesTreatsFullAbstentionAsMiss` → rename `TestAggregateOracleVotesRecordsFullAbstentionAsEligibleOnly`: abstaining voter expects `eligibleCounts == 1 && attendedCounts == 0`; positive voter `attendedCounts == 1`.
7. `TestAggregateOracleVotesTreatsAllTargetAbstentionAsMiss` → rename `...RecordsAllTargetAbstentionAsEligibleOnly`, same expectation shape.
8. `TestAggregateOracleVotesAllowsPartialAbstentionWithoutMiss` → rename `...CountsPartialAbstentionAsAttended`: abstainer `attendedCounts == 1`, weight 10.
9. `TestAggregateOracleVotesDoesNotTreatAbstentionAsUnavailable` → delete (no unavailability).
10. `TestAggregateOracleVotesKeepsOmissionMissDespiteAbstention` → delete; omission is not punished.
11. `TestAggregateOracleVotesExcludesAbstentionFromPriceQuorum` → keep, replace miss asserts with attended asserts (both voters attended).
12. `TestAggregateOracleVotesDoesNotMissAbstentionWhenAllTargetsUnavailable` → delete.
13. `TestAggregateOracleVotesPenalizesPositiveOutOfBandTargetRates` → rename `...GivesOutOfBandVotesNoRewardWeight`: out-of-band voter weight zero, `attendedCounts == 1`.
14. `TestAggregateOracleVotesFixedBandSkipsExtremeNonPositiveRate` → keep name; non-positive voter: weight zero, `eligibleCounts == 1`, `attendedCounts == 0`.

New tests to add:

```go
func TestAggregateOracleVotesMarksNonFunctioningBlockIneligible(t *testing.T) {
	participant := []byte{1}
	absentee := []byte{2}
	votes := []testVote{
		newTestVote(participant, 40, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(absentee, 60, map[string]math.LegacyDec{}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	voteTargets := map[string]math.LegacyDec{
		"ausd": math.LegacyZeroDec(),
	}

	keeper, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	// Participating power 40 of 100 is below the 1/2 functioning threshold, so
	// nobody accrues attendance eligibility this block.
	require.Zero(t, keeper.eligibleCounts[sdk.ConsAddress(participant).String()])
	require.Zero(t, keeper.eligibleCounts[sdk.ConsAddress(absentee).String()])
}

func TestAggregateOracleVotesGradesInvalidReportsEligibleOnFunctioningBlocks(t *testing.T) {
	participant := []byte{1}
	invalidVoter := []byte{2}
	votes := []testVote{
		newTestVote(participant, 60, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		// Absent extension: ValidReport false in the decoded votes.
		newTestVote(invalidVoter, 40, nil),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	voteTargets := map[string]math.LegacyDec{
		"ausd": math.LegacyZeroDec(),
	}

	keeper, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Equal(t, uint64(1), keeper.eligibleCounts[sdk.ConsAddress(participant).String()])
	require.Equal(t, uint64(1), keeper.attendedCounts[sdk.ConsAddress(participant).String()])
	require.Equal(t, uint64(1), keeper.eligibleCounts[sdk.ConsAddress(invalidVoter).String()])
	require.Zero(t, keeper.attendedCounts[sdk.ConsAddress(invalidVoter).String()])
}
```

(For the `nil`-rates vote, check how `applyOracleVoteExtensions` encodes votes: a `nil` map must produce an *empty vote extension* — if the helper always encodes a valid extension, extend `testVote` with an `absent bool` and pass empty `VoteExtension` bytes in `NewExtendedVoteInfo` for that voter; keep the helper change minimal.)

- [ ] **Step 5: Update `aggregation_internal_test.go`**

`TestAggregateOracleVotesWithNoTargetsDoesNotRecordMisses` → rename `TestAggregateOracleVotesWithNoTargetsIsNotFunctioning`; assert `result.functioningBlock` is false and every score has `participated == false` (replace the `score.missed` assertions). `TestComputePricesAndScoresRewardBand`: replace `scores[0].missed` assertions — out-of-band now asserts `rewardedTargets` stays 0 (in-band case asserts 1). `TestComputePricesAndScoresSkipsUnrepresentableRewardBand`: drop the `missed` assertion, keep reward assertions. Other ballot/reference tests: unaffected.

- [ ] **Step 6: Update `abci/preblock/target_transition_test.go`**

Recorder: replace `missed []bool` with `attended []bool`; `RecordVoteAccounting(_, _, _ math.Int, eligible, attended bool)` appends `attended`. Final assertion and comment:

```go
	// Both blocks function (the sole validator participates), and the explicit
	// zero on the just-activated target is an abstention that does not affect
	// attendance while ausd is still priced.
	require.Equal(t, []bool{true, true}, keeper.attended)
```

- [ ] **Step 7: Run the ABCI suites**

```bash
go test ./abci/... 2>&1 | grep -v 'no test files'
```

Expected: all `ok`. Then re-run `go test ./x/oracle/...` — all `ok`.

---

### Task 7: Sidecar zero-fill rollback

**Files:**
- Revert: `oracle/sidecar/rpc.go`, `oracle/sidecar/rpc_test.go`, `oracle/sidecar/runtime/prices.go`, `oracle/sidecar/runtime/runtime.go`, `oracle/sidecar/runtime/runtime_internal_test.go`, `oracle/sidecar/types/prices.go`

- [ ] **Step 1: Confirm these files contain only this session's zero-fill changes**

```bash
git diff --stat oracle/sidecar/
```

Expected: exactly the six files above. If anything else appears, stop and ask the user.

- [ ] **Step 2: Revert to HEAD**

```bash
git checkout -- oracle/sidecar/rpc.go oracle/sidecar/rpc_test.go oracle/sidecar/runtime/prices.go oracle/sidecar/runtime/runtime.go oracle/sidecar/runtime/runtime_internal_test.go oracle/sidecar/types/prices.go
```

- [ ] **Step 3: Verify**

```bash
go test ./oracle/... -skip 'TestNewRuntimeDerivesComponentLogger|TestOracleAndServerDeriveComponentLoggers|TestNewValidatorDerivesComponentLogger' 2>&1 | grep -v 'no test files'
```

Expected: all `ok`.

---

### Task 8: Documentation

**Files:**
- Modify: `abci/oracle/README.md`
- Modify: `abci/voteextension/README.md`

- [ ] **Step 1: Rewrite the accountability sections of `abci/oracle/README.md`**

Delete the "Target Unavailability" section entirely. Replace the miss-semantics sentences (the paragraph after the raw-quorum formula, and the unavailability-related sentences elsewhere) with one "Participation and Attendance" section:

```markdown
### Participation And Attendance

There is no per-block fault accounting. A validator *participates* in a block
when its valid report contains at least one positive rate; abstentions
(non-positive rates), omissions, and invalid reports simply do not participate
and earn nothing. A block is *functioning* when participating power reaches
half of total commit power. Every commit validator of a functioning block
accrues one eligible attendance unit, and participants also accrue one attended
unit. Once per attendance window the oracle module jails — never slashes —
validators whose attended share of eligible blocks is below
`min_attendance_per_window`, provided they were eligible for at least half the
window. Grading only functioning blocks means correlated outages judge no one;
accuracy and coverage incentives come entirely from band-gated rewards.
```

Sweep the rest of the file for `miss`, `unavailab`, `slash` and update any stragglers (e.g., the complexity section's mention of reported powers).

- [ ] **Step 2: Rewrite the ExtendVote paragraph of `abci/voteextension/README.md`**

Replace the current second paragraph (which describes zero-fill and unavailability claims) with:

```markdown
A fresh sparse response becomes a valid partial vote extension. Omitted targets
and non-positive rates are equivalent for accountability: neither is punished,
neither earns rewards, and neither contributes ballot power — price quorum
alone decides whether a target publishes. Oracle failures are not consensus
failures. If the oracle client is unavailable, returns nil prices, returns
invalid prices, or encoding fails, the handler logs the error and returns an
empty vote extension; an empty extension is an invalid report, which counts as
an eligible-but-unattended block for attendance grading when the rest of the
fleet is functioning.
```

- [ ] **Step 3: Repo-wide stale-term sweep**

```bash
grep -rn "SlashFraction\|MinValidPerWindow\|SlashWindow\|MissCount\|miss_count\|slash_window\|min_valid_per_window\|EventOracleSlash\|unavailability" --include="*.go" --include="*.md" --include="*.proto" . | grep -v classic-core | grep -v "/build/" | grep -v ".pb.go" | grep -v pulsar
```

Expected: no hits outside `docs/superpowers/plans/` and historical design docs (`docs/TREASURY_REDESIGN_PLAN.md` mentions oracle only peripherally — update only if a hit describes the removed mechanisms as current).

---

### Task 9: Full verification

- [ ] **Step 1: Build and test everything affected**

```bash
go build ./... && go test ./x/oracle/... ./abci/... ./oracle/... -skip 'TestNewRuntimeDerivesComponentLogger|TestOracleAndServerDeriveComponentLoggers|TestNewValidatorDerivesComponentLogger' 2>&1 | grep -v 'no test files'
```

Expected: build clean, all `ok`.

- [ ] **Step 2: Broader regression check**

```bash
go test ./app/... ./x/market/... ./x/treasury/... 2>&1 | tail -8
```

Expected: unchanged from before this plan (these modules do not consume the oracle accounting surface; if `app` wiring references removed query names, fix the references). Note: the working tree contains unrelated WIP in `x/asset`/`x/treasury` — pre-existing failures there, if any, are out of scope; compare against a baseline run if unsure.

- [ ] **Step 3: Report**

Summarize: what was deleted, the new semantics, test counts, and the exact verification commands run, flagging any deviations from this plan.
