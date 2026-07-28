# Per-Denom Oracle Target Transitions

Date: 2026-07-28
Status: approved design, pending implementation plan

## Problem

Oracle target membership is scheduled through a single chain-global pending
epoch. `OracleTargets` holds an active `{denoms, version}` plus at most one
`Pending {denoms, version, activation_vote_height}`
(`proto/ark/asset/v1/asset.proto:84-95`). Staging is guarded by
`prepareOracleTargetSchedule` (`x/asset/keeper/oracle_targets.go:132`): if a
pending epoch exists and the requested denom set differs from it, the call
fails with `ErrOracleTargetTransitionPending`.

Every scheduling entry point computes its next set from the **active** denoms
via `oracleTargetDenomsWith(oracleTargets.Denoms, denom, ...)`
(`x/asset/keeper/oracle_targets.go:57`), so a second denom's request can never
equal the staged set. The result is one single-denom change per epoch, and one
epoch per two blocks.

Three consequences motivate this redesign:

1. **Bulk throughput.** N asset transitions require N proposals and at least 2N
   blocks. A proposal batching several `MsgActivateAsset` messages fails at the
   second message.
2. **Emergency multi-suspend.** Scheduling is embedded inside lifecycle
   transitions. `suspendAsset` (`x/asset/keeper/lifecycle.go:519`) checks the
   asset's own phase, writes `SUSPENDED`, then schedules removal. When another
   denom occupies the pending slot the schedule fails and the **entire
   suspension reverts** — in exactly the correlated-failure scenario where the
   emergency committee needs to act on several assets at once.
3. **Hard-fail collisions.** Two governance proposals whose execution lands in
   the same two-block window: the second fails permanently. Governance does not
   retry execution, so the change must be re-proposed.

The global mutual exclusion is a stronger condition than consensus safety
requires. Safety needs only that a scheduled change is **immutable once
written** and **activates at schedule height + 2**; see "Correctness argument"
below. Mutual exclusion is surplus strictness, and it is the sole source of all
three problems.

## Non-goals

- Cancelling or amending an in-flight transition. A scheduled record runs to
  activation; reversing it means scheduling the opposite direction afterwards.
- Changing the two-height activation delay. It is preserved per record.
- Per-target vote-accounting grace ("never penalize a validator for a target
  that has never produced an aggregated rate",
  `docs/ASSET_MODULE_PLAN.md:519`). Not implemented today; deliberately left to
  a follow-up spec. See "Deferred work".
- Changing the vote-extension wire format.

## Approach

Replace the single pending epoch with **per-denom dated transition records**.
The target set at a vote height is the materialized active set folded with
every record whose activation height has arrived. Contention becomes per denom,
which is the natural unit of change; unrelated assets stop blocking each other.

Records are short-lived: written at H, absorbed into the materialized set at
H+2, then deleted. The in-flight set is bounded by transitions scheduled in the
last two blocks, so it is normally empty and never large. Storage therefore
stays a single `collections.Item` and read costs stay flat — see "Cost
profile".

This is *not* derive-from-status-on-read. Membership is derived from asset
status once, at **write** time, when a lifecycle transition emits a dated
record. Reads touch only the materialized set plus the small record list.

## Scope: Phase 3 cutover bundled

The mechanism is implemented **once, in `x/asset`**, completing the Phase 3
target-ownership cutover that `docs/ASSET_MODULE_PLAN.md` leaves pending. The
alternative — rebuilding inside `x/oracle` now and porting later — means
building the fold twice.

Concretely:

- `abci/preblock` and `abci/voteextension` resolve targets through a new narrow
  target keeper interface backed by the asset keeper, replacing
  `GetVoteTargets` / `AdvanceVoteTargets` on
  `arkabcitypes.OracleKeeper` (`abci/types/interfaces.go:22-23`).
- `arkabcitypes.OracleKeeper` retains rates, params, and vote accounting,
  backed by `x/oracle` as today.
- Deleted from `x/oracle`: the `VoteTargets` collection, `ScheduleVoteTargets`,
  `AdvanceVoteTargets` (`x/oracle/keeper/vote_targets.go`), the target-set
  types in `x/oracle/types/vote_targets.go`, and the target-scheduling branch
  of `UpdateParams` (`x/oracle/keeper/msg_server.go:41-58`).
- Tobin taxes remain oracle params. Params stop driving target membership;
  membership is asset-lifecycle-driven only. This removes the legacy
  bulk-add-but-never-remove path (`ErrVoteTargetRemoval`), whose capability is
  subsumed by per-denom records.

## State model

`x/asset` keeps one `collections.Item`. `proto/ark/asset/v1/asset.proto`:

```protobuf
// OracleTargets defines the materialized active target set and any scheduled
// per-denom transitions not yet activated.
message OracleTargets {
  repeated string denoms = 1;                      // sorted, unique
  uint64 version = 2;                              // bumps once per activation batch
  repeated OracleTargetTransition transitions = 3; // sorted by (height, denom)
}

// OracleTargetTransition is one scheduled membership change for one denom.
// Immutable once written.
message OracleTargetTransition {
  string denom = 1;
  OracleTargetDirection direction = 2;
  int64 activation_vote_height = 3;
}

enum OracleTargetDirection {
  ORACLE_TARGET_DIRECTION_UNSPECIFIED = 0;
  ORACLE_TARGET_DIRECTION_ADD = 1;
  ORACLE_TARGET_DIRECTION_REMOVE = 2;
}
```

`PendingOracleTargets` (`asset.proto:91`) is deleted, as is its `x/oracle` twin
(`proto/ark/oracle/v1/oracle.proto:77`).

`Validate()` invariants, replacing the current active/pending rules
(`x/asset/types/oracle_targets.go:95`):

- `version > 0`; active denoms sorted, unique, valid native base denoms, never
  `NoahBaseDenom` — unchanged from `validateVoteTargetDenoms`.
- At most one transition per denom.
- `direction = ADD` requires the denom absent from active; `REMOVE` requires it
  present.
- `activation_vote_height > 0`; transitions sorted by
  `(activation_vote_height, denom)`.
- `len(denoms) + #ADD ≤ MaxOracleTargets` (256), so the fold can never exceed
  the cap at any future height.
- The `Pending.Version == Version + 1` rule is replaced by the version
  arithmetic below.

Note the record list is strictly smaller on the wire than the current schema,
which carries a full duplicate copy of the denom set in `Pending`.

## Height-addressable reads

`AtHeight(V)` folds: start from `(denoms, version)`; apply every transition with
`activation_vote_height <= V` in sorted order; bump the version **once per
distinct activation height**. An activation height is a *batch*: any number of
records sharing one height advance the version by exactly one.

`Phase(denom)` (`x/asset/types/oracle_targets.go:75`) keeps its four values —
`Off`, `Adding`, `Active`, `Removing` — derived from active membership plus that
denom's own record, if any. The values become truthful per denom: `Adding` now
means *this* denom is being added, not that some denom is. Every lifecycle gate
and `RequireOff` keeps its current meaning while cross-denom blocking
disappears.

## Scheduling

Status preconditions in the seven lifecycle entry points (`ActivateAsset`,
`CancelAssetRegistration`, `SuspendAsset`, `CancelRecovery`, `BeginRecovery`,
`FinalizeRetirement`, `ResumeIssuance`) are unchanged. Only the schedule step
changes, and it becomes per denom:

| Situation | Result |
|---|---|
| Same-direction record already in flight | No-op, success (idempotent retry) |
| Opposite-direction record in flight | `ErrOracleTargetTransitionPending`, scoped to this denom |
| ADD of an already-active denom | No-op |
| REMOVE of an absent denom | No-op |
| Would exceed `MaxOracleTargets` | Error at schedule time |
| Otherwise | Append `{denom, direction, blockHeight + OracleTargetActivationDelayBlocks}` |

`prepareOracleTargetSchedule`'s global exclusion check is deleted. Each schedule
emits a per-denom `EventOracleTargetTransitionScheduled {denom, direction,
activation_vote_height, resulting_version}`, replacing
`EventOracleTargetsScheduled`.

Removals are never cap-blocked, so emergency actions cannot fail on the cap. N
schedules in one block produce N records sharing one activation height: one
batch, one version bump. Emergency multi-suspend and bulk proposals need no
special casing.

## Promotion

`AdvanceOracleTargets` (`x/asset/keeper/oracle_targets.go:194`) runs in preblock
**after** `ProcessVoteExtensions`. That ordering is already the code order
(`abci/preblock/preblock.go:82-88`); this design promotes it to a documented
consensus invariant with a test that pins it.

Promotion processes all records with `activation_vote_height <= blockHeight`,
grouped into batches by height, in height order:

1. Per batch, re-validate each record's asset (the existing status checks at
   `oracle_targets.go:209-295`, unchanged in substance): additions require
   `PENDING` or `SUSPENDED`; removals map `PENDING`/`ISSUANCE_HALTED` to
   `RETIRED` via `Complete`, while `SUSPENDED`/`WRITTEN_OFF` skip the status
   move because they already moved at schedule time.
2. Append retirement-residual `ResolutionRecord`s for removed
   `ISSUANCE_HALTED` assets with positive supply, guarded by
   `requireResolutionRecordAbsent` before any write, as today.
3. Apply the set delta, bump the version once, delete the batch's records.
4. Emit `EventOracleTargetsActivated {version, added_denoms, removed_denoms}`
   per batch, then residual and status-change events as today.

`x/asset` does not own exchange rates and must not prune them directly. As
today (`x/asset/keeper/oracle_targets.go:191-193`), `AdvanceOracleTargets`
**returns the removed denoms** — now accumulated across every batch promoted in
this block — and the preblock handler passes them to the oracle keeper for rate
pruning. Pruning therefore stays behind the `arkabcitypes.OracleKeeper`
boundary, and the asset keeper's return value becomes the sole channel.

Re-validation failures return an error from preblock and halt the chain, exactly
as today. The phase gates on every mutating path are what make those states
unreachable; that property is preserved because the gates now key on the denom's
own record.

Rate pruning becomes per record rather than per whole-set diff. A denom removed
in one batch and added in a later one loses its rate and must re-warm — correct
behavior, since a rate that survived removal would be stale on return.

## Correctness argument

The concern this design must answer: `ExtendVote` for vote height V runs against
committed state V−1, while the tally for V runs in block V+1 against committed
state V. If a transaction could change the target set with immediate effect, the
tally would validate votes against a set the voter could not have seen.

Two invariants close the gap, and neither requires a global pending slot:

1. A record is **immutable once written**.
2. `activation_vote_height = schedule height + 2`.

For any vote height V:

- A record affects `fold(V)` only if its activation ≤ V, so it was written in a
  block ≤ V−2.
- The voter computes `fold(V)` on state V−1, which contains everything committed
  through V−2. The tally computes `fold(V)` on state V, which contains the same
  records. Immutability means the contents are identical, so both compute the
  same set and the same version.
- Records written in blocks V−1 or V exist in one party's state and not the
  other's, but their activation heights are ≥ V+1, so `fold(V)` excludes them in
  both.

The argument is per record and holds for any number of records in flight,
including records activating at consecutive heights.

**Promotion lag.** Folding is lossy in one direction: once a batch is absorbed
into the materialized set, `AtHeight(V)` for a height before that batch returns
the newer set. This is safe because the only historical height ever queried is
V−1 during block V (`ProcessVoteExtensions`, `vote_processor.go:26`), and
promotion for records due at V happens *after* that read in the same block. The
loss is always strictly behind every reader. The consume-before-promote ordering
is therefore load-bearing, not incidental, and is tested as such.

**Warm-up.** Every record is committed and queryable at least one full height
before the first `ExtendVote` that must honor it — the same notice validators
get today, now per record.

## Wire, query, and sidecar

`proto/ark/abci/vote_extension.proto` is **unchanged**. `target_version` now
means "version of `fold(V)`" and remains exact-match validated
(`abci/oracle/oracle_votes.go:80`). No tolerance window is needed: voter and
tally agree by construction. Aggregation, scoring, and rate application are
untouched.

The targets query response replaces the single `pending` field with the repeated
transitions field (`proto/ark/asset/v1/query.proto:97`;
`QueryVoteTargetsResponse` in `proto/ark/oracle/v1/query.proto:142` is deleted
with the rest of the oracle target machinery).

The sidecar's warm-up union (`oracle/sidecar/chainstate/polling.go:120-125`)
changes from "active ∪ pending.denoms" to "active ∪ denoms of ADD records" — a
few lines, preserving the whole advance-notice mechanism. The sidecar remains
wall-clock polled and version-blind; no height is threaded through it.

Two pre-existing properties that consecutive boundaries stress without breaking,
recorded so implementers do not mistake them for regressions:

- A denom-set change forces a provider transport cycle
  (`oracle/sidecar/runtime/targets.go:51-56`). Back-to-back changes mean
  back-to-back cycles. `setMarkets` (`providers/base/update.go:19-34`) retains
  cached prices for unchanged pair/ticker mappings, so unaffected denoms keep
  their prices across a restart.
- `MaxPriceAge` filtering (`oracle/sidecar/runtime/prices.go:79-90`) can age out
  retained pairs during a reconnect gap.
- Change detection is by denom-set equality
  (`oracle/sidecar/runtime/targets.go:25`), so an add and a remove that net to
  the prior set are invisible to the sidecar even though the chain bumped the
  version twice. Harmless: the sidecar prices a union, and version agreement is
  a chain-side property.

## Accounting

Untouched by this design. Participation stays a fleet-wide OR over targets
(`abci/oracle/aggregation.go:82-93`), and `functioningBlock` keeps its
half-power threshold. A validator omitting a price for a newly added denom keeps
`ValidReport` and stays `participated` as long as any other target is priced.

## Cost profile

- **Reads** (`ExtendVote`, tally, targets query): one Item read plus an
  in-memory fold over a near-empty record list, once per height. Today: one Item
  read plus a height comparison. Same store I/O.
- **`Phase(denom)`** in lifecycle transactions: decode the Item, binary-search
  active membership, scan the record list. Today: decode a *larger* Item
  (carrying a duplicate denom set) and do two binary searches.
- **State size**: slightly smaller — deltas replace a duplicated snapshot.
- **Promotion**: same shape as today, per record instead of per epoch.

The real cost of this design is review and test surface, not runtime: "at most
one boundary in flight" stops being a theorem, so consumers must be verified
against consecutive-height changes.

## Genesis and migration

`x/asset` genesis carries the transitions list, validated by the same
`Validate()`. Importing mid-transition is legal; the `<=` comparison in
promotion absorbs any overdue records on the first block after import.

`x/oracle` genesis drops its target fields. Its "exchange rate denoms ⊆ targets"
check (`x/oracle/types/genesis.go:147-151`) moves to asset's genesis validation
via the expected-keeper boundary, and its "params denoms == active or pending"
check (`genesis.go:157-163`) is deleted along with params-driven membership.
Module init order must place asset after oracle; this is an explicit wiring
requirement in `app/`.

**Migration assumption: genesis-only.** This design assumes a pre-launch chain
with no live state to upgrade, so no upgrade handler is written. If that is
wrong, a state migration converting `Pending` into a single ADD/REMOVE record
set is required and must be specced before implementation.

## Documentation updates

`docs/ASSET_MODULE_PLAN.md:645-656` lists the invariants `x/asset` must preserve
when it absorbs the target scheduler. This design keeps four of them unchanged —
vote-height-based selection (649), old-epoch aggregation before promotion (653),
explicit target versioning (655), and empty target sets as valid protocol state
(656) — and amends three:

- **Line 650, "the two-height activation boundary"**: survives, now per record
  rather than per epoch.
- **Line 651, "one immutable pending target set at a time"**: replaced. The
  immutability half is retained per record and is load-bearing for the
  correctness argument; the one-at-a-time half is what this design removes.
- **Line 652, "complete sorted target snapshots rather than deltas"**: replaced.
  Records are deltas by construction. The sortedness and completeness properties
  move to the materialized active set and the fold's output.

Also needing updates: the "Derived Oracle state" table (`ASSET_MODULE_PLAN.md`
lines 314-323), which frames transitions as current-versus-pending *set*
membership and must be reframed per denom record; and the target-epoch
descriptions at lines 193 and 520. `abci/preblock/README.md`,
`abci/voteextension/README.md`, and `abci/oracle/README.md` describe the
single-pending model and need the same pass.

## Testing

**Types** (`x/asset/types/`, plain table-driven functions):

- Fold: no records; one record before, at, and after its activation height; two
  records at the same height (one batch, one version bump); two records at
  consecutive heights (two batches, two bumps); add and remove of different
  denoms in one batch.
- Version arithmetic across multi-batch folds.
- `Validate` mutate-pattern, one field per case, covering each invariant:
  duplicate denom records, ADD of an active denom, REMOVE of an absent denom,
  unsorted records, non-positive activation height, cap exceeded including the
  fold-at-future-height case.
- `Phase` for each of the four values, including a denom with a record versus a
  denom without one while other records are in flight.

**Keeper** (`x/asset/keeper/`, suite-based):

- Schedule idempotency (same direction twice) and opposite-direction conflict.
- Two different denoms scheduled in the same block: both succeed, one batch.
- Emergency multi-suspend: two `EmergencySuspendAsset` calls in one block both
  commit, neither reverts.
- Bulk activation: N assets activated in one block, all N in one batch.
- Promotion with status completions, retirement residuals, and rate pruning.
- Promotion of two batches due at the same block (records scheduled in
  consecutive earlier blocks) advancing the version twice.

**ABCI** (`abci/preblock/`):

- `target_transition_test.go` keeps its existing single-boundary case
  (`TestVoteTargetTransitionAcrossVoteAndFinaliseHeights`) with the fold
  substituted for the pending epoch.
- New consecutive-boundary case: records scheduled at H and H+1; at every vote
  height the extension's version matches what the tally expects; attendance is
  unaffected; the version advances twice.
- New ordering test: pinning that `ProcessVoteExtensions` observes the
  pre-promotion fold for vote height V in the block where records due at V are
  promoted. This is the test that fails if someone reorders preblock.
- The aggregation recording keeper (`abci/oracle/aggregation_test.go:810`)
  hardcodes one version; extend it to serve a version sequence and add cases
  covering a version change between consecutive tallies.

**Genesis**: roundtrip export/import with records in flight, and import with an
overdue record.

## Deferred work

Per-target first-aggregation grace in vote accounting. The audit confirms the
rule at `docs/ASSET_MODULE_PLAN.md:519` is unimplemented: the split-fleet case
is pinned as *current* behavior by
`TestAggregateOracleVotesRecordsSoleTargetAbstentionAsEligibleOnly`
(`abci/oracle/aggregation_test.go:173`), where a validator that abstains on the
sole target accrues an eligible-not-attended block. This design does not create
that gap, but by making bulk additions easy it makes the trigger condition more
reachable. The per-denom activation heights and events introduced here are the
hooks a grace implementation needs. It gets its own spec.
