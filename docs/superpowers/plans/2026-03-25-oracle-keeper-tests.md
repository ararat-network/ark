# Oracle Keeper Unit Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add unit tests for the 5 untested oracle keeper files: `vote.go`, `slash.go`, `reward.go`, `msg_server.go`,
and `grpc_query.go`.

**Architecture:** Each test file mirrors its source file (e.g., `vote_test.go` tests only `vote.go`). All tests use the
existing `KeeperTestSuite` from `keeper_test.go` which provides mock-based setup via `gomock`. Tests are table-driven
with `s.Run()` sub-tests. `SetupSubTest()` calls `SetupTest()` so every sub-test gets fresh state.

**Tech Stack:** Go, testify suite, gomock, cosmos-sdk v0.53, collections

---

## Conventions (Read This First)

All 5 test files share these conventions — do not repeat this boilerplate in each file:

**Package & imports.** Every file is `package keeper_test`. Common imports:

```go
import (
    "go.uber.org/mock/gomock"
    "cosmossdk.io/math"
    sdk "github.com/cosmos/cosmos-sdk/types"
    // + whatever else each specific file needs
    core "noah/types"
    "noah/x/oracle/types"
)
```

**Suite attachment.** Do NOT add a new `TestKeeperTestSuite` func — it already exists in `keeper_test.go`. Methods you
add (e.g., `(s *KeeperTestSuite) TestPickReferenceDenom()`) are auto-discovered by testify.

**Shared test fixtures.** `keeper_test.go` already defines these; use them directly:

```go
var (
    valAddr1 = sdk.ValAddress([]byte("validator1___________"))
    valAddr2 = sdk.ValAddress([]byte("validator2___________"))
    accAddr1 = sdk.AccAddress([]byte("feeder1______________"))
    accAddr2 = sdk.AccAddress([]byte("feeder2______________"))
)
func bondedValidator() stakingtypes.ValidatorI   { ... }
func unbondedValidator() stakingtypes.ValidatorI { ... }
```

**Shared helpers from `abci_test.go`** (already exist, reuse):

```go
var valCodec = address.NewBech32Codec("cosmosvaloper")
func makeValidator(valAddr sdk.ValAddress, status stakingtypes.BondStatus, power int64) stakingtypes.Validator
func operStr(valAddr sdk.ValAddress) string
func (s *KeeperTestSuite) setupBuildValidatorClaimMapMocks(bondedVals []sdk.ValAddress, powers []int64, maxVals uint32)
type mockModuleAccount struct { ... }
type mockIterator struct { ... }
```

**New helper needed for slash tests.** `makeValidator` doesn't set `ConsensusPubkey`, so `validator.GetConsAddr()`
fails. Add this helper to `slash_test.go` (or `abci_test.go`):

```go
import (
    "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
    codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

// makeValidatorWithConsKey extends makeValidator with a consensus pubkey
// so that GetConsAddr() works (needed for slash/jail path).
func makeValidatorWithConsKey(valAddr sdk.ValAddress, status stakingtypes.BondStatus, power int64) stakingtypes.Validator {
    v := makeValidator(valAddr, status, power)
    privKey := ed25519.GenPrivKey()
    pk, _ := codectypes.NewAnyWithValue(privKey.PubKey())
    v.ConsensusPubkey = pk
    return v
}
```

Use `makeValidatorWithConsKey` in any slash test case that enters the `if validator.IsBonded() && !validator.IsJailed()`
block (i.e., where Slash/Jail are expected to be called).

**Mock expectations.** The `SetupTest()` in `keeper_test.go` already calls
`s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})` for keeper creation. Any
additional mock expectations go in the test's `setup` func. Never set expectations outside `setup` — `SetupSubTest`
resets the controller.

**Default params.** `SetupTest` sets `types.DefaultParams()` which has `VotePeriod=15`, `VoteThreshold=0.50`,
`SlashWindow=100800`, `SlashFraction=0.0001`, `MinValidPerWindow=0.05`, `RewardDistributionWindow=5256000`. Override in
`setup` when needed.

**Running tests:**

```bash
go test -v -run TestKeeperTestSuite/TestFunctionName ./x/oracle/keeper/...
```

**Compile check:**

```bash
go build ./x/oracle/...
```

---

## File Map

| Source file                     | New test file                        | Functions to test                                                                                  |
| ------------------------------- | ------------------------------------ | -------------------------------------------------------------------------------------------------- |
| `x/oracle/keeper/vote.go`       | `x/oracle/keeper/vote_test.go`       | `PickReferenceDenom`, `BuildValidatorClaimMap`, `CountMisses`                                      |
| `x/oracle/keeper/slash.go`      | `x/oracle/keeper/slash_test.go`      | `SlashAndResetMissCounters`                                                                        |
| `x/oracle/keeper/reward.go`     | `x/oracle/keeper/reward_test.go`     | `RewardBallotWinners`                                                                              |
| `x/oracle/keeper/msg_server.go` | `x/oracle/keeper/msg_server_test.go` | `AggregateExchangeRatePrevote`, `AggregateExchangeRateVote`, `DelegateFeedConsent`, `UpdateParams` |
| `x/oracle/keeper/grpc_query.go` | `x/oracle/keeper/grpc_query_test.go` | All 13 query RPCs                                                                                  |

---

## Task 1: `vote_test.go` — PickReferenceDenom, BuildValidatorClaimMap, CountMisses

**Files:**

- Create: `x/oracle/keeper/vote_test.go`

### TestPickReferenceDenom

Tests `vote.go:18-58`. This function picks the denom with highest voter turnout as the reference. It also prunes denoms
below threshold from both `voteTargets` and `voteMap`.

Mocks needed: `TotalBondedTokens`, `PowerReduction` (called for threshold calculation).

- [ ] **Step 1: Write `TestPickReferenceDenom` with these table cases**

```go
func (s *KeeperTestSuite) TestPickReferenceDenom() {
 tests := []struct {
  name            string
  setup           func()
  voteTargets     map[string]math.LegacyDec
  voteMap         map[string]types.ExchangeRateBallot
  expectedDenom   string
  expectPruned    []string // denoms removed from voteTargets after call
 }{
  {
   name: "single denom above threshold — selected",
   // voteMap has ukrw with 20 power, threshold is 50% of 20 total = 10
   // ballot passes, ukrw selected
  },
  {
   name: "two denoms — higher power wins",
   // ukrw has 20 power, uusd has 10 power
   // both pass threshold, ukrw wins
  },
  {
   name: "two denoms same power — alphabetical wins",
   // ukrw and uusd both have 20 power
   // ukrw < uusd alphabetically, so ukrw wins
  },
  {
   name: "denom below threshold — pruned from voteTargets",
   // ukrw has 1 power, threshold = 10
   // ukrw pruned, returns empty string
  },
  {
   name: "denom not in voteTargets — pruned from voteMap",
   // voteMap has "ufoo" but voteTargets doesn't
   // "ufoo" removed from voteMap, returns empty string
  },
  {
   name: "no votes — returns empty string",
   // empty voteMap
  },
 }
}
```

Each test case needs `setup` to mock `TotalBondedTokens` and `PowerReduction`:

```go
setup: func() {
    s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
},
```

Build `voteMap` entries using `types.ExchangeRateBallot` with `types.VoteForTally`:

```go
voteMap: map[string]types.ExchangeRateBallot{
    core.MicroKRWDenom: {
        types.NewVoteForTally(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
        types.NewVoteForTally(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr2, 10),
    },
},
```

Assert `s.Require().Equal(tc.expectedDenom, result)` and verify pruned denoms are gone from voteTargets map.

- [ ] **Step 2: Run test — verify it passes**

```bash
go test -v -run TestKeeperTestSuite/TestPickReferenceDenom ./x/oracle/keeper/...
```

### TestBuildValidatorClaimMap

Tests `vote.go:61-91`. Iterates bonded validators from power store iterator and builds claim map.

Mocks needed: `MaxValidators`, `ValidatorsPowerStoreIterator`, `PowerReduction`, `Validator` (per val),
`ValidatorAddressCodec`. These are already wrapped in `setupBuildValidatorClaimMapMocks`.

- [ ] **Step 3: Write `TestBuildValidatorClaimMap` with these table cases**

```go
func (s *KeeperTestSuite) TestBuildValidatorClaimMap() {
 tests := []struct {
  name          string
  setup         func()
  expectedClaims int
  expectedPower  map[string]int64 // operStr -> power
 }{
  {
   name: "two bonded validators",
   setup: func() {
    s.setupBuildValidatorClaimMapMocks(
     []sdk.ValAddress{valAddr1, valAddr2},
     []int64{10, 20},
     100,
    )
   },
   expectedClaims: 2,
   expectedPower: map[string]int64{
    operStr(valAddr1): 10,
    operStr(valAddr2): 20,
   },
  },
  {
   name: "max validators caps result",
   setup: func() {
    s.setupBuildValidatorClaimMapMocks(
     []sdk.ValAddress{valAddr1},
     []int64{10},
     1, // maxValidators = 1, only first returned
    )
   },
   expectedClaims: 1,
  },
  {
   name: "no validators — empty map",
   setup: func() {
    s.stakingKeeper.EXPECT().MaxValidators(s.ctx).Return(uint32(100))
    s.stakingKeeper.EXPECT().ValidatorsPowerStoreIterator(s.ctx).Return(newMockIterator())
    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
   },
   expectedClaims: 0,
  },
  {
   name: "unbonded validator skipped",
   setup: func() {
    s.stakingKeeper.EXPECT().MaxValidators(s.ctx).Return(uint32(100))
    s.stakingKeeper.EXPECT().ValidatorsPowerStoreIterator(s.ctx).Return(
     newMockIterator([]byte(valAddr1)),
    )
    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(
     makeValidator(valAddr1, stakingtypes.Unbonded, 10),
    )
   },
   expectedClaims: 0,
  },
 }
}
```

Assert: `len(claimMap) == tc.expectedClaims`, and for power map entries verify `claimMap[key].Power == expectedPower`.

Note: the "unbonded validator skipped" case tests `vote.go:74` — the `if validator.IsBonded()` check. This is important:
Classic didn't test this path explicitly.

- [ ] **Step 4: Run test — verify it passes**

```bash
go test -v -run TestKeeperTestSuite/TestBuildValidatorClaimMap ./x/oracle/keeper/...
```

### TestCountMisses

Tests `vote.go:94-113`. Increments miss counter for validators whose `WinCount < len(voteTargets)`.

No mocks needed — this function only reads/writes `MissCounter` collection.

- [ ] **Step 5: Write `TestCountMisses` with these table cases**

```go
func (s *KeeperTestSuite) TestCountMisses() {
 tests := []struct {
  name              string
  setup             func()
  voteTargets       map[string]math.LegacyDec
  validatorClaimMap map[string]types.Claim
  expectedMisses    map[string]uint64 // valAddr bech32 -> miss count
 }{
  {
   name:  "validator voted all targets — no miss",
   setup: func() {},
   voteTargets: map[string]math.LegacyDec{
    core.MicroKRWDenom: math.LegacyZeroDec(),
   },
   validatorClaimMap: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1), // WinCount=1 == len(targets)
   },
   expectedMisses: map[string]uint64{}, // no miss counter set
  },
  {
   name:  "validator missed — counter incremented from zero",
   setup: func() {},
   voteTargets: map[string]math.LegacyDec{
    core.MicroKRWDenom: math.LegacyZeroDec(),
    core.MicroUSDDenom: math.LegacyZeroDec(),
   },
   validatorClaimMap: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 5, 1, valAddr1), // WinCount=1 < 2 targets
   },
   expectedMisses: map[string]uint64{
    valAddr1.String(): 1,
   },
  },
  {
   name: "existing miss counter — incremented",
   setup: func() {
    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 5))
   },
   voteTargets: map[string]math.LegacyDec{
    core.MicroKRWDenom: math.LegacyZeroDec(),
   },
   validatorClaimMap: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 0, 0, valAddr1), // WinCount=0 < 1
   },
   expectedMisses: map[string]uint64{
    valAddr1.String(): 6,
   },
  },
  {
   name:  "mixed — one voted, one missed",
   setup: func() {},
   voteTargets: map[string]math.LegacyDec{
    core.MicroKRWDenom: math.LegacyZeroDec(),
   },
   validatorClaimMap: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1), // voted
    operStr(valAddr2): types.NewClaim(10, 0, 0, valAddr2),  // missed
   },
   expectedMisses: map[string]uint64{
    valAddr2.String(): 1,
   },
  },
  {
   name:              "no targets — nobody misses",
   setup:             func() {},
   voteTargets:       map[string]math.LegacyDec{},
   validatorClaimMap: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 0, 0, valAddr1),
   },
   expectedMisses: map[string]uint64{}, // WinCount=0 == len(targets)=0
  },
 }
}
```

Assert: for each entry in `expectedMisses`, verify `s.keeper.MissCounter.Get(s.ctx, addr)`. For validators not in
`expectedMisses`, verify no counter was set (Get returns `collections.ErrNotFound`).

- [ ] **Step 6: Run test — verify it passes**

```bash
go test -v -run TestKeeperTestSuite/TestCountMisses ./x/oracle/keeper/...
```

- [ ] **Step 7: Run full keeper suite, verify compilation**

```bash
go build ./x/oracle/... && go test -v ./x/oracle/keeper/...
```

- [ ] **Step 8: Commit**

```bash
git add x/oracle/keeper/vote_test.go
git commit -m "Add table-driven tests for oracle keeper vote.go

Test PickReferenceDenom (threshold filtering, alphabetical tiebreak,
pruning), BuildValidatorClaimMap (bonded/unbonded/maxVals/empty),
and CountMisses (increment/skip/existing counter)."
```

---

## Task 2: `slash_test.go` — SlashAndResetMissCounters

**Files:**

- Create: `x/oracle/keeper/slash_test.go`

Tests `slash.go:13-62`. Iterates all miss counters, computes valid vote rate, slashes & jails bonded validators below
threshold, then clears all counters.

**Key logic paths (from Terra Classic's test, ported to mocks):**

1. Miss count at boundary — NOT slashed (valid vote rate == MinValidPerWindow)
2. Miss count over boundary — slashed and jailed
3. Unbonded validator over boundary — NOT slashed (only bonded get slashed)
4. Already-jailed validator over boundary — NOT slashed

- [ ] **Step 1: Write `TestSlashAndResetMissCounters` with these table cases**

```go
func (s *KeeperTestSuite) TestSlashAndResetMissCounters() {
 tests := []struct {
  name         string
  setup        func()
  expectSlash  bool
  expectJail   bool
 }{
  {
   name: "miss count at boundary — no slash",
   // With VotePeriod=5, SlashWindow=100, MinValidPerWindow=0.05:
   //   votePeriodsPerWindow = 100/5 = 20
   //   maxMisses without slash = 20 - ceil(0.05*20) = 20 - 1 = 19
   //   validVoteRate = (20-19)/20 = 0.05, NOT less than 0.05 → no slash
   // Set miss counter = 19, expect NO slash
   setup: func() {
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)
    s.ctx = sdkCtx

    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    params.SlashWindow = 100
    params.MinValidPerWindow = math.LegacyNewDecWithPrec(5, 2) // 5%
    params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 19))

    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
    // NOTE: Validator() is NOT called because validVoteRate (0.05) is NOT
    // less than MinValidPerWindow (0.05) — the LT check at slash.go:37
    // does not trigger, so the code never enters the slash block.
   },
   expectSlash: false,
   expectJail:  false,
  },
  {
   name: "miss count over boundary — slashed and jailed",
   // missCounter = 20, validVoteRate = (20-20)/20 = 0 < 0.05
   setup: func() {
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)
    s.ctx = sdkCtx

    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    params.SlashWindow = 100
    params.MinValidPerWindow = math.LegacyNewDecWithPrec(5, 2)
    params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 20))

    // IMPORTANT: use makeValidatorWithConsKey so GetConsAddr() works
    val := makeValidatorWithConsKey(valAddr1, stakingtypes.Bonded, 10)
    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(val)
    s.stakingKeeper.EXPECT().Slash(s.ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
    s.stakingKeeper.EXPECT().Jail(s.ctx, gomock.Any())
   },
   expectSlash: true,
   expectJail:  true,
  },
  {
   name: "unbonded validator — not slashed",
   setup: func() {
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)
    s.ctx = sdkCtx

    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    params.SlashWindow = 100
    params.MinValidPerWindow = math.LegacyNewDecWithPrec(5, 2)
    params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 20))

    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(
     makeValidator(valAddr1, stakingtypes.Unbonded, 10),
    )
    // No Slash/Jail expectations — they should NOT be called
   },
   expectSlash: false,
   expectJail:  false,
  },
  {
   name: "jailed validator — not slashed",
   setup: func() {
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)
    s.ctx = sdkCtx

    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    params.SlashWindow = 100
    params.MinValidPerWindow = math.LegacyNewDecWithPrec(5, 2)
    params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 20))

    // Build a bonded + jailed validator
    // makeValidator returns Bonded status, but we need Jailed=true
    // Validator.IsJailed() checks the Jailed field
    val := makeValidator(valAddr1, stakingtypes.Bonded, 10)
    val.Jailed = true
    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(val)
    // No Slash/Jail expectations
   },
   expectSlash: false,
   expectJail:  false,
  },
  {
   name: "miss counter cleared after processing",
   setup: func() {
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)
    s.ctx = sdkCtx

    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    params.SlashWindow = 100
    params.MinValidPerWindow = math.LegacyNewDecWithPrec(5, 2)
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

    // Set counters for both validators
    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 5))
    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr2, 10))

    s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
    s.stakingKeeper.EXPECT().Validator(s.ctx, gomock.Any()).Return(
     makeValidator(valAddr1, stakingtypes.Bonded, 10),
    ).AnyTimes()
   },
   expectSlash: false,
   expectJail:  false,
  },
 }
}
```

For the "miss counter cleared" case, after calling `SlashAndResetMissCounters`, walk `MissCounter` and assert count == 0
(all counters removed).

For all cases, assert `s.Require().NoError(err)`. The gomock controller verifies that `Slash`/`Jail` were called exactly
when expected (unexpected calls cause test failure).

- [ ] **Step 2: Run test — verify it passes**

```bash
go test -v -run TestKeeperTestSuite/TestSlashAndResetMissCounters ./x/oracle/keeper/...
```

- [ ] **Step 3: Compile and run full suite**

```bash
go build ./x/oracle/... && go test -v ./x/oracle/keeper/...
```

- [ ] **Step 4: Commit**

```bash
git add x/oracle/keeper/slash_test.go
git commit -m "Add table-driven tests for oracle keeper slash.go

Test SlashAndResetMissCounters: boundary miss count (no slash),
over-boundary (slash+jail), unbonded (skip), jailed (skip),
and counter clearing."
```

---

## Task 3: `reward_test.go` — RewardBallotWinners

**Files:**

- Create: `x/oracle/keeper/reward_test.go`

Tests `reward.go:18-72`. Distributes oracle pool rewards proportionally to ballot winner weights.

**Key logic paths (from Terra Classic's test):**

1. Normal distribution — two validators with different weights get proportional rewards
2. Empty ballot (ballotPowerSum == 0) — returns error
3. Empty reward pool — returns error
4. Validator not found (nil from `stakingKeeper.Validator`) — skipped, no panic
5. Bank send failure — propagated as error

- [ ] **Step 1: Write `TestRewardBallotWinners` with these table cases**

```go
func (s *KeeperTestSuite) TestRewardBallotWinners() {
 tests := []struct {
  name                     string
  setup                    func()
  votePeriod               int64
  rewardDistributionWindow int64
  ballotWinners            map[string]types.Claim
  expectErr                string
 }{
  {
   name: "two validators — proportional distribution",
   // val1 weight=10, val2 weight=20 → total=30
   // rewardPool = 1_000_000 uark
   // periodRewards = 1_000_000 * 5 / 100 = 50_000
   // val1 gets 50_000 / 30 * 10 = 16_666
   // val2 gets 50_000 / 30 * 20 = 33_333
   setup: func() {
    s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
     mockModuleAccount{addr: sdk.AccAddress{1}},
    )
    s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(
     sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1_000_000))),
    )
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(
     makeValidator(valAddr1, stakingtypes.Bonded, 10),
    )
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(
     makeValidator(valAddr2, stakingtypes.Bonded, 20),
    )
    s.distrKeeper.EXPECT().AllocateTokensToValidator(s.ctx, gomock.Any(), gomock.Any()).Times(2)
    s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
     s.ctx, types.ModuleName, "distribution", gomock.Any(),
    ).Return(nil)
   },
   votePeriod:               5,
   rewardDistributionWindow: 100,
   ballotWinners: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
    operStr(valAddr2): types.NewClaim(20, 20, 1, valAddr2),
   },
  },
  {
   name:                     "empty ballot — error",
   setup:                    func() {},
   votePeriod:               5,
   rewardDistributionWindow: 100,
   ballotWinners:            map[string]types.Claim{},
   expectErr:                "empty ballot",
  },
  {
   name: "zero reward pool — error",
   setup: func() {
    s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
     mockModuleAccount{addr: sdk.AccAddress{1}},
    )
    s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(sdk.Coins{})
   },
   votePeriod:               5,
   rewardDistributionWindow: 100,
   ballotWinners: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
   },
   expectErr: "no rewards",
  },
  {
   name: "nil validator — skipped without panic",
   setup: func() {
    s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
     mockModuleAccount{addr: sdk.AccAddress{1}},
    )
    s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(
     sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1_000_000))),
    )
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil)
    // No AllocateTokensToValidator call expected
    s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
     s.ctx, types.ModuleName, "distribution", gomock.Any(),
    ).Return(nil)
   },
   votePeriod:               5,
   rewardDistributionWindow: 100,
   ballotWinners: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
   },
  },
  {
   name: "bank send fails — error propagated",
   setup: func() {
    s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
     mockModuleAccount{addr: sdk.AccAddress{1}},
    )
    s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(
     sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1_000_000))),
    )
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(
     makeValidator(valAddr1, stakingtypes.Bonded, 10),
    )
    s.distrKeeper.EXPECT().AllocateTokensToValidator(s.ctx, gomock.Any(), gomock.Any())
    s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
     s.ctx, types.ModuleName, "distribution", gomock.Any(),
    ).Return(fmt.Errorf("insufficient funds"))
   },
   votePeriod:               5,
   rewardDistributionWindow: 100,
   ballotWinners: map[string]types.Claim{
    operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
   },
   expectErr: "Failed to send coins",
  },
 }
}
```

Additional import needed: `"fmt"` for the bank send error case.

Assert: if `expectErr != ""`, check `s.Require().ErrorContains(err, tc.expectErr)`. Otherwise
`s.Require().NoError(err)`.

- [ ] **Step 2: Run test — verify it passes**

```bash
go test -v -run TestKeeperTestSuite/TestRewardBallotWinners ./x/oracle/keeper/...
```

- [ ] **Step 3: Compile and run full suite**

```bash
go build ./x/oracle/... && go test -v ./x/oracle/keeper/...
```

- [ ] **Step 4: Commit**

```bash
git add x/oracle/keeper/reward_test.go
git commit -m "Add table-driven tests for oracle keeper reward.go

Test RewardBallotWinners: proportional distribution, empty ballot,
empty reward pool, nil validator handling, and bank send failure."
```

---

## Task 4: `msg_server_test.go` — All 4 message handlers

**Files:**

- Create: `x/oracle/keeper/msg_server_test.go`

This is the largest test file. Tests all 4 message handlers from `msg_server.go`.

### TestMsgAggregateExchangeRatePrevote

Tests `msg_server.go:31-76`. Validates feeder permission, hash format, hash length, stores prevote, emits events.

**Cases from Terra Classic + new ones:**

- [ ] **Step 1: Write `TestMsgAggregateExchangeRatePrevote`**

```go
func (s *KeeperTestSuite) TestMsgAggregateExchangeRatePrevote() {
 salt := "1"
 exchangeRatesStr := "1000.23ukrw,0.29uusd"
 hash := types.GetAggregateVoteHash(salt, exchangeRatesStr, valAddr1)

 tests := []struct {
  name      string
  setup     func()
  msg       *types.MsgAggregateExchangeRatePrevote
  expectErr string
 }{
  {
   name: "valid prevote — success",
   setup: func() {
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRatePrevote{
    Hash:      hex.EncodeToString(hash),
    Feeder:    sdk.AccAddress(valAddr1).String(),
    Validator: valAddr1.String(),
   },
  },
  {
   name:  "invalid validator address",
   setup: func() {},
   msg: &types.MsgAggregateExchangeRatePrevote{
    Hash:      hex.EncodeToString(hash),
    Feeder:    sdk.AccAddress(valAddr1).String(),
    Validator: "invalid",
   },
   expectErr: "decoding bech32 failed",
  },
  {
   name:  "invalid feeder address",
   setup: func() {},
   msg: &types.MsgAggregateExchangeRatePrevote{
    Hash:      hex.EncodeToString(hash),
    Feeder:    "invalid",
    Validator: valAddr1.String(),
   },
   expectErr: "decoding bech32 failed",
  },
  {
   name: "unauthorized feeder",
   setup: func() {
    // accAddr2 is not delegated for valAddr1
   },
   msg: &types.MsgAggregateExchangeRatePrevote{
    Hash:      hex.EncodeToString(hash),
    Feeder:    accAddr2.String(),
    Validator: valAddr1.String(),
   },
   expectErr: types.ErrNoVotingPermission.Error(),
  },
  {
   name: "invalid hash — not hex",
   setup: func() {
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRatePrevote{
    Hash:      "not-hex-zzzz",
    Feeder:    sdk.AccAddress(valAddr1).String(),
    Validator: valAddr1.String(),
   },
   expectErr: types.ErrInvalidHash.Error(),
  },
  {
   name: "invalid hash length — too short",
   setup: func() {
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRatePrevote{
    Hash:      "aabb",
    Feeder:    sdk.AccAddress(valAddr1).String(),
    Validator: valAddr1.String(),
   },
   expectErr: types.ErrInvalidHashLength.Error(),
  },
 }
}
```

Additional imports: `"encoding/hex"`.

Note: The `hex.EncodeToString(hash)` produces a 40-char hex string (20 bytes _ 2), matching `TruncatedHashSize _ 2 =
40`. For "too short" test case, use a valid hex string but wrong length.

For the success case, also verify prevote was stored:

```go
prevote, err := s.keeper.AggregateExchangeRatePrevote.Get(s.ctx, valAddr1)
s.Require().NoError(err)
s.Require().Equal(valAddr1.String(), prevote.Voter)
```

- [ ] **Step 2: Run test — verify it passes**

### TestMsgAggregateExchangeRateVote

Tests `msg_server.go:78-163`. This is the most complex handler — validates feeder, exchange rates string, salt length,
reveal period timing, hash verification, denom whitelist.

- [ ] **Step 3: Write `TestMsgAggregateExchangeRateVote`**

```go
func (s *KeeperTestSuite) TestMsgAggregateExchangeRateVote() {
 salt := "1"
 exchangeRatesStr := "1000.23ukrw,0.29uusd"
 hash := types.GetAggregateVoteHash(salt, exchangeRatesStr, valAddr1)

 // Helper: store a prevote at block 0 so reveal at block VotePeriod is valid
 storePrevote := func(votePeriod uint64) {
  prevote := types.NewAggregateExchangeRatePrevote(hash, valAddr1, 0)
  s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote))

  params, _ := s.keeper.Params.Get(s.ctx)
  params.VotePeriod = votePeriod
  s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

  // Set block height to vote period (reveal window)
  sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(int64(votePeriod))
  s.ctx = sdkCtx
 }

 // Helper: register denoms in whitelist (TobinTax)
 registerDenoms := func(denoms ...string) {
  for _, d := range denoms {
   s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, d, math.LegacyNewDecWithPrec(25, 4)))
  }
 }

 tests := []struct {
  name      string
  setup     func()
  msg       *types.MsgAggregateExchangeRateVote
  expectErr string
 }{
  {
   name: "valid vote — success",
   setup: func() {
    // IMPORTANT: storePrevote modifies s.ctx (WithBlockHeight), so
    // Validator mock must come AFTER to use the final context.
    storePrevote(5)
    registerDenoms(core.MicroKRWDenom, core.MicroUSDDenom)
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          salt,
    ExchangeRates: exchangeRatesStr,
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
  },
  {
   name:  "empty exchange rates",
   setup: func() {
    // Empty rates fails before ValidateFeeder, but feeder/validator parsing happens first
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          salt,
    ExchangeRates: "",
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: "must provide at least one oracle exchange rate",
  },
  {
   name:  "salt too long",
   setup: func() {
    storePrevote(5)
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          "12345",
    ExchangeRates: exchangeRatesStr,
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: types.ErrInvalidSaltLength.Error(),
  },
  {
   name:  "salt too short (empty)",
   setup: func() {
    storePrevote(5)
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          "",
    ExchangeRates: exchangeRatesStr,
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: types.ErrInvalidSaltLength.Error(),
  },
  {
   name: "no prevote exists — error",
   setup: func() {
    // Don't call storePrevote — no prevote in store
    // ValidateFeeder passes, then AggregateExchangeRatePrevote.Get fails
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          salt,
    ExchangeRates: exchangeRatesStr,
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: "not found",
  },
  {
   name: "wrong reveal period — same period as prevote",
   setup: func() {
    // Prevote at block 0, vote also at block 3 (same period: 3/5 == 0/5 == 0)
    prevote := types.NewAggregateExchangeRatePrevote(hash, valAddr1, 0)
    s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote))
    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(3)
    s.ctx = sdkCtx
    // Validator mock AFTER context modification
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          salt,
    ExchangeRates: exchangeRatesStr,
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: types.ErrRevealPeriodMissMatch.Error(),
  },
  {
   name: "wrong reveal period — two periods later",
   setup: func() {
    prevote := types.NewAggregateExchangeRatePrevote(hash, valAddr1, 0)
    s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote))
    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
    // Block height 10 → period 2, prevote period 0 → diff = 2 ≠ 1
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
    s.ctx = sdkCtx
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          salt,
    ExchangeRates: exchangeRatesStr,
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: types.ErrRevealPeriodMissMatch.Error(),
  },
  {
   name: "unknown denom in vote — rejected",
   setup: func() {
    unknownRates := "100.0ufoo"
    unknownHash := types.GetAggregateVoteHash(salt, unknownRates, valAddr1)
    prevote := types.NewAggregateExchangeRatePrevote(unknownHash, valAddr1, 0)
    s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote))
    params, _ := s.keeper.Params.Get(s.ctx)
    params.VotePeriod = 5
    s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
    sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(5)
    s.ctx = sdkCtx
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
    // ufoo is not in TobinTax whitelist
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          salt,
    ExchangeRates: "100.0ufoo",
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: types.ErrUnknownDenom.Error(),
  },
  {
   name: "hash mismatch — wrong salt",
   setup: func() {
    storePrevote(5)
    registerDenoms(core.MicroKRWDenom, core.MicroUSDDenom)
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgAggregateExchangeRateVote{
    Salt:          "9",  // different salt → different hash
    ExchangeRates: exchangeRatesStr,
    Feeder:        sdk.AccAddress(valAddr1).String(),
    Validator:     valAddr1.String(),
   },
   expectErr: types.ErrVerificationFailed.Error(),
  },
 }
}
```

For the success case, additionally verify:

- Vote is stored: `s.keeper.AggregateExchangeRateVote.Get(s.ctx, valAddr1)` succeeds
- Prevote is removed: `s.keeper.AggregateExchangeRatePrevote.Get(s.ctx, valAddr1)` returns `collections.ErrNotFound`

- [ ] **Step 4: Run test — verify it passes**

### TestMsgDelegateFeedConsent

Tests `msg_server.go:166-201`.

- [ ] **Step 5: Write `TestMsgDelegateFeedConsent`**

```go
func (s *KeeperTestSuite) TestMsgDelegateFeedConsent() {
 tests := []struct {
  name      string
  setup     func()
  msg       *types.MsgDelegateFeedConsent
  expectErr string
 }{
  {
   name: "valid delegation",
   setup: func() {
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
   },
   msg: &types.MsgDelegateFeedConsent{
    Operator: valAddr1.String(),
    Delegate: accAddr1.String(),
   },
  },
  {
   name:  "invalid operator address",
   setup: func() {},
   msg: &types.MsgDelegateFeedConsent{
    Operator: "invalid",
    Delegate: accAddr1.String(),
   },
   expectErr: "decoding bech32 failed",
  },
  {
   name:  "invalid delegate address",
   setup: func() {},
   msg: &types.MsgDelegateFeedConsent{
    Operator: valAddr1.String(),
    Delegate: "invalid",
   },
   expectErr: "decoding bech32 failed",
  },
  {
   name: "operator not a validator",
   setup: func() {
    s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil)
   },
   msg: &types.MsgDelegateFeedConsent{
    Operator: valAddr1.String(),
    Delegate: accAddr1.String(),
   },
   expectErr: "no validator found",
  },
 }
}
```

For success case, verify delegation was stored:

```go
delegate, err := s.keeper.FeederDelegation.Get(s.ctx, valAddr1)
s.Require().NoError(err)
s.Require().Equal(accAddr1, delegate)
```

- [ ] **Step 6: Run test — verify it passes**

### TestMsgUpdateParams

Tests `msg_server.go:204-218`.

- [ ] **Step 7: Write `TestMsgUpdateParams`**

```go
func (s *KeeperTestSuite) TestMsgUpdateParams() {
 authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

 tests := []struct {
  name      string
  msg       *types.MsgUpdateParams
  expectErr string
 }{
  {
   name: "valid update",
   msg: &types.MsgUpdateParams{
    Authority: authority,
    Params:    types.DefaultParams(),
   },
  },
  {
   name: "wrong authority",
   msg: &types.MsgUpdateParams{
    Authority: accAddr1.String(),
    Params:    types.DefaultParams(),
   },
   expectErr: "invalid authority",
  },
  {
   name: "invalid params — zero vote period",
   msg: func() *types.MsgUpdateParams {
    p := types.DefaultParams()
    p.VotePeriod = 0
    return &types.MsgUpdateParams{Authority: authority, Params: p}
   }(),
   expectErr: "VotePeriod must be > 0",
  },
 }
}
```

Additional imports: `authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"`,
`govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"`.

- [ ] **Step 8: Run all msg_server tests, compile, full suite**

```bash
go build ./x/oracle/... && go test -v -run "TestKeeperTestSuite/TestMsg" ./x/oracle/keeper/...
```

- [ ] **Step 9: Commit**

```bash
git add x/oracle/keeper/msg_server_test.go
git commit -m "Add table-driven tests for oracle keeper msg_server.go

Test AggregateExchangeRatePrevote (valid, invalid hash/length, unauthorized),
AggregateExchangeRateVote (valid, empty rates, salt bounds, no prevote,
reveal period, unknown denom, hash mismatch), DelegateFeedConsent (valid,
invalid addrs, non-validator), UpdateParams (valid, wrong authority, invalid)."
```

---

## Task 5: `grpc_query_test.go` — All 13 query RPCs

**Files:**

- Create: `x/oracle/keeper/grpc_query_test.go`

These tests use the `s.queryClient` (gRPC test client) already wired in `SetupTest`. They're simpler than msg_server
tests — mostly set state, query, verify response.

**Important:** The query client goes through the full gRPC stack, testing routing + serialization. Use `s.queryClient`,
not `queryServer` directly.

**Nil request caveat:** gRPC clients panic when marshalling nil proto messages. For "nil request" test cases below, use
the query server directly instead of the gRPC client:

```go
queryServer := keeper.NewQueryServerImpl(s.keeper)
_, err := queryServer.ExchangeRate(s.ctx, nil)
s.Require().Error(err)
```

When you see `req: nil` in the table cases below, the test runner must detect nil and call the query server directly.
Simplest approach: handle it in the test loop body:

```go
for _, tc := range tests {
    s.Run(tc.name, func() {
        tc.setup()
        var err error
        if tc.req == nil {
            queryServer := keeper.NewQueryServerImpl(s.keeper)
            _, err = queryServer.ExchangeRate(s.ctx, nil)
        } else {
            resp, e := s.queryClient.ExchangeRate(s.ctx, tc.req)
            err = e
            if err == nil { /* verify resp */ }
        }
        // assert err
    })
}
```

- [ ] **Step 1: Write `TestQueryParams`**

```go
func (s *KeeperTestSuite) TestQueryParams() {
 resp, err := s.queryClient.Params(s.ctx, &types.QueryParamsRequest{})
 s.Require().NoError(err)
 s.Require().Equal(types.DefaultParams(), resp.Params)
}
```

- [ ] **Step 2: Write `TestQueryExchangeRate`**

```go
func (s *KeeperTestSuite) TestQueryExchangeRate() {
 tests := []struct {
  name      string
  setup     func()
  req       *types.QueryExchangeRateRequest
  expected  math.LegacyDec
  expectErr string
 }{
  {
   name: "known denom",
   setup: func() {
    s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDec(1700)))
   },
   req:      &types.QueryExchangeRateRequest{Denom: core.MicroKRWDenom},
   expected: math.LegacyNewDec(1700),
  },
  {
   name:      "nil request",
   setup:     func() {},
   req:       nil,
   expectErr: "invalid request",
  },
  {
   name:      "empty denom",
   setup:     func() {},
   req:       &types.QueryExchangeRateRequest{Denom: ""},
   expectErr: "empty denom",
  },
  {
   name:      "unknown denom",
   setup:     func() {},
   req:       &types.QueryExchangeRateRequest{Denom: "ufoo"},
   expectErr: "ufoo",
  },
 }
}
```

- [ ] **Step 3: Write `TestQueryExchangeRates`**

```go
func (s *KeeperTestSuite) TestQueryExchangeRates() {
 tests := []struct {
  name     string
  setup    func()
  expected int // number of exchange rates
 }{
  {
   name:     "no rates",
   setup:    func() {},
   expected: 0,
  },
  {
   name: "two rates",
   setup: func() {
    s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDec(1700)))
    s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDec(1)))
   },
   expected: 2,
  },
 }
}
```

- [ ] **Step 4: Write `TestQueryTobinTax` and `TestQueryTobinTaxes`**

```go
func (s *KeeperTestSuite) TestQueryTobinTax() {
 tests := []struct {
  name      string
  setup     func()
  req       *types.QueryTobinTaxRequest
  expected  math.LegacyDec
  expectErr string
 }{
  {
   name: "known denom",
   setup: func() {
    s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyOneDec()))
   },
   req:      &types.QueryTobinTaxRequest{Denom: core.MicroKRWDenom},
   expected: math.LegacyOneDec(),
  },
  {
   name:      "nil request",
   setup:     func() {},
   req:       nil,
   expectErr: "invalid request",
  },
  {
   name:      "empty denom",
   setup:     func() {},
   req:       &types.QueryTobinTaxRequest{Denom: ""},
   expectErr: "empty denom",
  },
  {
   name:      "unknown denom",
   setup:     func() {},
   req:       &types.QueryTobinTaxRequest{Denom: "ufoo"},
   expectErr: "ufoo",
  },
 }
}

func (s *KeeperTestSuite) TestQueryTobinTaxes() {
 tests := []struct {
  name     string
  setup    func()
  expected int
 }{
  {
   name:     "no taxes",
   setup:    func() {},
   expected: 0,
  },
  {
   name: "two taxes",
   setup: func() {
    s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyOneDec()))
    s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroSDRDenom, math.LegacyNewDecWithPrec(123, 2)))
   },
   expected: 2,
  },
 }
}
```

- [ ] **Step 5: Write `TestQueryActives` and `TestQueryVoteTargets`**

```go
func (s *KeeperTestSuite) TestQueryActives() {
 tests := []struct {
  name     string
  setup    func()
  expected int
 }{
  {
   name:     "no exchange rates",
   setup:    func() {},
   expected: 0,
  },
  {
   name: "three active denoms",
   setup: func() {
    for _, d := range []string{core.MicroKRWDenom, core.MicroUSDDenom, core.MicroSDRDenom} {
     s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, d, math.LegacyNewDec(1)))
    }
   },
   expected: 3,
  },
 }
}

func (s *KeeperTestSuite) TestQueryVoteTargets() {
 tests := []struct {
  name     string
  setup    func()
  expected int
 }{
  {
   name:     "no tobin taxes",
   setup:    func() {},
   expected: 0,
  },
  {
   name: "two vote targets",
   setup: func() {
    s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, "denom1", math.LegacyOneDec()))
    s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, "denom2", math.LegacyOneDec()))
   },
   expected: 2,
  },
 }
}
```

- [ ] **Step 6: Write `TestQueryFeederDelegation` and `TestQueryMissCounter`**

```go
func (s *KeeperTestSuite) TestQueryFeederDelegation() {
 tests := []struct {
  name      string
  setup     func()
  req       *types.QueryFeederDelegationRequest
  expected  string
  expectErr string
 }{
  {
   name: "delegation set",
   setup: func() {
    s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
   },
   req:      &types.QueryFeederDelegationRequest{ValidatorAddr: valAddr1.String()},
   expected: accAddr1.String(),
  },
  {
   name:      "nil request",
   setup:     func() {},
   req:       nil,
   expectErr: "invalid request",
  },
  {
   name:      "invalid validator address",
   setup:     func() {},
   req:       &types.QueryFeederDelegationRequest{ValidatorAddr: "invalid"},
   expectErr: "decoding bech32 failed",
  },
  {
   name:     "no delegation — defaults to validator address",
   setup:    func() {},
   req:      &types.QueryFeederDelegationRequest{ValidatorAddr: valAddr1.String()},
   expected: sdk.AccAddress(valAddr1).String(),
  },
 }
}

func (s *KeeperTestSuite) TestQueryMissCounter() {
 tests := []struct {
  name      string
  setup     func()
  req       *types.QueryMissCounterRequest
  expected  uint64
  expectErr string
 }{
  {
   name: "counter set",
   setup: func() {
    s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 42))
   },
   req:      &types.QueryMissCounterRequest{ValidatorAddr: valAddr1.String()},
   expected: 42,
  },
  {
   name:      "nil request",
   setup:     func() {},
   req:       nil,
   expectErr: "invalid request",
  },
  {
   name:     "no counter — returns zero",
   setup:    func() {},
   req:      &types.QueryMissCounterRequest{ValidatorAddr: valAddr1.String()},
   expected: 0,
  },
 }
}
```

- [ ] **Step 7: Write `TestQueryAggregatePrevote`, `TestQueryAggregatePrevotes`, `TestQueryAggregateVote`,
      `TestQueryAggregateVotes`**

```go
func (s *KeeperTestSuite) TestQueryAggregatePrevote() {
 tests := []struct {
  name      string
  setup     func()
  req       *types.QueryAggregatePrevoteRequest
  expectErr string
 }{
  {
   name: "prevote exists",
   setup: func() {
    prevote := types.NewAggregateExchangeRatePrevote(types.AggregateVoteHash{}, valAddr1, 0)
    s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote))
   },
   req: &types.QueryAggregatePrevoteRequest{ValidatorAddr: valAddr1.String()},
  },
  {
   name:      "nil request",
   setup:     func() {},
   req:       nil,
   expectErr: "invalid request",
  },
  {
   name:      "not found",
   setup:     func() {},
   req:       &types.QueryAggregatePrevoteRequest{ValidatorAddr: valAddr1.String()},
   expectErr: valAddr1.String(),
  },
 }
}

func (s *KeeperTestSuite) TestQueryAggregatePrevotes() {
 // Simple: store 2 prevotes, query all, verify count == 2
 prevote1 := types.NewAggregateExchangeRatePrevote(types.AggregateVoteHash{}, valAddr1, 0)
 prevote2 := types.NewAggregateExchangeRatePrevote(types.AggregateVoteHash{}, valAddr2, 0)
 s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote1))
 s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr2, prevote2))

 resp, err := s.queryClient.AggregatePrevotes(s.ctx, &types.QueryAggregatePrevotesRequest{})
 s.Require().NoError(err)
 s.Require().Len(resp.AggregatePrevotes, 2)
}

func (s *KeeperTestSuite) TestQueryAggregateVote() {
 tests := []struct {
  name      string
  setup     func()
  req       *types.QueryAggregateVoteRequest
  expectErr string
 }{
  {
   name: "vote exists",
   setup: func() {
    vote := types.NewAggregateExchangeRateVote(
     types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyOneDec()}},
     valAddr1,
    )
    s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, vote))
   },
   req: &types.QueryAggregateVoteRequest{ValidatorAddr: valAddr1.String()},
  },
  {
   name:      "nil request",
   setup:     func() {},
   req:       nil,
   expectErr: "invalid request",
  },
  {
   name:      "not found",
   setup:     func() {},
   req:       &types.QueryAggregateVoteRequest{ValidatorAddr: valAddr1.String()},
   expectErr: valAddr1.String(),
  },
 }
}

func (s *KeeperTestSuite) TestQueryAggregateVotes() {
 vote1 := types.NewAggregateExchangeRateVote(
  types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyOneDec()}}, valAddr1,
 )
 vote2 := types.NewAggregateExchangeRateVote(
  types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyOneDec()}}, valAddr2,
 )
 s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, vote1))
 s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr2, vote2))

 resp, err := s.queryClient.AggregateVotes(s.ctx, &types.QueryAggregateVotesRequest{})
 s.Require().NoError(err)
 s.Require().Len(resp.AggregateVotes, 2)
}
```

- [ ] **Step 8: Run all query tests, compile, full suite**

```bash
go build ./x/oracle/... && go test -v -run "TestKeeperTestSuite/TestQuery" ./x/oracle/keeper/...
```

- [ ] **Step 9: Commit**

```bash
git add x/oracle/keeper/grpc_query_test.go
git commit -m "Add table-driven tests for oracle keeper grpc_query.go

Test all 13 query RPCs via gRPC test client: Params, ExchangeRate,
ExchangeRates, TobinTax, TobinTaxes, Actives, VoteTargets,
FeederDelegation, MissCounter, AggregatePrevote, AggregatePrevotes,
AggregateVote, AggregateVotes. Covers nil requests, not-found,
empty/invalid args, and success paths."
```

---

## Task 6: Final verification

- [ ] **Step 1: Run the full test suite**

```bash
go test -v -count=1 ./x/oracle/keeper/...
```

- [ ] **Step 2: Verify no test file is missing**

Every `.go` file in `x/oracle/keeper/` that defines exported functions should have a corresponding `_test.go`:

- `keeper.go` → `keeper_test.go` (existing)
- `ballot.go` → `ballot_test.go` (existing)
- `genesis.go` → `genesis_test.go` (existing)
- `abci.go` → `abci_test.go` (existing)
- `vote.go` → `vote_test.go` (Task 1)
- `slash.go` → `slash_test.go` (Task 2)
- `reward.go` → `reward_test.go` (Task 3)
- `msg_server.go` → `msg_server_test.go` (Task 4)
- `grpc_query.go` → `grpc_query_test.go` (Task 5)

---

## Coverage Gap Analysis vs Terra Classic

| Terra Classic test                                                                                              | Noah equivalent                                                          | Status                                                                 |
| --------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| `keeper_test.go` — ExchangeRate, Params, FeederDelegation, MissCounter, Prevote, Vote, TobinTax, ValidateFeeder | `keeper_test.go`                                                         | Already exists                                                         |
| `ballot_test.go` — OrganizeAggregate, ClearBallots, ApplyWhitelist                                              | `ballot_test.go`                                                         | Already exists                                                         |
| `querier_test.go` — all 13 query RPCs                                                                           | `grpc_query_test.go`                                                     | Task 5                                                                 |
| `msg_server_test.go` — FeederDelegation flow, PrevoteVote flow                                                  | `msg_server_test.go`                                                     | Task 4                                                                 |
| `reward_test.go` — RewardBallotWinners                                                                          | `reward_test.go`                                                         | Task 3                                                                 |
| `slash_test.go` — 4 slash scenarios                                                                             | `slash_test.go`                                                          | Task 2                                                                 |
| `vote_target_test.go` — GetVoteTargets, IsVoteTarget                                                            | N/A                                                                      | Not needed; these are simple collection reads (covered by query tests) |
| `legacy_querier_test.go` — Tendermint legacy querier                                                            | N/A                                                                      | Not needed; v0.53 removed legacy querier                               |
| N/A                                                                                                             | `vote_test.go` — PickReferenceDenom, BuildValidatorClaimMap, CountMisses | Task 1 (NEW — Terra tested these inline in ABCI tests, not separately) |
| N/A                                                                                                             | `msg_server_test.go` — UpdateParams                                      | Task 4 (NEW — Terra didn't have UpdateParams msg)                      |
