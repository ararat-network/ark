# Pricefeed Decoupling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `x/oracle`'s params-driven single-pending target machinery with a feed registry (per-feed dated transitions), and slim the dormant `x/asset` to reference feeds through `price_source` instead of choreographing target membership.

**Architecture:** The 2026-07-28 per-denom transition mechanism moves from `x/asset` to `x/oracle`, re-keyed from asset denoms to opaque feed IDs. Oracle owns feed add/remove via two governance messages guarded by derivation-based referent checks; ABCI and the sidecar keep talking to oracle with reshaped types; `x/asset` gains `feed_id` + `completion_requested` and loses all target scheduling. Spec: `docs/superpowers/specs/2026-07-29-pricefeed-decoupling-design.md`.

**Tech Stack:** cosmos-sdk v0.54.2, collections, gogo+pulsar dual protogen (`make proto-gen`), mockgen, table-driven tests.

**Verification prefix:** run all Go tests as `GOCACHE=/private/tmp/ark-gocache go test ...`.

**Ground rules from CLAUDE.md that bind every task:** British spelling in test names; types tests are plain functions, keeper tests are suites; mutate-pattern for genesis/params validation; every commit message is subject + detailed body, no Co-Authored-By.

---

### Task 1: Additive oracle proto surface

Add the new messages alongside the legacy ones so nothing breaks. Deletions happen in Task 5.

**Files:**
- Modify: `proto/ark/oracle/v1/oracle.proto`
- Modify: `proto/ark/oracle/v1/tx.proto`
- Modify: `proto/ark/oracle/v1/query.proto`
- Modify: `proto/ark/oracle/v1/genesis.proto`
- Modify: `proto/ark/oracle/v1/event.proto`

- [ ] **Step 1: Add feed messages to `oracle.proto`**

Directly below the `PendingVoteTargets` message, add:

```protobuf
// Feeds defines the materialized active feed set and any scheduled per-feed
// transitions not yet activated. The feed set for a vote height is the active
// set folded with every transition whose activation height has arrived.
message Feeds {
  // feed_ids is the sorted unique active feed set.
  repeated string feed_ids = 1;
  // version advances once per activation batch.
  uint64 version = 2 [ (amino.dont_omitempty) = true ];
  // transitions are scheduled membership changes, sorted by
  // (activation_vote_height, feed_id). Immutable once written.
  repeated FeedTransition transitions = 3
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
}

// FeedTransition is one scheduled membership change for one feed. Immutable
// once written.
message FeedTransition {
  string feed_id = 1;
  FeedDirection direction = 2 [ (amino.dont_omitempty) = true ];
  int64 activation_vote_height = 3 [ (amino.dont_omitempty) = true ];
}

// FeedDirection identifies whether a transition adds or removes a feed.
enum FeedDirection {
  FEED_DIRECTION_UNSPECIFIED = 0;
  FEED_DIRECTION_ADD = 1;
  FEED_DIRECTION_REMOVE = 2;
}
```

- [ ] **Step 2: Add governance messages to `tx.proto`**

Inside `service Msg` after the `UpdateParams` rpc:

```protobuf
  // AddFeed schedules the addition of one price feed to the active feed set.
  rpc AddFeed(MsgAddFeed) returns (MsgAddFeedResponse) {}
  // RemoveFeed schedules the removal of one price feed from the active feed
  // set.
  rpc RemoveFeed(MsgRemoveFeed) returns (MsgRemoveFeedResponse) {}
```

After `MsgUpdateParamsResponse`:

```protobuf
// MsgAddFeed schedules one feed addition.
message MsgAddFeed {
  option (cosmos.msg.v1.signer) = "authority";
  option (amino.name) = "ark/x/oracle/MsgAddFeed";

  // authority is the address that controls the module (defaults to x/gov
  // unless overwritten).
  string authority = 1 [ (cosmos_proto.scalar) = "cosmos.AddressString" ];
  // feed_id is the feed to add.
  string feed_id = 2;
}

// MsgAddFeedResponse defines the Msg/AddFeed response type.
message MsgAddFeedResponse {}

// MsgRemoveFeed schedules one feed removal.
message MsgRemoveFeed {
  option (cosmos.msg.v1.signer) = "authority";
  option (amino.name) = "ark/x/oracle/MsgRemoveFeed";

  // authority is the address that controls the module (defaults to x/gov
  // unless overwritten).
  string authority = 1 [ (cosmos_proto.scalar) = "cosmos.AddressString" ];
  // feed_id is the feed to remove.
  string feed_id = 2;
}

// MsgRemoveFeedResponse defines the Msg/RemoveFeed response type.
message MsgRemoveFeedResponse {}
```

- [ ] **Step 3: Add the Feeds query to `query.proto`**

Inside `service Query` after the `VoteTargets` rpc (which Task 5 deletes):

```protobuf
  // Feeds returns the active feed set and scheduled transitions.
  rpc Feeds(QueryFeedsRequest) returns (QueryFeedsResponse) {
    option (cosmos.query.v1.module_query_safe) = true;
    option (google.api.http).get = "/ark/oracle/v1/feeds";
  }
```

After the `QueryVoteTargetsResponse` message:

```protobuf
// QueryFeedsRequest is the request type for the Query/Feeds RPC method.
message QueryFeedsRequest {}

// QueryFeedsResponse is the response type for the Query/Feeds RPC method.
message QueryFeedsResponse {
  Feeds feeds = 1
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
}
```

- [ ] **Step 4: Add the genesis field to `genesis.proto`**

In `GenesisState`, after the existing `vote_targets = 5` field (keep it for now — Task 5 removes it); the highest current field is `accounting = 6`, so:

```protobuf
  // feeds seeds the active feed set and scheduled transitions.
  Feeds feeds = 7
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
```

- [ ] **Step 5: Add feed events to `event.proto`**

```protobuf
// EventFeedTransitionScheduled is emitted when one feed transition is
// scheduled.
message EventFeedTransitionScheduled {
  string feed_id = 1;
  FeedDirection direction = 2 [ (amino.dont_omitempty) = true ];
  int64 activation_vote_height = 3 [ (amino.dont_omitempty) = true ];
  uint64 resulting_version = 4 [ (amino.dont_omitempty) = true ];
}

// EventFeedsActivated is emitted once per promoted activation batch.
message EventFeedsActivated {
  uint64 version = 1 [ (amino.dont_omitempty) = true ];
  repeated string added_feed_ids = 2;
  repeated string removed_feed_ids = 3;
}
```

Add `import "ark/oracle/v1/oracle.proto";` to `event.proto` if not present (needed for `FeedDirection`).

- [ ] **Step 6: Format, lint, generate**

Run: `make proto-format && make proto-lint && make proto-gen`
Expected: lint passes; `x/oracle/types/*.pb.go` and `api/ark/oracle/v1/*.pulsar.go` regenerate; `go build ./...` still passes (nothing consumes the new types yet).

- [ ] **Step 7: Commit**

```bash
git add proto/ api/ x/oracle/types/*.pb.go
git commit -m "feat(oracle): add feed registry proto surface

Adds Feeds/FeedTransition/FeedDirection state messages, MsgAddFeed and
MsgRemoveFeed governance messages, the Query/Feeds RPC, the genesis feeds
field, and feed lifecycle events. Additive only: the legacy VoteTargets
surface remains until the consensus cutover commit."
```

---

### Task 2: Oracle feed types — fold, phase, validation

Port `x/asset/types/oracle_targets.go` to `x/oracle/types/feeds.go`. The fold, phase, and apply logic is copied verbatim under these renames; validation swaps denom rules for feed-ID rules.

**Files:**
- Create: `x/oracle/types/feeds.go`
- Create: `x/oracle/types/feeds_test.go`
- Modify: `x/oracle/types/constants.go`
- Source (read-only this task): `x/asset/types/oracle_targets.go`, `x/asset/types/oracle_targets_test.go`

**Rename table (applies to code and tests):**

| From (asset) | To (oracle) |
| --- | --- |
| `OracleTargets` | `Feeds` |
| `OracleTargetTransition` | `FeedTransition` |
| `OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD` | `FeedDirection_FEED_DIRECTION_ADD` |
| `OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE` | `FeedDirection_FEED_DIRECTION_REMOVE` |
| `OracleTargetSet` | `FeedSet` |
| `OracleTargetPhase` / `OracleTargetPhaseOff/Adding/Active/Removing` | `FeedPhase` / `FeedPhaseOff/Adding/Active/Removing` |
| `ApplyOracleTargetTransition` | `ApplyFeedTransition` |
| `.Denoms` (field) | `.FeedIds` (field, from proto) |
| `.Denom` (transition field) | `.FeedId` |
| `MaxOracleTargets` | `MaxFeeds` |
| `NewOracleTargets` | `NewFeeds` |

- [ ] **Step 1: Add constants**

In `x/oracle/types/constants.go` add (keep the legacy trio until Task 5):

```go
	// MaxFeeds bounds the number of price feeds accepted by the oracle module
	// and represented in a validator's vote extension.
	MaxFeeds = 256

	// InitialFeedVersion identifies the genesis feed epoch.
	InitialFeedVersion uint64 = 1

	// FeedActivationDelayBlocks leaves one fully committed height between
	// scheduling a feed transition and using it in ExtendVote.
	FeedActivationDelayBlocks int64 = 2

	// MaxFeedIDLength bounds one feed identifier.
	MaxFeedIDLength = 16
```

- [ ] **Step 2: Write failing type tests**

Create `x/oracle/types/feeds_test.go` as plain table-driven functions (not a suite). Port every case from `x/asset/types/oracle_targets_test.go` under the rename table, replacing denom fixtures (`ausd`, `aeur`) with feed IDs (keep `ausd`-style IDs in some cases — they are valid feed IDs — and add clean-symbol cases like `usd`, `xau`). Then add the feed-ID validation cases:

```go
func TestValidateFeedID(t *testing.T) {
	tests := []struct {
		name    string
		feedID  string
		wantErr bool
	}{
		{name: "clean symbol", feedID: "usd", wantErr: false},
		{name: "denom-shaped id is legal", feedID: "ausd", wantErr: false},
		{name: "digits after first letter", feedID: "brent2026", wantErr: false},
		{name: "sixteen characters", feedID: "abcdefghijklmnop", wantErr: false},
		{name: "empty", feedID: "", wantErr: true},
		{name: "single character", feedID: "u", wantErr: true},
		{name: "leading digit", feedID: "1usd", wantErr: true},
		{name: "uppercase", feedID: "USD", wantErr: true},
		{name: "punctuation", feedID: "usd/eur", wantErr: true},
		{name: "seventeen characters", feedID: "abcdefghijklmnopq", wantErr: true},
		{name: "reserved noah", feedID: "noah", wantErr: true},
		{name: "reserved anoah", feedID: "anoah", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.ValidateFeedID(tt.feedID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
```

Mandatory ported coverage (same table shapes as the asset originals): `TestFeedsAtHeight` (no transitions; one transition before/at/after activation; two same-height records = one batch one bump; two consecutive-height records = two bumps; add and remove of different feeds in one batch), `TestFeedsValidate` (mutate pattern: zero version, unsorted/duplicate active IDs, invalid feed ID, reserved ID, duplicate transition per feed, ADD of active, REMOVE of absent, unsorted transitions, non-positive activation height, cap exceeded now, cap exceeded at future height via pending ADDs), `TestFeedsPhase` (all four phases, and a feed with no record while other records are in flight), `TestFeedTransitionValidate`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/types/... -run 'Feed' -v`
Expected: FAIL — `ValidateFeedID`, `NewFeeds`, fold methods undefined.

- [ ] **Step 4: Create `x/oracle/types/feeds.go`**

Copy `x/asset/types/oracle_targets.go` wholesale, apply the rename table, then make exactly these substantive edits:

1. Replace the denom validation inside `Validate()` and `FeedTransition.Validate()` — delete the `chain.ValidateNativeBaseDenom` + `NoahBaseDenom` checks and call `ValidateFeedID` instead. Drop the `ark/pkg/chain` import.
2. Add the ID validator:

```go
var feedIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)

// ValidateFeedID checks one feed identifier. Feed IDs are opaque: they may
// coincide with denomination strings because no consensus path mixes the two
// keyspaces. The numeraire has no feed, so its spellings are reserved.
func ValidateFeedID(feedID string) error {
	if !feedIDPattern.MatchString(feedID) {
		return fmt.Errorf(
			"feed id %q must match %s",
			feedID,
			feedIDPattern.String(),
		)
	}
	if feedID == "noah" || feedID == chain.NoahBaseDenom {
		return fmt.Errorf("feed id %q is reserved for the numeraire", feedID)
	}
	return nil
}
```

(keep the `chain` import just for `NoahBaseDenom` here). Keep `RequireOff` (renamed onto `FeedPhase`) — Task 7's `AmendRegistration` no longer uses it, but oracle-side tests do; if nothing references it after Task 7, delete it there, not here.

3. `NewFeeds(feedIDs []string) Feeds` mirrors `NewOracleTargets`: clone, sort, `Version: InitialFeedVersion`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/types/... -v`
Expected: PASS, including all pre-existing oracle types tests.

- [ ] **Step 6: Commit**

```bash
git add x/oracle/types/feeds.go x/oracle/types/feeds_test.go x/oracle/types/constants.go
git commit -m "feat(oracle): port feed fold, phase, and validation types

Ports the per-ID transition mechanism types from x/asset/types under the
feed naming, with feed-ID validation replacing native-denom validation.
Feed IDs are opaque lowercase identifiers; the numeraire spellings are
reserved. Adds MaxFeeds, InitialFeedVersion, FeedActivationDelayBlocks."
```

---

### Task 3: Oracle keeper mechanism — schedule, fold read, promote

Port the keeper mechanism. Promotion becomes pure set arithmetic + rate pruning + one event: the asset-status re-validation, status completions, and residual records from `x/asset/keeper/oracle_targets.go:260-447` **do not port** — that is asset-domain work the decoupling deletes.

**Files:**
- Create: `x/oracle/keeper/feeds.go`
- Create: `x/oracle/keeper/feeds_test.go` (suite-based, following the existing keeper test suite pattern in `x/oracle/keeper`)
- Modify: `x/oracle/keeper/keeper.go`
- Modify: `x/oracle/types/errors.go`
- Modify: `x/oracle/types/keys.go` (new collection prefix)

- [ ] **Step 1: Register errors**

Append to `x/oracle/types/errors.go` (codes 7-9 are the next free):

```go
	ErrFeedTransitionPending = sdkerrors.Register(ModuleName, 7, "conflicting feed transition is pending")
	ErrFeedReferenced        = sdkerrors.Register(ModuleName, 8, "feed is referenced by a consumer")
	ErrUnknownFeed           = sdkerrors.Register(ModuleName, 9, "unknown feed")
```

- [ ] **Step 2: Add the collection**

In `x/oracle/types/keys.go` the existing prefixes run 0-5 (`AccountingKey = 5`), so add:

```go
	FeedsKey = collections.NewPrefix(6)
```

In `x/oracle/keeper/keeper.go` add to the `Keeper` struct, next to `VoteTargets`:

```go
	Feeds collections.Item[types.Feeds]
```

and initialize it in the constructor exactly as `VoteTargets` is initialized, with `types.FeedsKey`, name `"feeds"`, `codec.CollValue[types.Feeds](cdc)`.

- [ ] **Step 3: Write failing keeper tests**

In `feeds_test.go`, following the module's existing keeper-suite fixture, cover:

- `TestScheduleFeedTransitionIdempotentSameDirection`: schedule ADD `xau` twice in one block — second call no-ops, one transition stored.
- `TestScheduleFeedTransitionOppositeDirectionConflicts`: ADD `xau`, then REMOVE `xau` before activation — expect `ErrFeedTransitionPending`.
- `TestScheduleFeedTransitionAddActiveNoOps` / `RemoveAbsentNoOps`.
- `TestScheduleFeedTransitionsSameBlockShareOneBatch`: ADD `xau` and `btc` in one block — both stored with the same activation height; after promotion the version advanced exactly once.
- `TestAdvanceFeedsPromotesDueBatchesInOrder`: records due at H and H+1 both overdue — version advances twice, sets fold in order.
- `TestAdvanceFeedsPrunesRemovedRates`: seed `ExchangeRate` for `ausd`, schedule REMOVE `ausd`, advance past activation — rate deleted; a rate for a surviving feed remains.
- `TestGetFeedsFoldsAtHeight`: with one in-flight transition, `GetFeeds(H-1)` excludes and `GetFeeds(H)` includes it, versions differ by one.
- `TestFeedPhaseReadsRegistry`: phases for active, adding, removing, off.

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/keeper/... -run 'Feed' -v`
Expected: FAIL — methods undefined.

- [ ] **Step 4: Create `x/oracle/keeper/feeds.go`**

Port from `x/asset/keeper/oracle_targets.go` with the Task 2 rename table, structured as:

```go
// GetFeeds returns the feed epoch validators must report for voteHeight.
func (k Keeper) GetFeeds(ctx context.Context, voteHeight int64) (types.FeedSet, error)

// FeedPhase returns feedID's relationship to the active set and its own
// scheduled transition, if any.
func (k Keeper) FeedPhase(ctx context.Context, feedID string) (types.FeedPhase, error)

// ScheduleFeedTransition stages one feed's membership change.
func (k Keeper) ScheduleFeedTransition(ctx context.Context, feedID string, direction types.FeedDirection) error

// prepareFeedTransition — verbatim port of prepareOracleTargetTransition
// (x/asset/keeper/oracle_targets.go:133-194) including the version-overflow
// guard and post-append Validate; compareFeedTransitions ports
// compareOracleTargetTransitions.

// AdvanceFeeds promotes every due transition batch after the previous vote
// height has been consumed. It must run after vote extensions are processed:
// the tally for vote height V reads the fold at V, and promoting a batch due
// at V before that read would validate votes against a set no validator could
// have seen. Removed feeds' rates are pruned here; the oracle owns both sides.
func (k Keeper) AdvanceFeeds(ctx context.Context) error
```

`ScheduleFeedTransition` validates `types.ValidateFeedID(feedID)` first, then mirrors the asset scheduler: read item, `prepareFeedTransition`, no-op return when unchanged, `Feeds.Set`, emit `EventFeedTransitionScheduled` with `ResultingVersion: scheduled.AtHeight(transition.ActivationVoteHeight).Version`.

`AdvanceFeeds` loops `dueFeedBatch` (port of `dueOracleTargetBatch`) and per batch:

```go
	promoted := types.Feeds{
		FeedIds:     slices.Clone(feeds.FeedIds),
		Version:     feeds.Version + 1,
		Transitions: slices.Clone(feeds.Transitions[len(batch):]),
	}
	added := make([]string, 0, len(batch))
	removed := make([]string, 0, len(batch))
	for _, transition := range batch {
		promoted.FeedIds = types.ApplyFeedTransition(promoted.FeedIds, transition)
		if transition.Direction == types.FeedDirection_FEED_DIRECTION_ADD {
			added = append(added, transition.FeedId)
		} else {
			removed = append(removed, transition.FeedId)
		}
	}
	if err := promoted.Validate(); err != nil { ... }
	for _, feedID := range removed {
		if err := k.ExchangeRate.Remove(ctx, feedID); err != nil { ... }
	}
	if err := k.Feeds.Set(ctx, promoted); err != nil { ... }
	// emit EventFeedsActivated{Version: promoted.Version, AddedFeedIds: added, RemovedFeedIds: removed}
```

(`collections.Map.Remove` on an absent key is a no-op in the SDK — no existence check needed; the legacy `AdvanceVoteTargets` relied on the same behavior.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/... -v`
Expected: PASS (legacy vote-target tests still pass; nothing legacy was touched).

- [ ] **Step 6: Commit**

```bash
git add x/oracle/keeper/feeds.go x/oracle/keeper/feeds_test.go x/oracle/keeper/keeper.go x/oracle/types/errors.go x/oracle/types/keys.go
git commit -m "feat(oracle): add feed registry keeper mechanism

Ports per-ID scheduling, the height fold read, and batched promotion from
the dormant x/asset implementation. Promotion is pure set arithmetic plus
internal rate pruning and one event per batch: the asset-status
re-validation and status completions do not port, because feed membership
no longer encodes asset lifecycle. Not yet wired to consensus."
```

---

### Task 4: Feed messages, referent guards, UpdateParams rewrite

**Files:**
- Modify: `x/oracle/keeper/msg_server.go`
- Create: `x/oracle/keeper/feed_guards.go`
- Modify: `x/oracle/types/codec.go` (register the two new messages, following the existing `MsgUpdateParams` registration pattern)
- Modify: `x/oracle/keeper/msg_server_test.go`

- [ ] **Step 1: Write failing msg tests**

Add to the msg server suite:

- `TestAddFeedSchedulesTransition`: authority submits `MsgAddFeed{FeedId: "xau"}` — one ADD transition stored, event emitted; non-authority signer rejected.
- `TestRemoveFeedRejectedWhileTobinTaxed`: params carry a Tobin entry for `ausd` (active feed); `MsgRemoveFeed{FeedId: "ausd"}` fails with `ErrFeedReferenced`.
- `TestRemoveFeedSucceedsWithoutReferents`: feed `xau` active, no Tobin entry — removal schedules.
- `TestUpdateParamsRejectsTobinEntryWithoutFeed`: params update introducing Tobin entry for `agbp` while `agbp` is not an active or adding feed — rejected; same update after `MsgAddFeed{agbp}` (transition in flight, phase Adding) — accepted.
- `TestUpdateParamsRejectsTobinEntryOnRemovingFeed`: feed `ausd` active with an in-flight REMOVE — introducing a Tobin entry for `ausd` is rejected.
- `TestUpdateParamsNoLongerSchedulesTargets`: a params update changing the Tobin set does not create any feed transition and does not touch `VoteTargets`.

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/keeper/... -run 'Feed|UpdateParams' -v`
Expected: FAIL.

- [ ] **Step 2: Create `x/oracle/keeper/feed_guards.go`**

```go
package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	"ark/x/oracle/types"
)

// FeedReferentGuard reports whether a consumer currently references a feed.
// Guards derive their answer from the consumer's own authoritative state at
// call time; nothing is indexed. Registration is hooks-style at app wiring.
type FeedReferentGuard interface {
	// FeedReferenced returns a human-readable referent description when the
	// consumer references feedID, or "" when it does not.
	FeedReferenced(ctx context.Context, feedID string) (string, error)
}

// SetFeedReferentGuards registers removal guards. Called once at wiring;
// this milestone registers only the module's own Tobin-tax guard.
func (k *Keeper) SetFeedReferentGuards(guards ...FeedReferentGuard) {
	k.feedReferentGuards = guards
}

// requireFeedUnreferenced rejects removal of a feed any registered consumer
// still depends on.
func (k Keeper) requireFeedUnreferenced(ctx context.Context, feedID string) error {
	for _, guard := range k.feedReferentGuards {
		referent, err := guard.FeedReferenced(ctx, feedID)
		if err != nil {
			return fmt.Errorf("checking feed %s referents: %w", feedID, err)
		}
		if referent != "" {
			return sdkerrors.Wrapf(types.ErrFeedReferenced, "%s: %s", feedID, referent)
		}
	}
	return nil
}

// tobinFeedReferentGuard pins feeds named by live Tobin-tax entries. It is
// the legacy stand-in for the asset price_source guard until x/asset
// activation; its authoritative state is the module's own params.
type tobinFeedReferentGuard struct{ k *Keeper }

func (g tobinFeedReferentGuard) FeedReferenced(ctx context.Context, feedID string) (string, error) {
	params, err := g.k.Params.Get(ctx)
	if err != nil {
		return "", fmt.Errorf("getting params: %w", err)
	}
	for _, tobinTax := range params.TobinTaxes {
		if tobinTax.Denom == feedID {
			return "tobin tax entry", nil
		}
	}
	return "", nil
}
```

Add `feedReferentGuards []FeedReferentGuard` to the `Keeper` struct and register the Tobin guard at the end of the keeper constructor: `k.feedReferentGuards = []FeedReferentGuard{tobinFeedReferentGuard{k: &k}}` (adjust to the constructor's actual value/pointer shape; `SetFeedReferentGuards` overrides at wiring when external guards arrive at activation).

- [ ] **Step 3: Add msg handlers to `msg_server.go`**

```go
// AddFeed schedules one feed addition.
func (m msgServer) AddFeed(ctx context.Context, msg *types.MsgAddFeed) (*types.MsgAddFeedResponse, error) {
	if m.k.authority != msg.Authority {
		return nil, sdkerrors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", m.k.authority, msg.Authority)
	}
	if err := m.k.ScheduleFeedTransition(ctx, msg.FeedId, types.FeedDirection_FEED_DIRECTION_ADD); err != nil {
		return nil, err
	}
	return &types.MsgAddFeedResponse{}, nil
}

// RemoveFeed schedules one feed removal after checking referent guards.
func (m msgServer) RemoveFeed(ctx context.Context, msg *types.MsgRemoveFeed) (*types.MsgRemoveFeedResponse, error) {
	if m.k.authority != msg.Authority {
		return nil, sdkerrors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", m.k.authority, msg.Authority)
	}
	if err := m.k.requireFeedUnreferenced(ctx, msg.FeedId); err != nil {
		return nil, err
	}
	if err := m.k.ScheduleFeedTransition(ctx, msg.FeedId, types.FeedDirection_FEED_DIRECTION_REMOVE); err != nil {
		return nil, err
	}
	return &types.MsgRemoveFeedResponse{}, nil
}
```

Match the authority-check idiom already used by `UpdateParams` in this file (reuse its exact error wrapping and imports).

- [ ] **Step 4: Rewrite the `UpdateParams` membership branch**

In `UpdateParams`, delete the `currentVoteTargets`/`nextVoteTargets` computation, the `ErrVoteTargetRemoval` loop, and the `ScheduleVoteTargets` call (`x/oracle/keeper/msg_server.go:41-58`). Replace the Tobin block with:

```go
	currentTobinDenoms := make(map[string]struct{}, len(currentParams.TobinTaxes))
	for _, tobinTax := range currentParams.TobinTaxes {
		currentTobinDenoms[tobinTax.Denom] = struct{}{}
	}
	for _, tobinTax := range msg.Params.TobinTaxes {
		if _, existing := currentTobinDenoms[tobinTax.Denom]; existing {
			continue
		}
		// A new Tobin entry is a new feed referent: it must name a feed in
		// phase Active or Adding. A feed with an in-flight removal does not
		// qualify, or a referent could appear inside the removal window.
		phase, err := m.k.FeedPhase(ctx, tobinTax.Denom)
		if err != nil {
			return nil, err
		}
		if phase != types.FeedPhaseActive && phase != types.FeedPhaseAdding {
			return nil, sdkerrors.Wrapf(
				types.ErrUnknownFeed,
				"tobin tax denom %s requires a feed in phase Active or Adding",
				tobinTax.Denom,
			)
		}
		m.k.registerTobinTaxMetadata(ctx, tobinTax.Denom)
	}
```

Note `ErrVoteTargetRemoval` (code 6) becomes unused here; it is deleted and its code burned in Task 5.

- [ ] **Step 5: Register messages in `x/oracle/types/codec.go`**

Follow the file's existing pattern for `MsgUpdateParams` exactly (legacy amino + interface registration) for `MsgAddFeed` and `MsgRemoveFeed`.

- [ ] **Step 6: Run tests**

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/... -v`
Expected: PASS. Legacy `UpdateParams` tests that asserted target scheduling now fail — rewrite those cases in this task to assert the new behavior (no transition created; coherence rules enforced).

- [ ] **Step 7: Commit**

```bash
git add x/oracle/keeper/msg_server.go x/oracle/keeper/feed_guards.go x/oracle/keeper/msg_server_test.go x/oracle/types/codec.go
git commit -m "feat(oracle): govern feed membership through AddFeed/RemoveFeed

Params updates stop driving membership. RemoveFeed is validated against
derivation-based referent guards, with the module-internal Tobin-tax guard
carrying the Phase-0 containment duty until x/asset activation; new Tobin
entries must name a feed in phase Active or Adding so no referent can be
created inside a removal window."
```

---

### Task 5: Consensus cutover and legacy deletion

The switch commit: genesis, queries, ABCI, and sidecar move to feeds; the entire legacy vote-target surface is deleted. Steps are ordered so package-scoped tests gate each layer; the repo compiles again at Step 9 and the task ends in one commit.

**Files:**
- Modify: `proto/ark/oracle/v1/oracle.proto`, `genesis.proto`, `query.proto` (deletions + reserved)
- Delete: `x/oracle/keeper/vote_targets.go`, `x/oracle/types/vote_targets.go` (+ their test files)
- Modify: `x/oracle/types/genesis.go`, `x/oracle/keeper/genesis.go`, `x/oracle/keeper/grpc_query.go`, `x/oracle/types/errors.go`, `x/oracle/types/constants.go`, `x/oracle/module/autocli.go`
- Modify: `abci/types/interfaces.go`, `abci/testutil/interfaces_mocks.go` (regenerated), `abci/oracle/vote_processor.go`, `abci/oracle/oracle_votes.go`, `abci/preblock/preblock.go`, plus their tests
- Modify: `oracle/sidecar/chainstate/polling.go` + tests

- [ ] **Step 1: Delete legacy proto surface**

In `oracle.proto`: delete the `VoteTargets` and `PendingVoteTargets` messages, leaving a comment in their place (proto3 reserves fields, not top-level message names):

```protobuf
// The VoteTargets and PendingVoteTargets messages were replaced by Feeds
// (2026-07-29 pricefeed decoupling); the names stay burned.
```

In `genesis.proto`: delete the `vote_targets` field and add inside `GenesisState`:

```protobuf
  reserved 5;
  reserved "vote_targets";
```

In `query.proto`: delete the `VoteTargets` rpc and its request/response messages; the HTTP route disappears with it. In `x/oracle/types/keys.go`, delete `VoteTargetsKey` and add `// Prefix 4 belonged to VoteTargetsKey and stays burned.`

Run: `make proto-format && make proto-lint && make proto-gen`
Expected: generation succeeds; `go build ./...` now FAILS — that is the work list for the next steps.

- [ ] **Step 2: Oracle types layer**

- Delete `x/oracle/types/vote_targets.go` and its test file.
- Delete from `constants.go`: `MaxVoteTargets`, `InitialVoteTargetVersion`, `VoteTargetActivationDelayBlocks`.
- In `errors.go`: delete `ErrVoteTargetRemoval` and add `// Code 6 belonged to ErrVoteTargetRemoval and stays burned.`
- Rewrite `x/oracle/types/genesis.go`: constructor takes `feeds Feeds` instead of `voteTargets VoteTargets`; `DefaultGenesisState` seeds `NewFeeds(denoms)` where `denoms` are the default params' Tobin denoms (inline the former `VoteTargetDenoms` loop); `Validate()` replaces the vote-target block (`genesis.go:143-163`) with:

```go
	if err := gs.Feeds.Validate(); err != nil {
		return err
	}
	for _, er := range gs.ExchangeRates {
		if _, found := slices.BinarySearch(gs.Feeds.FeedIds, er.Denom); !found {
			return fmt.Errorf(
				"genesis exchange rate %s is not an active feed",
				er.Denom,
			)
		}
	}
	// New Tobin referents may not appear inside a removal window, so genesis
	// holds the same coherence rule as MsgUpdateParams.
	for _, tobinTax := range gs.Params.TobinTaxes {
		phase := gs.Feeds.Phase(tobinTax.Denom)
		if phase != FeedPhaseActive && phase != FeedPhaseAdding {
			return fmt.Errorf(
				"tobin tax denom %s requires a feed in phase Active or Adding",
				tobinTax.Denom,
			)
		}
	}
```

- Update `genesis_test.go` with the mutate pattern over the new rules (valid default; rate for non-feed; Tobin entry on removing feed; Tobin entry on absent feed; roundtrip with a transition in flight; import with an overdue transition validates).

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/types/...`
Expected: PASS.

- [ ] **Step 3: Oracle keeper layer**

- Delete `x/oracle/keeper/vote_targets.go` (+ test). Remove the `VoteTargets` item from `keeper.go`.
- `keeper/genesis.go`: `InitGenesis` sets `k.Feeds.Set(ctx, data.Feeds)` where it set vote targets; `ExportGenesis` reads `k.Feeds`.
- `keeper/grpc_query.go`: delete the `VoteTargets` handler; add:

```go
// Feeds returns the active feed set and scheduled transitions.
func (q queryServer) Feeds(ctx context.Context, req *types.QueryFeedsRequest) (*types.QueryFeedsResponse, error) {
	feeds, err := q.k.Feeds.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryFeedsResponse{Feeds: feeds}, nil
}
```

(match the file's existing handler idiom and error mapping.)
- `module/autocli.go:59`: replace the `VoteTargets` query entry with `RpcMethod: "Feeds"`; add tx entries for `AddFeed`/`RemoveFeed` following the file's governance-message pattern (if governance-only messages are excluded from tx autocli in this module, mirror how `UpdateParams` is treated).
- Delete `GetVoteTargets` from `keeper.go:246-254`.

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/...`
Expected: PASS.

- [ ] **Step 4: ABCI interface and callers**

`abci/types/interfaces.go`: replace the two target methods:

```go
	GetFeeds(ctx context.Context, voteHeight int64) (oracletypes.FeedSet, error)
	AdvanceFeeds(ctx context.Context) error
```

Regenerate mocks:

Run: `mockgen -source=abci/types/interfaces.go -package testutil -destination abci/testutil/interfaces_mocks.go`

Update callers mechanically — the value flowing through is still a `{Version, []string}` set:
- `abci/oracle/vote_processor.go:28`: `oracleKeeper.GetFeeds(ctx, voteHeight)`; downstream variable renames (`voteTargets` → `feeds`).
- `abci/oracle/oracle_votes.go:77,131`: parameter type `oracletypes.FeedSet`; field access `.Denoms` → `.FeedIds`.
- `abci/preblock/preblock.go:99`: `h.oracleKeeper.AdvanceFeeds(ctx)`; keep the load-bearing ordering comment at `preblock.go:88`, rewording "vote targets" to "feeds".
- Rename remaining `VoteTargetSet` references across `abci/` and `abci/voteextension/` (grep `VoteTargetSet\|GetVoteTargets\|AdvanceVoteTargets`).

Run: `GOCACHE=/private/tmp/ark-gocache go test ./abci/...`
Expected: PASS — existing tests port mechanically (fixture types rename; single-boundary transition test now folds).

- [ ] **Step 5: Sidecar**

`oracle/sidecar/chainstate/polling.go`: `queryVoteTargets` becomes `queryFeeds`:

```go
	resp, err := query.Feeds(ctx, &oracletypes.QueryFeedsRequest{})
	if err != nil {
		return nil, fmt.Errorf("query oracle feeds: %w", err)
	}
	if resp == nil {
		return nil, errors.New("oracle feeds response is nil")
	}
	feeds := resp.Feeds
	if len(feeds.FeedIds) > oracletypes.MaxFeeds {
		return nil, fmt.Errorf(
			"active feed count %d exceeds maximum %d",
			len(feeds.FeedIds), oracletypes.MaxFeeds,
		)
	}
	if len(feeds.Transitions) > oracletypes.MaxFeeds {
		return nil, fmt.Errorf(
			"scheduled feed transition count %d exceeds maximum %d",
			len(feeds.Transitions), oracletypes.MaxFeeds,
		)
	}

	ids := slices.Clone(feeds.FeedIds)
	for _, transition := range feeds.Transitions {
		if transition.Direction == oracletypes.FeedDirection_FEED_DIRECTION_ADD {
			ids = append(ids, transition.FeedId)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
```

The warm-up union is now active ∪ ADD records; REMOVE records are deliberately not warmed. Update the chainstate tests accordingly (a REMOVE transition must not appear in the snapshot; an ADD must).

Run: `GOCACHE=/private/tmp/ark-gocache go test ./oracle/...`
Expected: PASS.

- [ ] **Step 6: Sweep for stragglers**

Run: `grep -rn "VoteTargets\|VoteTargetSet\|VoteTargetDenoms\|MaxVoteTargets\|ErrVoteTargetRemoval" --include="*.go" x/ abci/ oracle/ app/ cmd/ | grep -v "_test.go:.*burned"`
Expected: no hits outside generated `.pb.go` history (there should be none there either after proto-gen). Fix any found.

- [ ] **Step 7: Repo-wide build and test**

Run: `go build ./... && GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/... ./abci/... ./oracle/... ./app/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(oracle)!: cut consensus over to the feed registry

Genesis, queries, ABCI, and the sidecar switch from the single-pending
vote-target epoch to the feed fold. The legacy surface is deleted: params
no longer drive membership, the VoteTargets collection, types, query, and
ErrVoteTargetRemoval are gone (code 6 burned), and rate pruning stays
internal to AdvanceFeeds. Clean prelaunch genesis break; the vote wire and
attendance semantics are unchanged."
```

---

### Task 6: New ABCI coverage the decoupling makes load-bearing

**Files:**
- Modify: `abci/preblock/target_transition_test.go` (rename to `feed_transition_test.go`)
- Modify: `abci/oracle/aggregation_test.go`

- [ ] **Step 1: Consecutive-boundary test**

In the renamed `feed_transition_test.go`, alongside the ported single-boundary case: schedule transitions at H and H+1 (two different feeds). Drive vote heights H..H+3 through the preblock fixture, asserting at every height that the extension's `target_version` equals what the tally validates, attendance is recorded for every functioning block, and the stored version advances exactly twice.

- [ ] **Step 2: Ordering pin test**

Add `TestPreblockConsumesVoteExtensionsBeforePromotingFeeds`: schedule a transition due exactly at vote height V; run the preblock for the block that both tallies V and promotes the batch; assert the tally accepted extensions built against the pre-promotion fold (version v) while the post-block stored version is v+1. Use mock ordering assertions (`gomock.InOrder(mockKeeper.EXPECT().GetFeeds(...), mockKeeper.EXPECT().AdvanceFeeds(...))`) so the test fails if `abci/preblock/preblock.go` is ever reordered.

- [ ] **Step 3: Aggregation version sequence**

The recording keeper around `abci/oracle/aggregation_test.go:810` hardcodes one version — extend the fixture to serve a version sequence and add a case where the version changes between consecutive tallies, asserting each tally validates against its own height's fold.

- [ ] **Step 4: Run and commit**

Run: `GOCACHE=/private/tmp/ark-gocache go test ./abci/... -v`
Expected: PASS.

```bash
git add abci/
git commit -m "test(abci): pin feed-fold ordering and consecutive boundaries

Adds the consecutive-boundary vote/tally agreement case, the
consume-before-promote ordering pin the correctness argument depends on,
and aggregation coverage across a version sequence."
```

---

### Task 7: Slim x/asset — price_source, completion_requested, target machinery deletion

One commit; internal steps are gated by package-scoped tests (types compiles independently of keeper).

**Files:**
- Modify: `proto/ark/asset/v1/asset.proto`, `genesis.proto`, `query.proto`, `tx.proto`, `event.proto`
- Delete: `x/asset/types/oracle_targets.go`, `x/asset/types/oracle_targets_test.go`, `x/asset/keeper/oracle_targets.go` (+ its test)
- Modify: `x/asset/types/{asset.go,genesis.go,constants.go,errors.go,expected_keepers.go,keys.go}` and tests
- Modify: `x/asset/keeper/{keeper.go,lifecycle.go,lifecycle_completion.go,genesis.go,grpc_query.go,msg_server.go,emergency_mandate.go}` and tests
- Modify: `x/asset/testutil/expected_keepers_mocks.go` (regenerated), `x/asset/module/autocli.go`

- [ ] **Step 1: Asset proto slim**

`asset.proto` — replace `Asset` field 5 and add the intent flag. (The spec sketches `price_source` as a oneof; this plan realizes it as a plain string with field 6 reserved for the future `basket_id` — identical semantics, and it avoids gogo oneof friction. Do not "restore" the oneof.)

```protobuf
  // feed_id names the oracle feed pricing this asset, normalized as units of
  // the feed's quote unit per one NOAH. Empty for an unpriced asset.
  string feed_id = 5;
  // Field 6 is reserved for basket_id: pricing by a derived basket rate.
  reserved 6;
  // completion_requested marks an automatic completion in progress: activation
  // while PENDING, recovery while SUSPENDED. It must be false in every other
  // status.
  bool completion_requested = 7 [ (amino.dont_omitempty) = true ];
```

Delete `OracleTargets`, `OracleTargetTransition`, `OracleTargetDirection` messages (their concepts re-homed in Task 1; leave a comment that the names moved to `ark.oracle.v1`). `genesis.proto`: delete `oracle_targets` field 2 → `reserved 2; reserved "oracle_targets";`. `query.proto`: delete the `OracleTargets` rpc + its messages. `event.proto`: delete `EventOracleTargetTransitionScheduled`/`EventOracleTargetsActivated` if present; in `EventRegistrationAmended` replace `bool oracle_required = 3` with `string feed_id = 3`. `tx.proto`: in `MsgRegisterAsset` replace `bool oracle_required = 3` with `string feed_id = 3`; in `MsgAmendRegistration` replace `bool oracle_required = 4` with `string feed_id = 4`.

Run: `make proto-format && make proto-lint && make proto-gen`

- [ ] **Step 2: Types layer**

- Delete `x/asset/types/oracle_targets.go` + test (already ported).
- `constants.go`: delete `MaxOracleTargets` / `OracleTargetActivationDelayBlocks` (now oracle-owned).
- `errors.go`: delete `ErrOracleTargetTransitionPending` if now unused; keep its code registered-or-burned with a comment, matching the module's burned-code convention.
- `expected_keepers.go`: extend the oracle expected keeper with the phase read (mirror the existing `GetRateSet` declaration style):

```go
	// FeedPhase reports feedID's relationship to the active feed set.
	FeedPhase(ctx context.Context, feedID string) (oracletypes.FeedPhase, error)
```

- `asset.go`: add the pricing-mode helper that every current `OracleRequired` boolean read swaps to (there are consumers beyond the lifecycle: `x/asset/keeper/settlement.go:51,72,239`, `x/asset/types/genesis.go:44,206,249,259,266`, `x/asset/keeper/msg_server.go:40,65`, `x/asset/types/asset.go:122` — find them all with `grep -rn "OracleRequired" x/asset --include="*.go" | grep -v ".pb.go"`):

```go
// OraclePriced reports whether the asset declares a price feed.
func (a Asset) OraclePriced() bool {
	return a.FeedId != ""
}
```

Every semantic that read `OracleRequired` keeps its meaning under `OraclePriced()`: settlement eligibility, liability classification, priced-live membership (`priced_live.go` and `valuation.go` additionally re-key their `RateSet` lookups from `asset.Denom` to `asset.FeedId`), and the genesis rules that survive.

- `asset.go` `Validate()`: replace the `OracleRequired` handling with:

```go
	if a.FeedId != "" {
		if err := oracletypes.ValidateFeedID(a.FeedId); err != nil {
			return err
		}
	}
	if a.CompletionRequested &&
		a.Status != AssetStatus_ASSET_STATUS_PENDING &&
		a.Status != AssetStatus_ASSET_STATUS_SUSPENDED {
		return fmt.Errorf(
			"%s asset %s cannot carry a requested completion",
			a.Status, a.Denom,
		)
	}
	if a.CompletionRequested && a.FeedId == "" {
		return fmt.Errorf(
			"asset %s cannot request completion without a price feed",
			a.Denom,
		)
	}
```

- `genesis.go` `Validate()`: delete every status↔target rule; the `completion_requested` placement rule arrives free through per-asset `Validate()`. Update `genesis_test.go` with mutate cases: flag on `ACTIVE` asset rejected; flag without feed rejected; invalid feed ID rejected.

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/asset/types/...`
Expected: PASS.

- [ ] **Step 3: Regenerate expected-keeper mocks**

Run: `mockgen -source=x/asset/types/expected_keepers.go -package testutil -destination x/asset/testutil/expected_keepers_mocks.go`

- [ ] **Step 4: Keeper layer — deletions and helpers**

- Delete `x/asset/keeper/oracle_targets.go` (+ test file). Remove the `OracleTargets` item from `keeper.go:30` and its `collections.NewItem` block; keep the prefix byte burned with a comment in `keys.go`.
- In the status-write helper (`setAssetStatus` / `Complete` call path), clear intent on every status change: when the new status differs from the old, set `CompletionRequested = false` on the updated asset before persisting. Entering a carrying status starts clean; `BeginRecovery`'s `WRITTEN_OFF → SUSPENDED` move therefore changes status first, then sets the flag in the same update.
- Delete `keeper/grpc_query.go`'s `OracleTargets` handler and its autocli entry.

- [ ] **Step 5: Keeper layer — the seven entry points**

Apply these exact transformations in `lifecycle.go` (current line refs against this commit):

1. `RegisterAsset` (`:17`): signature `oracleRequired bool` → `feedID string`; construct `Asset{... FeedId: feedID}`; when `feedID != ""` require oracle phase:

```go
	if feedID != "" {
		phase, err := k.oracleKeeper.FeedPhase(ctx, feedID)
		if err != nil {
			return err
		}
		if phase != oracletypes.FeedPhaseActive && phase != oracletypes.FeedPhaseAdding {
			return sdkerrors.Wrapf(
				types.ErrInvalidAssetTransition,
				"asset %s feed %s must be in phase Active or Adding",
				metadata.Base, feedID,
			)
		}
	}
```

2. `AmendRegistration` (`:72`): signature swap as above; replace the `OracleTargets`/`RequireOff` block (`:106-114`) with: changing `FeedId` requires `!asset.CompletionRequested`; a non-empty new feed passes the same phase check as registration.
3. `ActivateAsset` (`:146`): delete the target block (`:160-188`); new body after the `PENDING` check:

```go
	if asset.FeedId == "" {
		return k.setAssetStatus(ctx, asset, types.AssetStatus_ASSET_STATUS_ACTIVE)
	}
	if asset.CompletionRequested {
		return nil // idempotent: activation already in progress
	}
	phase, err := k.oracleKeeper.FeedPhase(ctx, asset.FeedId)
	if err != nil {
		return err
	}
	if phase != oracletypes.FeedPhaseActive && phase != oracletypes.FeedPhaseAdding {
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s feed %s must be in phase Active or Adding",
			denom, asset.FeedId,
		)
	}
	updated := asset
	updated.CompletionRequested = true
	return k.advanceAsset(ctx, updated)
```

4. `ResumeIssuance` (`:221`): delete the target block (`:234-255`) entirely — the "no removal pending" precondition dies with target state; status check remains the guard.
5. `suspendAsset` (`:274`): delete the whole target block and `scheduleRemoval` tail (`:302-346`); the function becomes: status check → reference check → `setAssetStatus(SUSPENDED)` (which clears any recovery flag — harmless here since carrying statuses can't be suspended... `ISSUANCE_HALTED` carries no flag; correct by construction).
6. `BeginRecovery` (`:352`): keep every settlement rule; delete the target block (`:420-444`); keep the unpriced-no-plan shortcut to `ISSUANCE_HALTED`. For the priced path set the flag:

```go
	recoveryStatus := types.AssetStatus_ASSET_STATUS_SUSPENDED
	if asset.FeedId == "" && !hasPlan {
		recoveryStatus = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	}
	requestCompletion := asset.FeedId != "" && recoveryStatus == types.AssetStatus_ASSET_STATUS_SUSPENDED
```

then: if status changes, `setAssetStatus` first and re-read/carry the updated asset; apply `CompletionRequested = requestCompletion` (plus the plan mutation) in a single `advanceAsset` update so the version advances exactly once per the existing `statusChanged`/`planChanged` version discipline. Also require the feed phase Active or Adding when `requestCompletion` (same check as `ActivateAsset` — the feed may have been legitimately removed during suspension, and recovery is a referent-creating path).
7. `CancelRecovery` (`:486`): delete the target block; new body after the `SUSPENDED` check:

```go
	if !asset.CompletionRequested {
		return nil // recovery already cancelled
	}
	updated := asset
	updated.CompletionRequested = false
	return k.advanceAsset(ctx, updated)
```

8. `FinalizeRetirement` (`:549`): keep every supply/residual/reference/plan rule (`:555-633`); delete the entire phase switch (`:635-693`); the function proceeds directly to `setAssetStatus(RETIRED)` and the existing `ISSUANCE_HALTED` residual append (`:698-703`). All retirement is now immediate.

`emergency_mandate.go`: verify with `grep -n "scheduleOracle" x/asset/keeper/emergency_mandate.go` — the committee path reaches scheduling only through `suspendAsset`, so no edits expected; if a direct call exists, delete it.

- [ ] **Step 6: Keeper layer — completion re-key**

Rewrite `prepareLifecycleCompletions` (`lifecycle_completion.go:55`): completions are driven by intent, not by rate arrival per denom. Replace the rate-denoms iteration with a walk over flagged assets:

```go
func (k Keeper) prepareLifecycleCompletions(
	ctx context.Context,
	updatedRates oracletypes.RateSet,
) ([]lifecycleCompletion, error) {
	var completions []lifecycleCompletion
	err := k.Assets.Walk(ctx, nil, func(denom string, asset types.Asset) (bool, error) {
		if !asset.CompletionRequested {
			return false, nil
		}
		rate, fresh := updatedRates[asset.FeedId]
		if !fresh {
			return false, nil
		}
		if rate.IsNil() || !rate.IsPositive() {
			return false, sdkerrors.Wrapf(
				types.ErrAssetNotPriceable,
				"fresh Oracle rate for feed %s must be positive",
				asset.FeedId,
			)
		}
		newStatus := types.AssetStatus_ASSET_STATUS_ACTIVE
		if asset.Status == types.AssetStatus_ASSET_STATUS_SUSPENDED {
			hasPlan, err := k.SettlementPlans.Has(ctx, denom)
			if err != nil {
				return false, fmt.Errorf(
					"checking settlement plan for recovering asset %s: %w", denom, err,
				)
			}
			if hasPlan {
				return false, nil
			}
			newStatus = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
		}
		updated := asset
		updated.CompletionRequested = false
		if err := updated.Complete(newStatus); err != nil {
			return false, err
		}
		completions = append(completions, lifecycleCompletion{before: asset, after: updated})
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return completions, nil
}
```

The flag's placement invariant makes the old PENDING/SUSPENDED status filter redundant, but `Complete` still enforces legal moves. The `chain.NoahBaseDenom` filtering of rate keys is no longer needed (rates are feed-keyed and the numeraire has no feed). The walk visits every asset, but the registry holds a handful of records and completions run only in blocks that produced fresh rates — the same cost class as before.

- [ ] **Step 7: Keeper genesis + msg server**

- `keeper/genesis.go`: drop `OracleTargets` import/export. In `InitGenesis`, after assets import, enforce the cross-module rule: for each asset — `ACTIVE`/`ISSUANCE_HALTED` with `FeedId` require oracle `FeedPhase == Active`; any `CompletionRequested` carrier requires `Active` or `Adding`; other statuses unconstrained (a suspended or written-off asset's feed may legitimately be gone).
- `msg_server.go`: thread the `feedID string` signature changes for `RegisterAsset`/`AmendRegistration`; event field `OracleRequired` → `FeedId`.
- `module/autocli.go`: drop the `OracleTargets` query entry.

- [ ] **Step 8: Tests**

Update existing lifecycle/keeper suites for the new signatures and deleted machinery, and add the design's mandated cases:

- `TestActivateAssetSharedLiveFeedRequiresExplicitActivation`: register asset `bgold` with `FeedId: "xau"` while feed `xau` is active and a fresh rate exists; run `CompleteLifecycle` with that rate — asset stays `PENDING`. After `ActivateAsset`, the same completion moves it to `ACTIVE`.
- `TestSuspendedAssetWithFreshFeedDoesNotAutoRecover`: suspended asset, feed stayed active with fresh rates — no status change until `BeginRecovery`; after `BeginRecovery` + fresh rate + no plan, completes to `ISSUANCE_HALTED` and clears the flag.
- `TestFinalizeRetirementIsImmediate`: from `PENDING` and from priced `ISSUANCE_HALTED` (with residual record appended at the transition, bound enforced).
- `TestCancelRecoveryClearsIntent` (idempotent second call no-ops without version bump).
- `TestExitTransitionsClearCompletionIntent`: `FinalizeRetirement` on a flagged `PENDING` asset and `WriteOffAsset` on a flagged `SUSPENDED` asset leave the flag false.
- `TestRegisterAssetRejectsRemovingFeed`: feed with in-flight REMOVE rejected as a price source.

Run: `GOCACHE=/private/tmp/ark-gocache go test ./x/asset/... && go build ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor(asset)!: reference feeds through price_source

Assets declare feed_id instead of deriving oracle membership from
lifecycle transitions. The OracleTargets item, per-denom scheduling, the
derived phase table, and every status-target rule are deleted; retirement
paths become immediate single transitions. completion_requested replaces
phase-encoded activation and recovery intent, cleared by construction on
every exit from a carrying status, and completions key RateSet lookups by
the asset's feed."
```

---

### Task 8: Documentation pass

**Files:**
- Modify: `docs/ASSET_MODULE_PLAN.md`
- Modify: `docs/superpowers/specs/2026-07-28-oracle-target-transitions-design.md`
- Modify: `docs/superpowers/specs/2026-07-29-asset-consolidation-design.md`
- Modify: `abci/preblock/README.md`, `abci/voteextension/README.md`, `abci/oracle/README.md`

- [ ] **Step 1: `ASSET_MODULE_PLAN.md` coherence pass**

Per the spec's "Documentation updates" section: core decisions (deferral replaced, reversal recorded, pointer to the new spec), ownership model (oracle owns feeds/epochs; asset owns `price_source`), price contract (feed-keyed), state model (drop `OracleTargets`, add `feed_id`/`completion_requested`), lifecycle operations (no scheduling; immediate retirement), delete the "Derived Oracle state" section, re-home "Oracle target epochs" as a pointer to oracle's feed registry, sidecar mapping, genesis, application wiring (preblock steps 4-5 merged into oracle), Phase 3 rescoped to wiring + completions + Bank metadata.

- [ ] **Step 2: Spec cross-references**

Top of the 2026-07-28 spec, under Status: `Superseded-by note: the mechanism shipped verbatim, re-homed to x/oracle and re-keyed by feed ID — see 2026-07-29-pricefeed-decoupling-design.md.` In the consolidation spec, update reference-eligibility wording: "active Oracle-target phase" → "referenced feed in phase Active".

- [ ] **Step 3: ABCI READMEs**

Replace single-pending-epoch descriptions with the fold, and target/denom vocabulary with feeds where the text describes membership (rates and attendance text mostly survives).

- [ ] **Step 4: Commit**

```bash
git add docs/ abci/*/README.md
git commit -m "docs: align plans and READMEs with the pricefeed decoupling

Amends ASSET_MODULE_PLAN for oracle-owned feeds and asset price sources,
marks the 07-28 target-transitions spec superseded with the mechanism
preserved, updates consolidation reference-eligibility wording, and
replaces single-pending-epoch descriptions in the abci READMEs."
```

---

### Task 9: Full verification sweep

- [ ] **Step 1: Run the complete gate**

```bash
make proto-format && make proto-lint
go build ./...
GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/... ./x/asset/... ./x/market/... ./x/treasury/... ./abci/... ./oracle/... ./app/...
GOCACHE=/private/tmp/ark-gocache go test ./...
git diff --check
```

Expected: everything passes; `git status` clean apart from intended changes.

- [ ] **Step 2: Attendance-preservation spot check**

Run: `GOCACHE=/private/tmp/ark-gocache go test ./abci/oracle/... -run 'Attendance|Aggregate' -v`
Expected: PASS with no behavioral edits to those tests beyond type renames — the cutover's attendance-preservation requirement is that grading semantics never changed.

- [ ] **Step 3: Fix anything found, amend or add commits per the repo's commit-splitting rules.**
