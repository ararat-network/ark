# Asset Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the `AssetLocks` inverse dependency index and replace it with asset-owned `ReferenceState`, a governance `MsgSetReference` with atomic rebase executors, and a dormant priced-live view plus membership epoch.

**Architecture:** Spec: `docs/superpowers/specs/2026-07-29-asset-consolidation-design.md`. All work is inside `x/asset` (plus its protos and the plan doc); the module is dormant (not wired into `app/`), so nothing outside `x/asset` compiles against it except generated code. Commits stay green by sequencing: additive proto → new Go features → guard rewrites → Go lock deletion → destructive proto.

**Tech Stack:** cosmos-sdk v0.54.2, cosmossdk.io/collections, gogo+pulsar dual proto-gen (`make proto-gen`, Docker), testify suites for keeper tests, plain table-driven functions for types tests, gomock (`go.uber.org/mock`).

**Conventions that bind every task** (from CLAUDE.md): table-driven tests with `t.Run`; mutate pattern from `DefaultGenesisState()`; British spelling in test names; no `ValidateBasic` on msgs — validate in handlers/keeper; detailed commit messages, no Co-Authored-By lines.

---

### Task 1: Additive proto surface

Add every new proto element without deleting anything, so generated lock types survive until Go stops referencing them (Task 9).

**Files:**
- Modify: `proto/ark/asset/v1/asset.proto`
- Modify: `proto/ark/asset/v1/genesis.proto`
- Modify: `proto/ark/asset/v1/event.proto`
- Modify: `proto/ark/asset/v1/tx.proto`
- Modify: `proto/ark/asset/v1/query.proto`

- [ ] **Step 1: Add ReferenceState to asset.proto**

Insert after the `OracleTargetTransition` message (line 114):

```protobuf
// ReferenceState names the single protocol reference denomination shared by
// Market's base pool and Treasury's reference tax cap, plus the fallback
// promoted when the reference asset is suspended.
message ReferenceState {
  // reference_denom prices Market's base pool and denominates Treasury's
  // reference tax cap. Empty only before first configuration.
  string reference_denom = 1;
  // fallback_denom is promoted when the reference asset is suspended. It may
  // be empty; suspending the reference asset then fails atomically.
  string fallback_denom = 2;
}
```

- [ ] **Step 2: Add genesis fields**

In `genesis.proto`, append to `GenesisState` after `emergency_actions` (field 6):

```protobuf
  // reference names the shared protocol reference denomination and its
  // fallback. It may be empty only while no asset is eligible to serve it.
  ReferenceState reference = 7
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
  // priced_live_version is the membership epoch consumers compare against;
  // it advances whenever the priced-live denom set changes.
  uint64 priced_live_version = 8 [ (amino.dont_omitempty) = true ];
```

- [ ] **Step 3: Add events**

In `event.proto`, append at end of file:

```protobuf
// EventReferenceUpdated is emitted after every ReferenceState write.
message EventReferenceUpdated {
  string reference_denom = 1;
  string fallback_denom = 2;
}
```

And add to `EventEmergencyActionExecuted` (after `fallback_denoms = 5`, which Task 10 deletes):

```protobuf
  // replacement_denom is the reference promoted in place of the suspended
  // asset, empty when the asset was not the protocol reference.
  string replacement_denom = 6;
```

- [ ] **Step 4: Add MsgSetReference**

In `tx.proto`, add to `service Msg` after the `ReactivateAsset` rpc:

```protobuf
  // SetReference replaces the shared protocol reference denomination and its
  // fallback, rebasing consumer reference-unit state when the reference
  // denomination changes.
  rpc SetReference(MsgSetReference) returns (MsgSetReferenceResponse) {}
```

And append after `MsgReactivateAssetResponse`:

```protobuf
// MsgSetReference replaces the shared protocol reference denomination and its
// fallback. Changing a non-empty reference denomination rebases Market and
// Treasury reference-unit state atomically in the same transaction.
message MsgSetReference {
  option (cosmos.msg.v1.signer) = "authority";
  option (amino.name) = "ark/x/asset/MsgSetReference";

  string authority = 1 [ (cosmos_proto.scalar) = "cosmos.AddressString" ];
  string reference_denom = 2;
  // fallback_denom may be empty; suspending the reference asset then fails
  // until governance configures one.
  string fallback_denom = 3;
}

// MsgSetReferenceResponse is the response type for MsgSetReference.
message MsgSetReferenceResponse {}
```

- [ ] **Step 5: Add Reference query**

In `query.proto`, add to `service Query` after the `Assets` rpc:

```protobuf
  // Reference returns the shared protocol reference state.
  rpc Reference(QueryReferenceRequest) returns (QueryReferenceResponse) {
    option (cosmos.query.v1.module_query_safe) = true;
    option (google.api.http).get = "/ark/asset/v1/reference";
  }
```

And append after `QueryAssetsResponse`:

```protobuf
// QueryReferenceRequest is the request type for Query/Reference.
message QueryReferenceRequest {}

// QueryReferenceResponse returns the shared protocol reference state.
message QueryReferenceResponse {
  ReferenceState reference = 1
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
}
```

- [ ] **Step 6: Generate and verify**

Run: `make proto-format && make proto-lint && make proto-gen`
Expected: lint passes; generation rewrites `x/asset/types/*.pb.go` and `api/ark/asset/v1/*.pulsar.go`.

Run: `go build ./... && go test ./x/asset/...`
Expected: PASS (changes are purely additive).

- [ ] **Step 7: Commit**

```bash
git add proto/ark/asset/v1 x/asset/types api/ark/asset/v1
git commit -m "proto(asset): add ReferenceState, priced-live epoch, MsgSetReference, Reference query

Additive surface for the asset consolidation design. ReferenceState names
the shared protocol reference and fallback; genesis carries it plus the
priced-live membership epoch; MsgSetReference and the Reference query expose
it; EventReferenceUpdated reports writes and EventEmergencyActionExecuted
gains the single replacement_denom that supersedes fallback_denoms. Lock
messages are untouched here so the Go migration can land green before the
destructive proto change removes them."
```

---

### Task 2: types — ReferenceState validation and genesis checks

**Files:**
- Create: `x/asset/types/reference.go`
- Create: `x/asset/types/reference_test.go`
- Modify: `x/asset/types/genesis.go`
- Modify: `x/asset/types/genesis_test.go`

Do NOT touch `DefaultGenesisState()` yet — the keeper does not import the new fields until Task 3, and changing defaults first would break the keeper genesis round-trip test.

- [ ] **Step 1: Write failing types tests**

`x/asset/types/reference_test.go`:

```go
package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	chain "ark/pkg/chain"
	"ark/x/asset/types"
)

func TestReferenceStateValidate(t *testing.T) {
	valid := types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}

	testCases := []struct {
		name     string
		mutate   func(r *types.ReferenceState)
		errorMsg string
	}{
		{name: "valid pair", mutate: func(_ *types.ReferenceState) {}},
		{
			name: "valid empty state",
			mutate: func(r *types.ReferenceState) {
				r.ReferenceDenom = ""
				r.FallbackDenom = ""
			},
		},
		{
			name:   "valid reference without fallback",
			mutate: func(r *types.ReferenceState) { r.FallbackDenom = "" },
		},
		{
			name:     "fallback without reference",
			mutate:   func(r *types.ReferenceState) { r.ReferenceDenom = "" },
			errorMsg: "requires a reference denom",
		},
		{
			name:     "invalid reference denom",
			mutate:   func(r *types.ReferenceState) { r.ReferenceDenom = "usdr" },
			errorMsg: "reference denom",
		},
		{
			name:     "native reference denom",
			mutate:   func(r *types.ReferenceState) { r.ReferenceDenom = chain.NoahBaseDenom },
			errorMsg: "must not be native denom",
		},
		{
			name:     "invalid fallback denom",
			mutate:   func(r *types.ReferenceState) { r.FallbackDenom = "uusd" },
			errorMsg: "reference fallback denom",
		},
		{
			name:     "native fallback denom",
			mutate:   func(r *types.ReferenceState) { r.FallbackDenom = chain.NoahBaseDenom },
			errorMsg: "must not be native denom",
		},
		{
			name:     "fallback equals reference",
			mutate:   func(r *types.ReferenceState) { r.FallbackDenom = chain.SDRBaseDenom },
			errorMsg: "must differ from reference denom",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reference := valid
			tc.mutate(&reference)
			err := reference.Validate()
			if tc.errorMsg == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.errorMsg)
		})
	}
}
```

Append to `x/asset/types/genesis_test.go` (match the file's existing mutate-pattern style; `pendingReferenceAsset` builds a registered but never-activated asset so eligibility failures are reachable without breaking target-state validation):

```go
func pendingReferenceAsset(denom string) types.Asset {
	display := denom[1:]
	return types.Asset{
		Denom: denom,
		Metadata: banktypes.Metadata{
			Description: "Test asset.",
			DenomUnits: []*banktypes.DenomUnit{
				{Denom: denom, Exponent: 0},
				{Denom: display, Exponent: chain.NativeDisplayExponent},
			},
			Base:    denom,
			Display: display,
			Name:    "Test",
			Symbol:  "TST",
		},
		Status:         types.AssetStatus_ASSET_STATUS_PENDING,
		Version:        1,
		OracleRequired: true,
	}
}

func TestGenesisStateValidateReference(t *testing.T) {
	testCases := []struct {
		name     string
		mutate   func(gs *types.GenesisState)
		errorMsg string
	}{
		{
			name: "valid configured reference",
			mutate: func(gs *types.GenesisState) {
				gs.Reference = types.ReferenceState{
					ReferenceDenom: chain.SDRBaseDenom,
					FallbackDenom:  chain.USDBaseDenom,
				}
			},
		},
		{name: "valid empty reference", mutate: func(_ *types.GenesisState) {}},
		{
			name: "invalid reference state",
			mutate: func(gs *types.GenesisState) {
				gs.Reference = types.ReferenceState{FallbackDenom: chain.USDBaseDenom}
			},
			errorMsg: "requires a reference denom",
		},
		{
			name: "unregistered reference denom",
			mutate: func(gs *types.GenesisState) {
				gs.Reference = types.ReferenceState{ReferenceDenom: "azzy"}
			},
			errorMsg: "names unregistered asset",
		},
		{
			name: "reference without live pricing",
			mutate: func(gs *types.GenesisState) {
				gs.Assets = append(gs.Assets, pendingReferenceAsset("azzz"))
				gs.Reference = types.ReferenceState{ReferenceDenom: "azzz"}
			},
			errorMsg: "without live Oracle pricing",
		},
		{
			name: "fallback without live pricing",
			mutate: func(gs *types.GenesisState) {
				gs.Assets = append(gs.Assets, pendingReferenceAsset("azzz"))
				gs.Reference = types.ReferenceState{
					ReferenceDenom: chain.SDRBaseDenom,
					FallbackDenom:  "azzz",
				}
			},
			errorMsg: "without live Oracle pricing",
		},
		{
			name:     "zero priced-live version",
			mutate:   func(gs *types.GenesisState) { gs.PricedLiveVersion = 0 },
			errorMsg: "priced-live version must be positive",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := types.DefaultGenesisState()
			gs.PricedLiveVersion = 1
			tc.mutate(gs)
			err := gs.Validate()
			if tc.errorMsg == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.errorMsg)
		})
	}
}
```

Note the `gs.PricedLiveVersion = 1` line: `DefaultGenesisState` does not set it until Task 3, and Validate requires it positive. Keep the line after Task 3 too — it is then redundant but harmless.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./x/asset/types/ -run 'TestReferenceStateValidate|TestGenesisStateValidateReference' -v`
Expected: FAIL — `reference.Validate` undefined / validation messages missing.

- [ ] **Step 3: Implement**

Create `x/asset/types/reference.go`:

```go
package types

import (
	"fmt"

	chain "ark/pkg/chain"
)

// Validate checks reference and fallback identities in isolation. Eligibility
// against registry state belongs to the keeper and genesis validation.
func (r ReferenceState) Validate() error {
	if r.ReferenceDenom == "" {
		if r.FallbackDenom != "" {
			return fmt.Errorf(
				"reference fallback %s requires a reference denom",
				r.FallbackDenom,
			)
		}
		return nil
	}
	if err := chain.ValidateNativeBaseDenom(r.ReferenceDenom); err != nil {
		return fmt.Errorf("reference denom %w", err)
	}
	if r.ReferenceDenom == chain.NoahBaseDenom {
		return fmt.Errorf(
			"reference denom must not be native denom %s",
			r.ReferenceDenom,
		)
	}
	if r.FallbackDenom == "" {
		return nil
	}
	if err := chain.ValidateNativeBaseDenom(r.FallbackDenom); err != nil {
		return fmt.Errorf("reference fallback denom %w", err)
	}
	if r.FallbackDenom == chain.NoahBaseDenom {
		return fmt.Errorf(
			"reference fallback denom must not be native denom %s",
			r.FallbackDenom,
		)
	}
	if r.FallbackDenom == r.ReferenceDenom {
		return fmt.Errorf(
			"reference fallback must differ from reference denom %s",
			r.ReferenceDenom,
		)
	}

	return nil
}
```

In `x/asset/types/genesis.go`, inside `GenesisState.Validate`, insert after the `active` map is built (after the loop that fills `staged`, before the `for denom := range active` existence loop):

```go
	if err := gs.Reference.Validate(); err != nil {
		return err
	}
	if gs.PricedLiveVersion == 0 {
		return fmt.Errorf("priced-live version must be positive")
	}
	for _, denom := range []string{
		gs.Reference.ReferenceDenom,
		gs.Reference.FallbackDenom,
	} {
		if denom == "" {
			continue
		}
		asset, exists := assets[denom]
		if !exists {
			return fmt.Errorf("reference state names unregistered asset %s", denom)
		}
		if !asset.IsPriceable() || !active[denom] {
			return fmt.Errorf(
				"reference state names asset %s without live Oracle pricing",
				denom,
			)
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./x/asset/types/... && go test ./x/asset/...`
Expected: PASS (default genesis still has an empty reference and the keeper ignores the new fields, so nothing else changes).

- [ ] **Step 5: Commit**

```bash
git add x/asset/types/reference.go x/asset/types/reference_test.go x/asset/types/genesis.go x/asset/types/genesis_test.go
git commit -m "feat(asset): validate ReferenceState and genesis reference consistency

ReferenceState.Validate checks denom identities in isolation: a fallback
requires a reference, both must be governable native base denoms, and the
fallback must differ. Genesis validation additionally requires any named
denom to be a registered asset with live Oracle pricing (priceable status
and materialized target membership) and the priced-live epoch positive."
```

---

### Task 3: keeper — collections, priced-live view, epoch bumps, genesis wiring

**Files:**
- Modify: `x/asset/types/keys.go`
- Modify: `x/asset/types/genesis.go` (DefaultGenesisState)
- Modify: `x/asset/keeper/keeper.go`
- Create: `x/asset/keeper/priced_live.go`
- Create: `x/asset/keeper/priced_live_test.go`
- Modify: `x/asset/keeper/assets.go` (setAssetStatus)
- Modify: `x/asset/keeper/lifecycle_completion.go` (CompleteLifecycle)
- Modify: `x/asset/keeper/oracle_targets.go` (activateOracleTargetBatch)
- Modify: `x/asset/keeper/genesis.go` (InitGenesis/ExportGenesis)
- Modify: `x/asset/keeper/genesis_test.go`

- [ ] **Step 1: Write failing keeper tests**

Create `x/asset/keeper/priced_live_test.go` (suite methods on the existing `KeeperTestSuite`; the file declares `package keeper_test`):

```go
package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

// seedPricedLiveFixture stores one asset per lifecycle case plus a target set
// covering the membership matrix.
func (s *KeeperTestSuite) seedPricedLiveFixture() {
	assets := types.DefaultGenesisState().Assets
	fixture := map[string]types.AssetStatus{
		chain.CNYBaseDenom: types.AssetStatus_ASSET_STATUS_PENDING,
		chain.EURBaseDenom: types.AssetStatus_ASSET_STATUS_ACTIVE,
		chain.GBPBaseDenom: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		chain.JPYBaseDenom: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		chain.KRWBaseDenom: types.AssetStatus_ASSET_STATUS_RETIRED,
		chain.MNTBaseDenom: types.AssetStatus_ASSET_STATUS_ACTIVE,
		chain.SDRBaseDenom: types.AssetStatus_ASSET_STATUS_ACTIVE,
		chain.USDBaseDenom: types.AssetStatus_ASSET_STATUS_ACTIVE,
	}
	for _, asset := range assets {
		asset.Status = fixture[asset.Denom]
		if asset.Denom == chain.MNTBaseDenom {
			// Live status deliberately left untargeted: the defensive case.
			asset.OracleRequired = true
		}
		s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	}
	// Targets: everything except RETIRED KRW and the defensive MNT case. GBP
	// carries a scheduled removal that has not activated: a Removing-window
	// denom stays a member because the materialized set still contains it.
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, types.OracleTargets{
		Denoms: []string{
			chain.CNYBaseDenom,
			chain.EURBaseDenom,
			chain.GBPBaseDenom,
			chain.JPYBaseDenom,
			chain.SDRBaseDenom,
			chain.USDBaseDenom,
		},
		Version: 1,
		Transitions: []types.OracleTargetTransition{{
			Denom:                chain.GBPBaseDenom,
			Direction:            types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE,
			ActivationVoteHeight: 1_000_000,
		}},
	}))
}

func (s *KeeperTestSuite) TestPricedLiveDenoms() {
	s.seedPricedLiveFixture()

	denoms, err := s.keeper.PricedLiveDenoms(s.ctx)
	s.Require().NoError(err)
	// EUR active+targeted, GBP halted+targeted, SDR and USD active+targeted.
	// Excluded: CNY targeted PENDING, JPY targeted SUSPENDED, KRW retired,
	// MNT live but untargeted (defensive case).
	s.Require().Equal([]string{
		chain.EURBaseDenom,
		chain.GBPBaseDenom,
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	}, denoms)
}

func (s *KeeperTestSuite) TestIsPricedLive() {
	s.seedPricedLiveFixture()

	testCases := []struct {
		name   string
		denom  string
		member bool
	}{
		{name: "active targeted", denom: chain.EURBaseDenom, member: true},
		{name: "halted targeted", denom: chain.GBPBaseDenom, member: true},
		{name: "pending targeted", denom: chain.CNYBaseDenom, member: false},
		{name: "suspended targeted", denom: chain.JPYBaseDenom, member: false},
		{name: "retired untargeted", denom: chain.KRWBaseDenom, member: false},
		{name: "live untargeted", denom: chain.MNTBaseDenom, member: false},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			member, err := s.keeper.IsPricedLive(s.ctx, tc.denom)
			s.Require().NoError(err)
			s.Require().Equal(tc.member, member)
		})
	}

	_, err := s.keeper.IsPricedLive(s.ctx, "azzy")
	s.Require().ErrorIs(err, types.ErrAssetNotFound)
}

func (s *KeeperTestSuite) TestPricedLiveVersionAdvancesAcrossLiveBoundary() {
	genesis := types.DefaultGenesisState()
	asset := genesis.Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, genesis.OracleTargets))
	s.Require().NoError(s.keeper.PricedLiveVersion.Set(s.ctx, 1))

	// ACTIVE -> ISSUANCE_HALTED stays inside the live boundary: no bump.
	s.Require().NoError(s.keeper.HaltIssuance(s.ctx, asset.Denom, asset.Version))
	version, err := s.keeper.PricedLiveVersion.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), version)

	// ISSUANCE_HALTED -> SUSPENDED leaves the live set: bump.
	s.Require().NoError(s.keeper.SuspendAsset(s.ctx, asset.Denom, asset.Version+1))
	version, err = s.keeper.PricedLiveVersion.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), version)
}

func (s *KeeperTestSuite) TestPricedLiveVersionAdvancesOnRateCompletion() {
	pending := types.DefaultGenesisState().Assets[0]
	pending.Status = types.AssetStatus_ASSET_STATUS_PENDING
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, pending.Denom, pending))
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, types.OracleTargets{
		Denoms:  []string{pending.Denom},
		Version: 1,
	}))
	s.Require().NoError(s.keeper.PricedLiveVersion.Set(s.ctx, 1))

	// PENDING -> ACTIVE on the first fresh rate enters the live set: bump.
	s.Require().NoError(s.keeper.CompleteLifecycle(
		s.ctx,
		oracletypes.RateSet{pending.Denom: math.LegacyOneDec()},
	))
	version, err := s.keeper.PricedLiveVersion.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), version)
}

func (s *KeeperTestSuite) TestPricedLiveVersionAdvancesOnRemovalPromotion() {
	// Wind-down: FinalizeRetirement schedules removal from ISSUANCE_HALTED,
	// and batch promotion retires the asset — crossing the live boundary.
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, types.OracleTargets{
		Denoms:  []string{asset.Denom},
		Version: 1,
	}))
	s.Require().NoError(s.keeper.PricedLiveVersion.Set(s.ctx, 1))
	s.bankKeeper.EXPECT().
		GetSupply(gomock.Any(), asset.Denom).
		Return(sdk.NewCoin(asset.Denom, math.ZeroInt())).
		AnyTimes()

	s.Require().NoError(s.keeper.FinalizeRetirement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.ZeroInt(),
	))
	version, err := s.keeper.PricedLiveVersion.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), version, "scheduling alone must not bump")

	// Advance past the activation boundary and promote the batch.
	sdkCtx := sdk.UnwrapSDKContext(s.ctx)
	oracleTargets, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(oracleTargets.Transitions, 1)
	promoteCtx := sdkCtx.WithBlockHeight(
		oracleTargets.Transitions[0].ActivationVoteHeight,
	)
	_, err = s.keeper.AdvanceOracleTargets(promoteCtx)
	s.Require().NoError(err)

	version, err = s.keeper.PricedLiveVersion.Get(promoteCtx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), version)

	retired, err := s.keeper.Assets.Get(promoteCtx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(types.AssetStatus_ASSET_STATUS_RETIRED, retired.Status)
}
```

Imports for this file: `math "cosmossdk.io/math"`, `sdk "github.com/cosmos/cosmos-sdk/types"`, `"go.uber.org/mock/gomock"`, plus the ones shown at the top of Step 1.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./x/asset/keeper/ -run 'TestKeeperTestSuite/TestPricedLive|TestKeeperTestSuite/TestIsPricedLive' -v`
Expected: FAIL — `PricedLiveDenoms`, `IsPricedLive`, `PricedLiveVersion` undefined.

- [ ] **Step 3: Add prefixes and collections**

`x/asset/types/keys.go` — append two prefixes:

```go
	ReferenceKey         = collections.NewPrefix(7)
	PricedLiveVersionKey = collections.NewPrefix(8)
```

`x/asset/keeper/keeper.go` — add fields after `EmergencyActions`:

```go
	Reference         collections.Item[types.ReferenceState]
	PricedLiveVersion collections.Item[uint64]
```

and constructor entries after the `EmergencyActions` initializer:

```go
		Reference: collections.NewItem(
			sb,
			types.ReferenceKey,
			"reference",
			codec.CollValue[types.ReferenceState](cdc),
		),
		PricedLiveVersion: collections.NewItem(
			sb,
			types.PricedLiveVersionKey,
			"priced_live_version",
			collections.Uint64Value,
		),
```

- [ ] **Step 4: Implement the view and bump helper**

Create `x/asset/keeper/priced_live.go`:

```go
package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"

	"ark/x/asset/types"
)

// PricedLiveDenoms returns the sorted denoms with live Oracle pricing:
// priceable status intersected with materialized target membership. Batch
// promotion retires assets as their removals activate, so the intersection is
// defence in depth — the view can never name an untargeted denom, even under
// a lifecycle bug, because one unpriced member would poison every consumer
// rebuild that converts through it.
func (k Keeper) PricedLiveDenoms(ctx context.Context) ([]string, error) {
	oracleTargets, err := k.OracleTargets.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Oracle targets: %w", err)
	}

	denoms := make([]string, 0, len(oracleTargets.Denoms))
	for _, denom := range oracleTargets.Denoms {
		asset, err := k.getAsset(ctx, denom)
		if err != nil {
			return nil, err
		}
		if asset.IsPriceable() {
			denoms = append(denoms, denom)
		}
	}

	return denoms, nil
}

// IsPricedLive reports whether one registered denom has live Oracle pricing.
func (k Keeper) IsPricedLive(ctx context.Context, denom string) (bool, error) {
	asset, err := k.getAsset(ctx, denom)
	if err != nil {
		return false, err
	}
	if !asset.IsPriceable() {
		return false, nil
	}
	oracleTargets, err := k.OracleTargets.Get(ctx)
	if err != nil {
		return false, fmt.Errorf("getting Oracle targets: %w", err)
	}
	_, active := slices.BinarySearch(oracleTargets.Denoms, denom)

	return active, nil
}

// bumpPricedLiveOnTransition advances the membership epoch when a status
// write crosses the priceable boundary. Every status write funnels through
// setAssetStatus, CompleteLifecycle, or activateOracleTargetBatch, and each
// calls this with the before/after pair. Over-bumping is harmless;
// under-bumping breaks consumer refresh.
func (k Keeper) bumpPricedLiveOnTransition(
	ctx context.Context,
	before types.Asset,
	after types.Asset,
) error {
	if before.IsPriceable() == after.IsPriceable() {
		return nil
	}
	version, err := k.PricedLiveVersion.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting priced-live version: %w", err)
		}
		version = 0
	}
	if err := k.PricedLiveVersion.Set(ctx, version+1); err != nil {
		return fmt.Errorf("advancing priced-live version: %w", err)
	}

	return nil
}
```

- [ ] **Step 5: Wire the bump at all three status-write sites**

`x/asset/keeper/assets.go`, in `setAssetStatus`, between the `Assets.Set` and the emit:

```go
	if err := k.Assets.Set(ctx, asset.Denom, updated); err != nil {
		return fmt.Errorf("setting status for asset %s: %w", asset.Denom, err)
	}
	if err := k.bumpPricedLiveOnTransition(ctx, asset, updated); err != nil {
		return err
	}

	return emitAssetStatusChanged(ctx, asset, updated)
```

`x/asset/keeper/lifecycle_completion.go`, in `CompleteLifecycle`'s write loop, after the `Assets.Set`:

```go
		if err := k.Assets.Set(ctx, denom, completion.after); err != nil {
			return fmt.Errorf("setting completed asset %s: %w", denom, err)
		}
		if err := k.bumpPricedLiveOnTransition(
			ctx,
			completion.before,
			completion.after,
		); err != nil {
			return err
		}
```

`x/asset/keeper/oracle_targets.go`, in `activateOracleTargetBatch`'s `statusChanges` write loop, after the `Assets.Set`:

```go
		if err := k.bumpPricedLiveOnTransition(
			ctx,
			change.before,
			change.after,
		); err != nil {
			return types.OracleTargets{}, nil, err
		}
```

- [ ] **Step 6: Genesis defaults and keeper wiring**

`x/asset/types/genesis.go` — extend `DefaultGenesisState`'s return:

```go
	return &GenesisState{
		Assets:        assets,
		OracleTargets: NewOracleTargets(denoms),
		Reference: ReferenceState{
			ReferenceDenom: chain.SDRBaseDenom,
			FallbackDenom:  chain.USDBaseDenom,
		},
		PricedLiveVersion: 1,
		EmergencyMandate:  DefaultEmergencyMandate(),
	}
```

`x/asset/keeper/genesis.go` — in `InitGenesis`, after `OracleTargets.Set`:

```go
	if err := k.Reference.Set(ctx, data.Reference); err != nil {
		return fmt.Errorf("setting genesis reference state: %w", err)
	}
	if err := k.PricedLiveVersion.Set(ctx, data.PricedLiveVersion); err != nil {
		return fmt.Errorf("setting genesis priced-live version: %w", err)
	}
```

In `ExportGenesis`, after `oracleTargets` is read:

```go
	reference, err := k.Reference.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting reference state: %w", err)
	}
	pricedLiveVersion, err := k.PricedLiveVersion.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting priced-live version: %w", err)
	}
```

and add `Reference: reference, PricedLiveVersion: pricedLiveVersion,` to the returned `GenesisState`.

Update `x/asset/keeper/genesis_test.go` round-trip expectations to include the two new fields (the existing test compares imported and exported state; with both sides now carrying the fields it should pass unchanged — adjust only if it constructs a literal expected struct).

- [ ] **Step 7: Run tests**

Run: `go test ./x/asset/...`
Expected: PASS, including the new suite methods and the genesis round-trip.

- [ ] **Step 8: Commit**

```bash
git add x/asset/types/keys.go x/asset/types/genesis.go x/asset/keeper
git commit -m "feat(asset): add priced-live view, membership epoch, and reference storage

PricedLiveDenoms and IsPricedLive expose the consumer-facing membership
predicate: Asset.IsPriceable intersected with materialized target
membership. The intersection is defence in depth; batch promotion already
retires assets as removals activate, so the view refuses to depend on that
distributed invariant. PricedLiveVersion is the membership epoch, advanced
by one shared helper at the three status-write sites exactly when a write
crosses the priceable boundary. Reference and the epoch enter the keeper
schema and genesis; the default genesis names the SDR reference with the
USD fallback."
```

---

### Task 4: keeper — reference operations and rebase executors

**Files:**
- Modify: `x/asset/types/expected_keepers.go`
- Modify: `x/asset/types/errors.go`
- Modify: `x/asset/keeper/keeper.go`
- Modify: `x/asset/keeper/emergency_mandate.go` (error rename only)
- Create: `x/asset/keeper/reference.go`
- Create: `x/asset/keeper/reference_test.go`
- Regenerate: `x/asset/testutil/expected_keepers_mocks.go`

- [ ] **Step 1: Add the new executor interfaces (keep the old ones for now)**

Append to `x/asset/types/expected_keepers.go` — do NOT remove `EmergencyMarketKeeper`/`EmergencyTreasuryKeeper` yet; `executeReferenceFallbacks` still compiles against them until Task 8:

```go
// MarketReferenceKeeper re-denominates Market state held in reference units
// (the base-pool delta) when the protocol reference moves. It runs in the
// same transaction as the reference change and its error fails the whole
// action.
type MarketReferenceKeeper interface {
	RebaseBasePool(ctx context.Context, from string, to string) error
}

// TreasuryReferenceKeeper re-expresses Treasury state held in reference units
// (the reference tax cap amount) when the protocol reference moves. It runs
// in the same transaction as the reference change and its error fails the
// whole action.
type TreasuryReferenceKeeper interface {
	RebaseTaxCap(ctx context.Context, from string, to string) error
}
```

- [ ] **Step 2: Regenerate mocks**

Run: `go run go.uber.org/mock/mockgen -source=x/asset/types/expected_keepers.go -package testutil -destination x/asset/testutil/expected_keepers_mocks.go`
Expected: file regenerated with `MockMarketReferenceKeeper` and `MockTreasuryReferenceKeeper` added.

- [ ] **Step 3: Rename the fallback error**

`x/asset/types/errors.go` — code 13 keeps its number, the name broadens because routine reference moves now share the path:

```go
	ErrReferenceFallbackUnavailable  = sdkerrors.Register(ModuleName, 13, "reference fallback is unavailable")
```

Update the three uses of `ErrEmergencyFallbackUnavailable` in `x/asset/keeper/emergency_mandate.go` and any use in its tests to the new name (mechanical rename; behaviour unchanged).

- [ ] **Step 4: Keeper injection fields**

`x/asset/keeper/keeper.go` — add fields next to the existing emergency consumer fields:

```go
	marketReferenceKeeper   types.MarketReferenceKeeper
	treasuryReferenceKeeper types.TreasuryReferenceKeeper
```

and a new injection method after `SetEmergencyConsumers` (which Task 8 deletes):

```go
// SetReferenceConsumers injects the Market and Treasury rebase executors run
// whenever the protocol reference moves — by governance or by fallback
// promotion during suspension. It must be called once during application
// wiring, after both consumer keepers exist.
func (k *Keeper) SetReferenceConsumers(
	marketKeeper types.MarketReferenceKeeper,
	treasuryKeeper types.TreasuryReferenceKeeper,
) {
	k.marketReferenceKeeper = marketKeeper
	k.treasuryReferenceKeeper = treasuryKeeper
}
```

- [ ] **Step 5: Write failing reference tests**

Create `x/asset/keeper/reference_test.go`. Add mock fields to the suite in `keeper_test.go` (`marketReference *testutil.MockMarketReferenceKeeper`, `treasuryReference *testutil.MockTreasuryReferenceKeeper`, constructed in `SetupTest` with the shared `ctrl` and injected via `s.keeper.SetReferenceConsumers(...)`):

```go
package keeper_test

import (
	chain "ark/pkg/chain"
	"ark/x/asset/types"
)

// seedReferenceFixture registers the default genesis assets ACTIVE and
// targeted so eligibility checks are exercised against real registry state.
func (s *KeeperTestSuite) seedReferenceFixture() {
	genesis := types.DefaultGenesisState()
	for _, asset := range genesis.Assets {
		s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	}
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, genesis.OracleTargets))
	s.Require().NoError(s.keeper.PricedLiveVersion.Set(s.ctx, 1))
}

func (s *KeeperTestSuite) TestGetReferenceDefaultsEmpty() {
	reference, err := s.keeper.GetReference(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.ReferenceState{}, reference)
}

func (s *KeeperTestSuite) TestSetReferenceFirstConfiguration() {
	s.seedReferenceFixture()

	reference := types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}
	// First configuration performs no rebase: no executor expectations.
	s.Require().NoError(s.keeper.SetReference(s.ctx, reference))

	stored, err := s.keeper.GetReference(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(reference, stored)
}

func (s *KeeperTestSuite) TestSetReferenceRejectsIneligibleDenoms() {
	s.seedReferenceFixture()
	pending := types.DefaultGenesisState().Assets[0]
	pending.Denom = "azzz"
	pending.Metadata.Base = "azzz"
	pending.Metadata.Display = "zzz"
	pending.Metadata.DenomUnits[0].Denom = "azzz"
	pending.Metadata.DenomUnits[1].Denom = "zzz"
	pending.Status = types.AssetStatus_ASSET_STATUS_PENDING
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, pending.Denom, pending))

	testCases := []struct {
		name      string
		reference types.ReferenceState
		errorIs   error
	}{
		{
			name:      "empty reference denom",
			reference: types.ReferenceState{},
			errorIs:   types.ErrInvalidAssetTransition,
		},
		{
			name:      "unregistered reference",
			reference: types.ReferenceState{ReferenceDenom: "azzy"},
			errorIs:   types.ErrAssetNotFound,
		},
		{
			name:      "pending reference",
			reference: types.ReferenceState{ReferenceDenom: "azzz"},
			errorIs:   types.ErrInvalidAssetTransition,
		},
		{
			name: "pending fallback",
			reference: types.ReferenceState{
				ReferenceDenom: chain.SDRBaseDenom,
				FallbackDenom:  "azzz",
			},
			errorIs: types.ErrInvalidAssetTransition,
		},
		{
			name: "fallback equals reference",
			reference: types.ReferenceState{
				ReferenceDenom: chain.SDRBaseDenom,
				FallbackDenom:  chain.SDRBaseDenom,
			},
			errorIs: types.ErrInvalidAssetTransition,
		},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.keeper.SetReference(s.ctx, tc.reference)
			s.Require().ErrorIs(err, tc.errorIs)
		})
	}
}

func (s *KeeperTestSuite) TestSetReferenceChangeRebasesConsumers() {
	s.seedReferenceFixture()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
	}))

	s.marketReference.EXPECT().
		RebaseBasePool(s.ctx, chain.SDRBaseDenom, chain.USDBaseDenom).
		Return(nil)
	s.treasuryReference.EXPECT().
		RebaseTaxCap(s.ctx, chain.SDRBaseDenom, chain.USDBaseDenom).
		Return(nil)

	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.USDBaseDenom,
		FallbackDenom:  chain.EURBaseDenom,
	}))

	stored, err := s.keeper.GetReference(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, stored.ReferenceDenom)
	s.Require().Equal(chain.EURBaseDenom, stored.FallbackDenom)
}

func (s *KeeperTestSuite) TestSetReferenceFallbackOnlyUpdateSkipsRebase() {
	s.seedReferenceFixture()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
	}))

	// No executor expectations: a fallback-only update must not rebase.
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}))
}
```

Executor-failure and nil-executor cases need a keeper without injected mocks; add:

```go
func (s *KeeperTestSuite) TestSetReferenceChangeWithoutExecutorsFails() {
	s.seedReferenceFixture()
	s.keeper.SetReferenceConsumers(nil, nil)
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
	}))

	err := s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.USDBaseDenom,
	})
	s.Require().ErrorIs(err, types.ErrReferenceFallbackUnavailable)
}

func (s *KeeperTestSuite) TestSetReferenceExecutorErrorFailsAtomically() {
	s.seedReferenceFixture()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
	}))

	s.marketReference.EXPECT().
		RebaseBasePool(s.ctx, chain.SDRBaseDenom, chain.USDBaseDenom).
		Return(fmt.Errorf("pool rebase failed"))

	err := s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.USDBaseDenom,
	})
	s.Require().ErrorIs(err, types.ErrReferenceFallbackUnavailable)

	stored, err := s.keeper.GetReference(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.SDRBaseDenom, stored.ReferenceDenom,
		"a failed rebase must leave the reference unchanged")
}
```

`promoteReferenceFallback` is unexported and has no caller until Tasks 6 and 8 wire it; its behaviour (promotion, empty-fallback failure, execution-time re-validation) is covered end to end by Task 8's emergency tests rather than duplicated here. The executor-error test above needs `"fmt"` in the imports.

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./x/asset/keeper/ -run 'TestKeeperTestSuite/TestSetReference|TestKeeperTestSuite/TestGetReference' -v`
Expected: FAIL — `GetReference`/`SetReference` undefined.

- [ ] **Step 7: Implement reference.go**

Create `x/asset/keeper/reference.go`:

```go
package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/asset/types"
)

// GetReference returns the shared protocol reference state, empty when never
// configured.
func (k Keeper) GetReference(ctx context.Context) (types.ReferenceState, error) {
	reference, err := k.Reference.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ReferenceState{}, nil
		}
		return types.ReferenceState{}, fmt.Errorf("getting reference state: %w", err)
	}

	return reference, nil
}

// SetReference replaces the reference state. Changing a non-empty reference
// denom rebases consumer reference-unit state atomically; first-time
// configuration and fallback-only updates do not.
func (k Keeper) SetReference(ctx context.Context, reference types.ReferenceState) error {
	if err := reference.Validate(); err != nil {
		return sdkerrors.Wrap(types.ErrInvalidAssetTransition, err.Error())
	}
	if reference.ReferenceDenom == "" {
		return sdkerrors.Wrap(
			types.ErrInvalidAssetTransition,
			"reference denom must be set",
		)
	}
	if err := k.requireReferenceEligible(ctx, reference.ReferenceDenom); err != nil {
		return err
	}
	if reference.FallbackDenom != "" {
		if err := k.requireReferenceEligible(ctx, reference.FallbackDenom); err != nil {
			return err
		}
	}

	current, err := k.GetReference(ctx)
	if err != nil {
		return err
	}
	if current.ReferenceDenom != "" &&
		current.ReferenceDenom != reference.ReferenceDenom {
		if err := k.rebaseReference(
			ctx,
			current.ReferenceDenom,
			reference.ReferenceDenom,
		); err != nil {
			return err
		}
	}

	return k.writeReference(ctx, reference)
}

// promoteReferenceFallback moves the reference to its fallback ahead of
// suspending denom. It returns the replacement denom, empty when denom is
// not the reference.
func (k Keeper) promoteReferenceFallback(
	ctx context.Context,
	denom string,
) (string, error) {
	current, err := k.GetReference(ctx)
	if err != nil {
		return "", err
	}
	if current.ReferenceDenom != denom {
		return "", nil
	}
	fallback := current.FallbackDenom
	if fallback == "" {
		return "", sdkerrors.Wrapf(
			types.ErrReferenceFallbackUnavailable,
			"reference asset %s has no configured fallback",
			denom,
		)
	}
	// The fallback was eligible when configured; the world may have moved, so
	// eligibility is re-validated at execution.
	if err := k.requireReferenceEligible(ctx, fallback); err != nil {
		return "", err
	}
	if err := k.rebaseReference(ctx, denom, fallback); err != nil {
		return "", err
	}
	if err := k.writeReference(
		ctx,
		types.ReferenceState{ReferenceDenom: fallback},
	); err != nil {
		return "", err
	}

	return fallback, nil
}

// requireNotReferenceNamed refuses retirement paths for an asset named by the
// reference state; ReferenceState must never name a retired asset.
func (k Keeper) requireNotReferenceNamed(ctx context.Context, denom string) error {
	reference, err := k.GetReference(ctx)
	if err != nil {
		return err
	}
	if reference.ReferenceDenom == denom || reference.FallbackDenom == denom {
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s is named by the protocol reference state",
			denom,
		)
	}

	return nil
}

// requireReferenceEligible checks the live-reference rules: Oracle-priced,
// live status, active target phase.
func (k Keeper) requireReferenceEligible(ctx context.Context, denom string) error {
	asset, err := k.getAsset(ctx, denom)
	if err != nil {
		return err
	}
	oracleTargets, err := k.OracleTargets.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Oracle targets: %w", err)
	}
	if !asset.IsPriceable() ||
		oracleTargets.Phase(denom) != types.OracleTargetPhaseActive {
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot serve as the protocol reference",
			asset.Status,
			denom,
		)
	}

	return nil
}

// rebaseReference re-denominates every consumer's reference-unit state. A
// nil executor or executor error fails the whole action atomically.
func (k Keeper) rebaseReference(ctx context.Context, from string, to string) error {
	if k.marketReferenceKeeper == nil || k.treasuryReferenceKeeper == nil {
		return sdkerrors.Wrapf(
			types.ErrReferenceFallbackUnavailable,
			"moving reference %s with no rebase executors wired",
			from,
		)
	}
	if err := k.marketReferenceKeeper.RebaseBasePool(ctx, from, to); err != nil {
		return sdkerrors.Wrapf(
			types.ErrReferenceFallbackUnavailable,
			"rebasing Market base pool from %s to %s: %v",
			from,
			to,
			err,
		)
	}
	if err := k.treasuryReferenceKeeper.RebaseTaxCap(ctx, from, to); err != nil {
		return sdkerrors.Wrapf(
			types.ErrReferenceFallbackUnavailable,
			"rebasing Treasury tax cap from %s to %s: %v",
			from,
			to,
			err,
		)
	}

	return nil
}

func (k Keeper) writeReference(ctx context.Context, reference types.ReferenceState) error {
	if err := k.Reference.Set(ctx, reference); err != nil {
		return fmt.Errorf("setting reference state: %w", err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventReferenceUpdated{
			ReferenceDenom: reference.ReferenceDenom,
			FallbackDenom:  reference.FallbackDenom,
		},
	); err != nil {
		return fmt.Errorf("emitting reference update: %w", err)
	}

	return nil
}
```

`promoteReferenceFallback` and `requireNotReferenceNamed` are used by Tasks 6 and 8; Go tolerates the unused unexported methods meanwhile (methods are not flagged like unused locals).

- [ ] **Step 8: Run tests**

Run: `go test ./x/asset/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add x/asset/types/expected_keepers.go x/asset/types/errors.go x/asset/keeper x/asset/testutil
git commit -m "feat(asset): own the protocol reference with atomic rebase executors

GetReference/SetReference manage the asset-owned ReferenceState under the
live-reference eligibility rules the lock path enforced: Oracle-priced,
live status, active target phase, re-validated for the fallback at
execution. Changing a non-empty reference rebases both consumers through
the new MarketReferenceKeeper and TreasuryReferenceKeeper interfaces —
asset now owns the replacement choice and passes it, instead of each
consumer choosing its own — and a nil executor or executor error fails the
whole action. ErrEmergencyFallbackUnavailable becomes
ErrReferenceFallbackUnavailable (same code) because routine governance
moves now share the path."
```

---

### Task 5: MsgSetReference handler, Reference query, codec, autocli

**Files:**
- Modify: `x/asset/keeper/msg_server.go`
- Modify: `x/asset/keeper/msg_server_test.go`
- Modify: `x/asset/keeper/grpc_query.go`
- Modify: `x/asset/keeper/grpc_query_test.go`
- Modify: `x/asset/types/codec.go`
- Modify: `x/asset/module/autocli.go`
- Modify: `x/asset/module/module_test.go`

- [ ] **Step 1: Write failing tests**

Append to `x/asset/keeper/msg_server_test.go`, following its existing pattern for authority-gated messages:

```go
func (s *KeeperTestSuite) TestMsgSetReference() {
	s.seedReferenceFixture()
	server := keeper.NewMsgServerImpl(s.keeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	_, err := server.SetReference(s.ctx, &types.MsgSetReference{
		Authority:      "invalid",
		ReferenceDenom: chain.SDRBaseDenom,
	})
	s.Require().Error(err)

	_, err = server.SetReference(s.ctx, &types.MsgSetReference{
		Authority:      authority,
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	})
	s.Require().NoError(err)

	stored, err := s.keeper.GetReference(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.SDRBaseDenom, stored.ReferenceDenom)
	s.Require().Equal(chain.USDBaseDenom, stored.FallbackDenom)
}
```

Append to `x/asset/keeper/grpc_query_test.go`:

```go
func (s *KeeperTestSuite) TestQueryReference() {
	server := keeper.NewQueryServerImpl(s.keeper)

	response, err := server.Reference(s.ctx, &types.QueryReferenceRequest{})
	s.Require().NoError(err)
	s.Require().Equal(types.ReferenceState{}, response.Reference)

	s.seedReferenceFixture()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
	}))

	response, err = server.Reference(s.ctx, &types.QueryReferenceRequest{})
	s.Require().NoError(err)
	s.Require().Equal(chain.SDRBaseDenom, response.Reference.ReferenceDenom)

	_, err = server.Reference(s.ctx, nil)
	s.Require().Error(err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./x/asset/keeper/ -run 'TestKeeperTestSuite/TestMsgSetReference|TestKeeperTestSuite/TestQueryReference' -v`
Expected: FAIL — handlers unimplemented (`UnimplementedMsgServer` returns codes.Unimplemented, the stored state assertions fail).

- [ ] **Step 3: Implement handler and query**

`x/asset/keeper/msg_server.go` — insert after the `ReactivateAsset` handler:

```go
func (m msgServer) SetReference(
	ctx context.Context,
	msg *types.MsgSetReference,
) (*types.MsgSetReferenceResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set reference message")
	}
	if err := sdk.ValidateAuthority(
		sdk.UnwrapSDKContext(ctx),
		m.k.authority,
		msg.Authority,
	); err != nil {
		return nil, err
	}
	if err := m.k.SetReference(ctx, types.ReferenceState{
		ReferenceDenom: msg.ReferenceDenom,
		FallbackDenom:  msg.FallbackDenom,
	}); err != nil {
		return nil, err
	}

	return &types.MsgSetReferenceResponse{}, nil
}
```

`x/asset/keeper/grpc_query.go` — insert after the `Assets` method:

```go
// Reference returns the shared protocol reference state.
func (q queryServer) Reference(
	ctx context.Context,
	req *types.QueryReferenceRequest,
) (*types.QueryReferenceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	reference, err := q.k.GetReference(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting reference state: %v", err)
	}

	return &types.QueryReferenceResponse{Reference: reference}, nil
}
```

`x/asset/types/codec.go` — add to both registration functions:

```go
	legacy.RegisterAminoMsg(cdc, &MsgSetReference{}, "ark/x/asset/MsgSetReference")
```

and `&MsgSetReference{},` in the `RegisterImplementations` list.

- [ ] **Step 4: autocli entries**

`x/asset/module/autocli.go` — query entry after the `Assets` block:

```go
				{
					RpcMethod: "Reference",
					Use:       "reference",
					Short:     "Query the shared protocol reference state",
					Example: fmt.Sprintf(
						"%s query asset reference",
						version.AppName,
					),
				},
```

Tx entry after the `ReactivateAsset` proposal command:

```go
				assetProposalCommand(
					"SetReference",
					"set-reference-proposal [reference-denom] [fallback-denom]",
					"Submit a proposal to set the protocol reference and fallback",
					"reference_denom",
					"fallback_denom",
				),
```

`x/asset/module/module_test.go` — in `TestAutoCLIOptionsCoverAssetServices`, add `"Reference"` to the sorted query list (between `"OracleTargets"` and `"ResolutionHistory"`) and `"SetReference"` to the sorted tx list (between `"SetEmergencyMandate"` and `"SuspendAsset"`).

- [ ] **Step 5: Run tests**

Run: `go test ./x/asset/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add x/asset/keeper x/asset/types/codec.go x/asset/module
git commit -m "feat(asset): expose SetReference message and Reference query

MsgSetReference is authority-gated and delegates to the keeper, which
validates eligibility and rebases consumers on a reference change. The
Reference query returns the stored state and is module-query-safe. Both are
registered in the codec and autocli, with the coverage test lists extended."
```

---

### Task 6: lifecycle guards — suspension and retirement

**Files:**
- Modify: `x/asset/keeper/lifecycle.go`
- Modify: `x/asset/keeper/lifecycle_test.go`

- [ ] **Step 1: Write failing tests**

Append to `x/asset/keeper/lifecycle_test.go` (reuse `seedReferenceFixture` from Task 4):

```go
func (s *KeeperTestSuite) TestSuspendAssetRefusesProtocolReference() {
	s.seedReferenceFixture()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}))

	err := s.keeper.SuspendAsset(s.ctx, chain.SDRBaseDenom, 1)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
	s.Require().ErrorContains(err, "move the reference first")

	// The fallback asset is suspendable; eligibility re-validation happens at
	// promotion time, not here.
	s.Require().NoError(s.keeper.SuspendAsset(s.ctx, chain.USDBaseDenom, 1))
}

func (s *KeeperTestSuite) TestFinalizeRetirementRefusesReferenceNamedAssets() {
	s.seedReferenceFixture()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}))

	for _, denom := range []string{chain.SDRBaseDenom, chain.USDBaseDenom} {
		halted, err := s.keeper.Assets.Get(s.ctx, denom)
		s.Require().NoError(err)
		halted.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
		s.Require().NoError(s.keeper.Assets.Set(s.ctx, denom, halted))

		err = s.keeper.FinalizeRetirement(s.ctx, denom, 1, math.ZeroInt())
		s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
		s.Require().ErrorContains(err, "named by the protocol reference state")
	}
}
```

`TestFinalizeRetirementRefusesReferenceNamedAssets` needs no bank mock expectations because the reference check fires before the supply read — place the `requireNotReferenceNamed` call accordingly (Step 3).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./x/asset/keeper/ -run 'TestKeeperTestSuite/TestSuspendAssetRefuses|TestKeeperTestSuite/TestFinalizeRetirementRefuses' -v`
Expected: FAIL — suspension of the reference currently succeeds (no locks are seeded, so the lock guard passes).

- [ ] **Step 3: Rewrite the guards**

In `suspendAsset` (`x/asset/keeper/lifecycle.go:275`), replace the live-pricing-lock block:

```go
	// Live-reference dependencies must have moved first, priced or not:
	// suspending an asset another module still points at would leave that
	// reference dangling.
	if err := k.requireNoLivePricingLocks(ctx, denom); err != nil {
		return err
	}
```

with:

```go
	// The protocol reference must have moved first: suspending the asset both
	// consumers price through would leave their reference-unit state dangling.
	reference, err := k.GetReference(ctx)
	if err != nil {
		return err
	}
	if reference.ReferenceDenom == denom {
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s is the protocol reference; move the reference first",
			denom,
		)
	}
```

In `FinalizeRetirement` (`x/asset/keeper/lifecycle.go:568`), replace:

```go
	if err := k.requireNoAssetLocks(ctx, denom); err != nil {
		return err
	}
```

with:

```go
	if err := k.requireNotReferenceNamed(ctx, denom); err != nil {
		return err
	}
```

Delete the now-unreferenced `requireNoAssetLocks` and `requireNoLivePricingLocks` functions (`x/asset/keeper/lifecycle.go:759-811`) and drop the `"cosmossdk.io/collections"` import if it becomes unused in the file.

Update `MsgFinalizeRetirement`-related doc comments in `lifecycle.go` only if they mention locks (the `FinalizeRetirement` function comment says "retires a cleared asset" — leave; proto comments change in Task 10).

- [ ] **Step 4: Fix displaced lock-based tests**

`x/asset/keeper/lifecycle_test.go` currently contains cases that seed locks to prove suspension/retirement refusal (search `AssetLock` in the file). Rewrite each to seed the reference instead (as in Step 1) or delete where the new tests already cover the behaviour. Cases proving "dormant policy locks do not block suspension" lose their subject and are deleted.

- [ ] **Step 5: Run tests**

Run: `go test ./x/asset/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add x/asset/keeper/lifecycle.go x/asset/keeper/lifecycle_test.go
git commit -m "refactor(asset): guard suspension and retirement with reference state

suspendAsset refuses the current protocol reference — the emergency path
satisfies this by promoting the fallback first, and routine governance
bundles MsgSetReference before MsgSuspendAsset. Retirement refuses any
asset named by ReferenceState so it can never name a retired asset. The
lock-walking guards are deleted; their remaining semantics live entirely
in asset-owned state."
```

---

### Task 7: settlement guard

**Files:**
- Modify: `x/asset/keeper/settlement.go`
- Modify: `x/asset/keeper/settlement_test.go`

- [ ] **Step 1: Write failing test**

Append to `x/asset/keeper/settlement_test.go`, matching its fixture style for a suspended asset (find the existing `OpenSettlement` happy-path setup and copy its supply mock expectation):

```go
func (s *KeeperTestSuite) TestOpenSettlementRequiresOraclePricing() {
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.OracleRequired = false
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(gomock.Any(), asset.Denom).
		Return(sdk.NewCoin(asset.Denom, math.OneInt()))

	err := s.keeper.OpenSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.LegacyOneDec(),
		s.blockHeight()+types.SettlementActivationDelayBlocks,
		0,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
	s.Require().ErrorContains(err, "not Oracle-priced")
}
```

(If the suite has no `blockHeight()` helper, read the height with `sdk.UnwrapSDKContext(s.ctx).BlockHeight()` inline, as the existing settlement tests do.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./x/asset/keeper/ -run 'TestKeeperTestSuite/TestOpenSettlementRequiresOraclePricing' -v`
Expected: FAIL — the current code errors with "has no Market settlement policy" (different message) because no lock is seeded; the assertion on "not Oracle-priced" fails.

- [ ] **Step 3: Replace the check**

In `OpenSettlement` (`x/asset/keeper/settlement.go`), replace:

```go
	if err := k.requireMarketSettlementPolicy(ctx, denom); err != nil {
		return err
	}
```

with:

```go
	// Settlement redemption executes through Market conversion, which under
	// identical-by-construction membership every Oracle-priced asset carries.
	if !asset.OracleRequired {
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s is not Oracle-priced and has no Market settlement path",
			denom,
		)
	}
```

Delete the `requireMarketSettlementPolicy` function (`x/asset/keeper/settlement.go:424-449`). Remove settlement tests that seed `MARKET_ASSET_POLICY` locks to satisfy the old check — the lock seeding lines just disappear from otherwise-valid tests (search `ASSET_LOCK_KIND_MARKET_ASSET_POLICY` in `settlement_test.go`).

- [ ] **Step 4: Run tests**

Run: `go test ./x/asset/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add x/asset/keeper/settlement.go x/asset/keeper/settlement_test.go
git commit -m "refactor(asset): settlement asserts Oracle pricing, not a Market lock

Under identical-by-construction membership every Oracle-priced asset is
Market-convertible, so requireMarketSettlementPolicy reduced to an
oracle_required assertion. OpenSettlement now states that directly and the
lock-based check is deleted."
```

---

### Task 8: emergency mandate rework

**Files:**
- Modify: `x/asset/keeper/emergency_mandate.go`
- Modify: `x/asset/keeper/emergency_mandate_test.go`
- Modify: `x/asset/types/expected_keepers.go` (delete old interfaces)
- Modify: `x/asset/keeper/keeper.go` (delete old fields + `SetEmergencyConsumers`)
- Regenerate: `x/asset/testutil/expected_keepers_mocks.go`

- [ ] **Step 1: Write failing tests**

Rewrite the fallback-related tests in `x/asset/keeper/emergency_mandate_test.go`. The file's existing suspend tests seed a mandate and call `EmergencySuspendAsset`; adapt them so the reference drives the fallback path (follow the file's existing mandate-seeding helper):

```go
func (s *KeeperTestSuite) TestEmergencySuspendPromotesReferenceFallback() {
	s.seedReferenceFixture()
	s.seedActiveMandate() // existing helper in this file; keep its name
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}))

	s.marketReference.EXPECT().
		RebaseBasePool(s.ctx, chain.SDRBaseDenom, chain.USDBaseDenom).
		Return(nil)
	s.treasuryReference.EXPECT().
		RebaseTaxCap(s.ctx, chain.SDRBaseDenom, chain.USDBaseDenom).
		Return(nil)

	s.Require().NoError(s.keeper.EmergencySuspendAsset(
		s.ctx,
		s.committee,
		chain.SDRBaseDenom,
		s.mandateTerm,
	))

	reference, err := s.keeper.GetReference(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, reference.ReferenceDenom)
	s.Require().Empty(reference.FallbackDenom)

	suspended, err := s.keeper.Assets.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(types.AssetStatus_ASSET_STATUS_SUSPENDED, suspended.Status)
}

func (s *KeeperTestSuite) TestEmergencySuspendReferenceWithoutFallbackFails() {
	s.seedReferenceFixture()
	s.seedActiveMandate()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
	}))

	err := s.keeper.EmergencySuspendAsset(
		s.ctx,
		s.committee,
		chain.SDRBaseDenom,
		s.mandateTerm,
	)
	s.Require().ErrorIs(err, types.ErrReferenceFallbackUnavailable)

	// Atomic failure: still ACTIVE, reference unchanged.
	asset, err := s.keeper.Assets.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(types.AssetStatus_ASSET_STATUS_ACTIVE, asset.Status)
}

func (s *KeeperTestSuite) TestEmergencySuspendIneligibleFallbackFails() {
	s.seedReferenceFixture()
	s.seedActiveMandate()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}))
	// Suspend the fallback asset first (allowed); the reference then has a
	// configured but ineligible fallback, re-validated at execution.
	s.Require().NoError(s.keeper.SuspendAsset(s.ctx, chain.USDBaseDenom, 1))

	err := s.keeper.EmergencySuspendAsset(
		s.ctx,
		s.committee,
		chain.SDRBaseDenom,
		s.mandateTerm,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)

	asset, err := s.keeper.Assets.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(types.AssetStatus_ASSET_STATUS_ACTIVE, asset.Status)
}

func (s *KeeperTestSuite) TestEmergencySuspendNonReferenceSkipsRebase() {
	s.seedReferenceFixture()
	s.seedActiveMandate()
	s.Require().NoError(s.keeper.SetReference(s.ctx, types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}))

	// No executor expectations: suspending a non-reference asset never
	// touches consumer reference-unit state.
	s.Require().NoError(s.keeper.EmergencySuspendAsset(
		s.ctx,
		s.committee,
		chain.EURBaseDenom,
		s.mandateTerm,
	))
}
```

Adjust helper names (`seedActiveMandate`, `s.committee`, `s.mandateTerm`) to whatever the existing file actually uses — read the current suspend tests first and reuse their setup verbatim.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./x/asset/keeper/ -run 'TestKeeperTestSuite/TestEmergencySuspend' -v`
Expected: FAIL — the current path executes lock-driven fallbacks (none seeded), so no promotion happens and the reference assertions fail.

- [ ] **Step 3: Rework EmergencySuspendAsset**

In `x/asset/keeper/emergency_mandate.go`:

Replace the body section of `EmergencySuspendAsset`:

```go
	fallbacks, err := k.executeReferenceFallbacks(ctx, denom)
	if err != nil {
		return err
	}
	if err := k.suspendAsset(ctx, asset); err != nil {
		return err
	}

	return k.recordEmergencyAction(
		ctx,
		denom,
		types.EmergencyAction_EMERGENCY_ACTION_SUSPEND,
		expectedTerm,
		asset.Version+1,
		fallbacks,
	)
```

with:

```go
	replacement, err := k.promoteReferenceFallback(ctx, denom)
	if err != nil {
		return err
	}
	if err := k.suspendAsset(ctx, asset); err != nil {
		return err
	}

	return k.recordEmergencyAction(
		ctx,
		denom,
		types.EmergencyAction_EMERGENCY_ACTION_SUSPEND,
		expectedTerm,
		asset.Version+1,
		replacement,
	)
```

Update its doc comment:

```go
// EmergencySuspendAsset promotes the reference fallback when the target is
// the protocol reference, then applies SuspendAsset semantics, in one
// transaction. A missing or ineligible fallback fails the whole action
// atomically, and the governance path takes over.
```

Delete `executeReferenceFallbacks` entirely (lines 190-259).

Change `recordEmergencyAction`'s signature and event:

```go
func (k Keeper) recordEmergencyAction(
	ctx context.Context,
	denom string,
	action types.EmergencyAction,
	term uint64,
	version uint64,
	replacementDenom string,
) error {
```

and in the emitted event replace `FallbackDenoms: fallbackDenoms,` with `ReplacementDenom: replacementDenom,`. Update the `EmergencyHaltIssuance` call site to pass `""` instead of `nil`. Remove the now-unused `"slices"` import.

- [ ] **Step 4: Delete the superseded interfaces and injection**

`x/asset/types/expected_keepers.go`: delete `EmergencyMarketKeeper` and `EmergencyTreasuryKeeper`.
`x/asset/keeper/keeper.go`: delete the `emergencyMarketKeeper`/`emergencyTreasuryKeeper` fields, their constructor absence is already fine, and the `SetEmergencyConsumers` method; update the struct comment to describe the rebase executors as the only asset-to-consumer edges.

Regenerate mocks:

Run: `go run go.uber.org/mock/mockgen -source=x/asset/types/expected_keepers.go -package testutil -destination x/asset/testutil/expected_keepers_mocks.go`

Fix any test still constructing the old mocks (search `MockEmergencyMarketKeeper|MockEmergencyTreasuryKeeper|SetEmergencyConsumers` under `x/asset/`).

- [ ] **Step 5: Run tests**

Run: `go test ./x/asset/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add x/asset/types/expected_keepers.go x/asset/keeper x/asset/testutil
git commit -m "refactor(asset): emergency suspension promotes the owned reference fallback

EmergencySuspendAsset replaces lock-driven per-consumer fallback execution
with promoteReferenceFallback: when the target is the protocol reference,
the asset-owned fallback is re-validated, both consumers rebase through the
reference executors, and the reference moves — atomically with the
suspension. The action record and event now carry the single
replacement_denom. The superseded EmergencyMarketKeeper and
EmergencyTreasuryKeeper interfaces and their injection method are deleted."
```

---

### Task 9: de-lock sweep

Everything Go-side that still mentions locks goes. Generated proto types survive until Task 10.

**Files:**
- Delete: `x/asset/keeper/asset_locks.go`
- Delete: `x/asset/keeper/asset_locks_test.go`
- Modify: `x/asset/keeper/keeper.go`
- Modify: `x/asset/keeper/keeper_test.go`
- Modify: `x/asset/keeper/grpc_query.go`
- Modify: `x/asset/keeper/grpc_query_test.go`
- Modify: `x/asset/types/keys.go`
- Modify: `x/asset/types/asset.go`
- Modify: `x/asset/types/asset_test.go`
- Modify: `x/asset/types/errors.go`
- Modify: `x/asset/module/autocli.go`
- Modify: `x/asset/module/module_test.go`

- [ ] **Step 1: Delete lock code**

- Remove `x/asset/keeper/asset_locks.go` and `x/asset/keeper/asset_locks_test.go` (`git rm`).
- `x/asset/keeper/keeper.go`: delete the `AssetLocks` field and its constructor entry.
- `x/asset/types/keys.go`: delete `AssetLocksKey` and `AssetLockKindKey`; leave a burn comment:

```go
	AssetsKey = collections.NewPrefix(0)
	// Prefix 1 belonged to the deleted AssetLocks KeySet and stays burned.
	OracleTargetsKey     = collections.NewPrefix(2)
```

- `x/asset/types/asset.go`: delete `AssetLock.Validate`, `AssetLock.Key`, and `AssetLockKind.RequiresOraclePricing` (lines 128-157); drop the `"cosmossdk.io/collections"` import if now unused.
- `x/asset/types/errors.go`: delete `ErrAssetLocked`; leave a comment on the block: `// Code 5 belonged to ErrAssetLocked and stays burned.`
- `x/asset/keeper/grpc_query.go`: delete the `AssetLocks` method.
- `x/asset/module/autocli.go`: delete the `AssetLocks` query entry.
- `x/asset/module/module_test.go`: remove `"AssetLocks"` from the query list.
- `x/asset/keeper/keeper_test.go`: in `TestCollectionsPersistState`, replace the lock KeySet round-trip block with a `Reference` item round-trip:

```go
	reference := types.ReferenceState{
		ReferenceDenom: chain.SDRBaseDenom,
		FallbackDenom:  chain.USDBaseDenom,
	}
	s.Require().NoError(s.keeper.Reference.Set(s.ctx, reference))
	storedReference, err := s.keeper.Reference.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(reference, storedReference)
```

- `x/asset/keeper/grpc_query_test.go` and `x/asset/types/asset_test.go`: delete the `AssetLocks` query tests and `AssetLock` validation tests.

- [ ] **Step 2: Sweep for stragglers**

Run: `grep -rn "AssetLock\|asset_locks\|ErrAssetLocked" x/ abci/ oracle/ app/ --include="*.go" | grep -v "\.pb\.go"`
Expected: no output. Fix anything that appears.

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./x/asset/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add -A x/asset
git commit -m "refactor(asset): delete the AssetLocks inverse dependency index

With membership, liability inclusion, and the protocol reference all
derived from or owned by asset state, no foreign per-denom state remains
for locks to protect. The KeySet, its keeper file, the query RPC and
autocli entry, the AssetLock type helpers, and ErrAssetLocked are deleted;
collection prefix 1 and error code 5 stay burned. Generated proto types
remain until the destructive proto change lands."
```

---

### Task 10: destructive proto change

**Files:**
- Modify: `proto/ark/asset/v1/asset.proto`
- Modify: `proto/ark/asset/v1/event.proto`
- Modify: `proto/ark/asset/v1/query.proto`
- Modify: `proto/ark/asset/v1/genesis.proto`
- Modify: `proto/ark/asset/v1/tx.proto`

- [ ] **Step 1: asset.proto**

Delete the `AssetLockKind` enum (lines 41-56) and the `AssetLock` message (lines 75-79). Proto3 has no file-level reservation for deleted top-level names — the burn is documented in the plan doc (Task 11).

Update the `EmergencyMandate` comment's last clause:

```protobuf
// EmergencyMandate stores the governance-appointed committee that may remove
// asset capabilities in minutes and restore nothing. It carries exactly the
// shared appointment envelope: consumed per-term actions are separate state,
// and the reference fallback lives in the asset module's ReferenceState.
```

Update the `EMERGENCY_ACTION_SUSPEND` comment:

```protobuf
  // EMERGENCY_ACTION_SUSPEND promotes the reference fallback when needed and
  // applies SuspendAsset semantics.
  EMERGENCY_ACTION_SUSPEND = 2;
```

- [ ] **Step 2: event.proto**

Delete `EventAssetLockAdded` and `EventAssetLockRemoved`. In `EventEmergencyActionExecuted`, delete the `fallback_denoms` field and reserve it:

```protobuf
  reserved 5;
  reserved "fallback_denoms";
```

- [ ] **Step 3: query.proto**

Delete the `AssetLocks` rpc, `QueryAssetLocksRequest`, and `QueryAssetLocksResponse`.

- [ ] **Step 4: genesis.proto**

Rewrite the `GenesisState` header comment:

```protobuf
// GenesisState defines the asset module's genesis state, including the
// shared protocol reference and the priced-live membership epoch.
```

- [ ] **Step 5: tx.proto**

Update the two lock-flavoured comments: the `FinalizeRetirement` rpc comment becomes:

```protobuf
  // FinalizeRetirement retires an asset not named by the reference state
  // within its approved residual bound; from PENDING it cancels the
  // registration.
```

and `MsgFinalizeRetirement`'s message comment first line becomes:

```protobuf
// MsgFinalizeRetirement retires a settlement-free asset not named by the
// reference state, scheduling Oracle target removal first when the asset
// remains Oracle-targeted. From PENDING it cancels the registration before
// first activation.
```

Update `EmergencySuspendAsset` rpc and `MsgEmergencySuspendAsset` comments:

```protobuf
  // EmergencySuspendAsset promotes the asset-owned reference fallback when
  // needed and applies SuspendAsset semantics in one transaction.
```

```protobuf
// MsgEmergencySuspendAsset promotes the asset-owned reference fallback when
// the target is the protocol reference, then applies SuspendAsset semantics,
// in one transaction. A missing or ineligible fallback fails the whole
// action atomically.
```

- [ ] **Step 6: Generate and verify**

Run: `make proto-format && make proto-lint && make proto-gen`
Then: `go build ./... && go test ./x/asset/...`
Expected: PASS — no Go code references the deleted types after Task 9.

- [ ] **Step 7: Commit**

```bash
git add proto/ark/asset/v1 x/asset/types api/ark/asset/v1
git commit -m "proto(asset): delete lock messages and the superseded fallback event field

AssetLockKind, AssetLock, both lock events, and the AssetLocks query are
removed now that no Go code references them. EventEmergencyActionExecuted
reserves field 5 (fallback_denoms), superseded by replacement_denom. Lock
and fallback comments across the asset protos are rewritten to the
reference-state model."
```

---

### Task 11: rewrite ASSET_MODULE_PLAN.md

**Files:**
- Modify: `docs/ASSET_MODULE_PLAN.md`

- [ ] **Step 1: Update each lock-flavoured section**

Work through these anchors (grep the quoted text):

1. **Line ~35** `- x/asset owns identity, Bank metadata, ...`: replace "dependency locks" with "the protocol reference".
2. **Section "### x/market" / "### x/treasury" (~150-162)**: delete "the base-pool emergency fallback" and "the reference-tax-cap emergency fallback" from the ownership lists; add to each a sentence: "State held in reference units rebases through the asset module's reference executors when the protocol reference moves." Replace the "Each consumer owns the fallback..." paragraph with:

```markdown
The base-pool reference and the reference-tax-cap denomination are the same
denomination by design, permanently. `x/asset` therefore owns a single
`ReferenceState` — reference plus fallback — set by governance, promoted
automatically on emergency suspension, and rebased into each consumer's
reference-unit state through executor interfaces. This reverses the earlier
two-independent-fallbacks decision; the reversal is recorded in
`docs/superpowers/specs/2026-07-29-asset-consolidation-design.md`.
```

3. **State model (~166-176)**: remove the `AssetLocks` line; add:

```text
    Reference         collections.Item[types.ReferenceState]
    PricedLiveVersion collections.Item[uint64]
```

4. **Per-status sections**: in ACTIVE, replace "Live-reference and policy locks may be attached." with "The asset may serve as the protocol reference or its fallback."; in PENDING/ISSUANCE_HALTED/RETIRED, delete lock-acquisition sentences.
5. **Section "## Asset locks" (~620-652)**: replace wholesale with:

```markdown
## Protocol reference

`ReferenceState` names the single reference denomination shared by Market's
base pool and Treasury's reference tax cap, plus its fallback. Rules:

- Both denominations must be Oracle-priced with live status (`ACTIVE` or
  non-finalized `ISSUANCE_HALTED`) and active target phase when set; the
  fallback is re-validated at execution.
- Suspending the reference asset requires the reference to move first. The
  emergency path promotes the fallback and rebases both consumers
  atomically; the governance path bundles `MsgSetReference` ahead of
  `MsgSuspendAsset`.
- No retirement path may retire an asset named by `ReferenceState`.
- Consumer membership is not locked: swap eligibility, tax-cap membership,
  and liability inclusion are derived from asset status and target
  membership (`PricedLiveDenoms`, and status ∉ {PENDING, RETIRED} for
  liabilities), so nothing dangles when an asset leaves the live set.
- `PricedLiveVersion` advances whenever the priced-live set changes;
  consumers compare it each BeginBlocker instead of scanning.

`AssetLockKind` values 1-4, collection prefix 1, and error code 5 belonged
to the deleted lock index and stay burned.
```

6. **Emergency sections (~581-583, ~605-618)**: replace "each consumer executes its own fallback, moves its reference to it, and maintains its lock atomically" language with "the asset module promotes its owned fallback and rebases both consumers through the reference executors atomically".
7. **Genesis section (~893-894)**: replace "reconstructs locks from consumer genesis; imports no independent lock list;" with "imports the protocol reference state and priced-live epoch;".
8. **Line ~882** (dependency-cycle note): keep the cycle explanation but rename the interfaces to `MarketReferenceKeeper` / `TreasuryReferenceKeeper` and the injection to `SetReferenceConsumers`.
9. **Interfaces/summary lists (~818, ~839, ~944-955)**: remove `AssetLocks` / `asset_locks.go` entries; add `reference.go` and `priced_live.go`; replace "locks" in the scope summary with "the protocol reference and priced-live epoch".

- [ ] **Step 2: Self-check**

Run: `grep -n "lock" docs/ASSET_MODULE_PLAN.md`
Expected: no remaining references to asset locks except the burn note (case-insensitive check the hits).

- [ ] **Step 3: Commit**

```bash
git add docs/ASSET_MODULE_PLAN.md
git commit -m "docs(asset): rewrite module plan for reference ownership

The Asset locks section becomes Protocol reference: one asset-owned
reference and fallback with eligibility and retirement rules, derived
consumer membership, and the priced-live epoch. Ownership lists, state
model, emergency path, genesis, and interface sections drop lock language
and record the burned identifiers. The two-independent-fallbacks decision
is superseded per the 2026-07-29 asset consolidation spec."
```

---

### Task 12: final verification

- [ ] **Step 1: Full build and tests**

Run: `go build ./... && go test ./x/asset/... && go test ./...`
Expected: PASS everywhere (only `x/asset` and its protos changed; the module remains unwired).

- [ ] **Step 2: Proto lint**

Run: `make proto-lint`
Expected: clean.

- [ ] **Step 3: Residual sweep**

Run: `grep -rn "AssetLock\|ErrAssetLocked\|ExecuteBasePoolFallback\|ExecuteTaxCapFallback\|SetEmergencyConsumers" --include="*.go" x/ abci/ oracle/ app/ docs/`
Expected: no Go hits; doc hits only in the spec's historical narrative.

- [ ] **Step 4: Request review**

Use superpowers:requesting-code-review to review the branch against the spec before merging.
