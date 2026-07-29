# Pricefeed Decoupling: Oracle-Owned Feeds, Asset-Owned Price Sources

Date: 2026-07-29
Status: approved design, pending implementation plan

## Problem

Every priced observation in the current design is a registered Ark-native
asset denomination. Oracle target membership is derived from asset lifecycle
transitions, targets are validated as native base denoms, and the rate store
is keyed by denom. `docs/ASSET_MODULE_PLAN.md` deferred generalized feed
identifiers "until the protocol needs non-asset observations or multiple
independent prices for one asset."

That trigger has fired, on both clauses:

1. **Basket-indexed flagship currency.** The roadmap includes a spendable
   SDR-like currency whose peg is a basket of component units. Basket weights
   are monetary policy and must be on-chain governance state. Under the
   current design the only way to price such an asset is composite reporting:
   every validator's sidecar holds the weights in its resolver config and
   reports one blended rate. A weight change then becomes a fleet
   coordination event — votes split between old-weight and new-weight rates
   during any rollout that is not perfectly synchronized. On-chain
   composition requires component prices (`usd`, `eur`, `xau`, ...) that are
   observations, not assets.
2. **Strategic reserve diversification.** The Treasury reserve will acquire
   non-NOAH holdings through committee-gated acquire/dispose operations with
   on-chain accounting. Marking those positions to market — planned as a
   follow-on, see "Follow-on specs" — needs prices for holdings such as gold
   or bitcoin that will never be lifecycle-managed Ark assets.
3. **Prices with more than one consumer.** Once a basket component, a reserve
   position, and an asset can share one price, tying the price's existence to
   a single asset's lifecycle is wrong by construction: suspending or
   retiring the asset must not tear a feed out from under its other
   consumers.

This reverses a documented decision. The asset plan coupled target membership
to asset lifecycle because, at the time, every consumer of a price was an
asset operation, and lifecycle-derived membership eliminated a drift class (a
priced thing with no lifecycle, an active asset with no price) by
construction. The reversal is triggered by the appearance of non-asset price
consumers, and it is executed now — before `x/asset` activation — because the
re-keying lands almost entirely in the dormant module and in `x/oracle`
internals. Activating first and decoupling later would pay the same redesign
plus a live-state migration.

The mechanism built by
`docs/superpowers/specs/2026-07-28-oracle-target-transitions-design.md` —
per-ID dated transition records, the height fold, batched promotion, and the
two-height correctness argument — is preserved in full. This design re-homes
it from `x/asset` to `x/oracle` and re-keys it from asset denoms to feed IDs.

## Non-goals

- Basket definitions, on-chain composition, or derived rates. That is
  follow-on spec #2; this design only reserves the proto slot.
- Reserve positions, `MsgAcquireAsset`/`MsgDisposeAsset`, or valuation of
  reserve holdings. That is follow-on spec #3.
- Market or Treasury consumer changes. The Tobin-tax move to `x/market`
  remains Phase 4 work, unchanged.
- `x/asset` activation. The module stays dormant; it slims down in place.
- Vote-extension wire changes. The wire is untouched.
- Attendance or vote-accounting changes. Grading semantics carry over
  unchanged, keyed by feed ID.

## Layering

    x/oracle   observations: feed registry, reporting, aggregation, rates,
               freshness, pruning
    x/asset    economics: lifecycle, settlement, resolution, reference,
               and each asset's declared price source
    consumers  Market, Treasury, later baskets and the reserve, reading
               through lifecycle-aware views

`x/oracle` does not depend on `x/asset`. The reverse dependency (asset reads
feed state and rates) already exists and is unchanged.

## Feed registry

`x/oracle` replaces `VoteTargets` (`x/oracle/keeper/keeper.go:43`, message at
`proto/ark/oracle/v1/oracle.proto:81`) with one item of the ported mechanism:

```protobuf
// Feeds defines the materialized active feed set and any scheduled
// per-feed transitions not yet activated.
message Feeds {
  repeated string feed_ids = 1;             // sorted, unique
  uint64 version = 2;                       // bumps once per activation batch
  repeated FeedTransition transitions = 3;  // sorted by (height, feed_id)
}

// FeedTransition is one scheduled membership change for one feed.
// Immutable once written.
message FeedTransition {
  string feed_id = 1;
  FeedDirection direction = 2;
  int64 activation_vote_height = 3;
}

enum FeedDirection {
  FEED_DIRECTION_UNSPECIFIED = 0;
  FEED_DIRECTION_ADD = 1;
  FEED_DIRECTION_REMOVE = 2;
}
```

`PendingVoteTargets` is deleted. The fold (`AtHeight`), per-ID `Phase`, the
scheduling decision table (same-direction idempotent, opposite-direction
rejected, cap checked against the fold at future heights), batched promotion
with one version bump per activation height, and the immutable-record
plus-two-heights correctness argument are the 2026-07-28 design verbatim,
re-keyed. That spec's correctness section remains the authority; this design
adds no new consensus timing.

`MaxVoteTargets` (256) and `VoteTargetActivationDelayBlocks` (2) carry over
as `MaxFeeds` and `FeedActivationDelayBlocks` with unchanged values.

### Feed identity

A feed ID is an opaque lowercase identifier matching `^[a-z][a-z0-9]{1,15}$`.
`noah` and `anoah` are reserved: the numeraire has no feed. There is
deliberately **no** rule that a feed ID must not look like a denomination,
because no consensus path mixes the two keyspaces: feeds live in the oracle
registry, denoms in Bank and the asset registry, and the only bridge is an
asset's explicit `price_source` reference.

### Rate contract

Unchanged in substance, re-keyed:

    rate[feed_id] = units of the feed's quote unit per one NOAH

All Ark-native assets use exponent 18, so a rate under the old reading
("base-denom units per anoah") and the new reading ("display units per
NOAH") is the same number. Genesis therefore seeds the registry with feeds
named exactly like today's targets (`ausd`, `aeur`, ...), and **the rate
store keys do not change**. Live Market conversion, Treasury liability
valuation, and the tax-cap path are untouched by this milestone. Future
non-asset feeds use clean unit symbols (`usd`, `xau`, `btc`); the mixed
naming is cosmetic because IDs are opaque end to end. The supported
`LegacyDec` precision band and fail-closed arithmetic rules from the asset
plan apply to feed rates unchanged.

### Messages

Two governance messages replace params-driven membership:

- `MsgAddFeed{authority, feed_id}` schedules an ADD transition.
- `MsgRemoveFeed{authority, feed_id}` schedules a REMOVE transition.

Both follow the ported scheduling table and emit
`EventFeedTransitionScheduled{feed_id, direction, activation_vote_height}`.
Batch promotion emits `EventFeedsActivated{version, added, removed}`.

`MsgUpdateParams` loses its membership branch: `VoteTargetDenoms`, the
`ScheduleVoteTargets` call, and the `ErrVoteTargetRemoval` guard
(`x/oracle/keeper/msg_server.go`) are deleted. Tobin taxes remain oracle
params until Phase 4; two coherence rules bind them to the registry
meanwhile:

1. **Removal containment.** `MsgRemoveFeed` is rejected while any live
   Tobin-tax entry names the feed. This preserves the Phase-0 containment —
   removing a feed the live converter prices through was the confirmed
   defect — scoped per feed instead of banning all removals.
2. **Addition coherence.** A params update may not introduce a Tobin entry
   whose denom is not a feed in phase Active or Adding — a feed with an
   in-flight REMOVE does not qualify, or a referent could be created inside
   the two-height removal window. Without this rule, governance could create
   a swap-enabled-but-unpriced denom, the mirror image of the removal
   defect. Metadata auto-registration for new Tobin denoms stays where it
   is.

Because every referent-creating path excludes feeds in phase Removing (this
rule now; the asset-side reference validation below at activation), guards
run at schedule time only and promotion needs no re-validation: no referent
can appear between a REMOVE being scheduled and its activation.

### Referent guards

Feed removal is validated against **referent guards**: at `MsgRemoveFeed`
execution, each registered consumer is asked whether it currently references
the feed, and any yes rejects the removal. A guard answers by reading its own
authoritative state at call time. Nothing is indexed and nothing is stored;
there is no write path on consumer transitions and nothing that can drift.

Relationship to the deleted `AssetLocks` index
(`docs/superpowers/specs/2026-07-29-asset-consolidation-design.md`): the
purpose is the same — a mutation must not strand a dependent — but the
mechanism is the one that replaced the locks, not the locks back. AssetLocks
was maintained back-pointer state, and it dissolved because the memberships
it indexed were derivable from registry state by construction. Feed
referents cannot dissolve the same way: decoupling deliberately breaks the
identity between feed membership and feed consumers (a feed may have zero
consumers, or several), so the dependents are genuinely foreign state — and
they are therefore *derived at validation time* from each consumer's own
records, never mirrored into an oracle-side index. The rejected alternative,
a `FeedReferents` KeySet written by consumers, would resurrect both lock
failure modes: bookkeeping writes on every consumer transition, and an index
that can disagree with the `price_source` fields it summarizes.

Guard registration is hooks-style at app wiring (the `SetReferenceConsumers`
precedent), because `x/asset` already depends on `x/oracle` and the reverse
edge must be injected. Rollout:

- This milestone: the Tobin-tax guard only, which is oracle-internal (its
  authoritative state is oracle params), so no cross-module interface is
  exercised yet. The guard interface is defined and tested against this
  implementation.
- `x/asset` activation: the asset guard replaces the Tobin guard's duty —
  a feed is pinned while any asset in a status that requires pricing now or
  imminently references it: `ACTIVE`, `ISSUANCE_HALTED`, `PENDING` with
  `completion_requested`, or `SUSPENDED` with `completion_requested`.
  Plain `SUSPENDED`, `WRITTEN_OFF`, and `RETIRED` referents do not pin the
  feed — today's design already removes targets on suspension, so feed-off
  during suspension is the established semantic, and recovery already
  tolerates re-add plus re-warm.
- Later: basket definitions (spec #2) and reserve positions (spec #3)
  register guards over their own state.

Guards prevent an operational outage (removing a consumed feed forces a
re-add, two-height activation, and freshness re-warm — for the reference
denomination's feed that halts conversion chain-wide until warm). They are
not the last line of defense: every consumer fails closed on a missing rate,
unchanged.

### Queries

`Query/Feeds` (module-query-safe) replaces `Query/VoteTargets`, returning the
active set and the transitions list. The pending-epoch response shape is
deleted.

## Asset side

`x/asset` stays dormant and slims down.

### Price source

`Asset.oracle_required` is replaced:

```protobuf
message Asset {
  string denom = 1;
  cosmos.bank.v1beta1.Metadata metadata = 2;
  AssetStatus status = 3;
  uint64 version = 4;
  oneof price_source {
    string feed_id = 5;     // priced by one oracle feed
    // field 6 reserved for basket_id: priced by a derived basket rate
    // (follow-on spec #2)
  }                          // unset: unpriced asset
  bool completion_requested = 7;
}
```

Several assets may reference one feed. A successor asset after a
residual-supply tombstone reuses the predecessor's feed under a new denom
with no re-warm. `RegisterAsset` and `AmendRegistration` validate that a
referenced feed is in phase Active or Adding (never Removing, closing the
in-flight-removal window); `AmendRegistration` may change
the price source only while `PENDING` with `completion_requested` false,
replacing the old "absent from active and pending targets" rule. Metadata
mutability rules are unchanged.

### Completion intent

`completion_requested` is the explicit replacement for intent that target
phase used to encode. Decoupled, a feed may already be live and fresh when an
asset registers against it (a second asset sharing `usd`) or throughout a
suspension (the feed never stopped). Without explicit intent, preblock
completion would activate a merely-registered `PENDING` asset, or recover any
suspended asset with a fresh feed, the moment conditions held.

Semantics by status:

| Status      | `completion_requested` means          | Set by          | Cleared by                          |
| ----------- | ------------------------------------- | --------------- | ----------------------------------- |
| `PENDING`   | activation in progress                | `ActivateAsset` | completion into `ACTIVE`            |
| `SUSPENDED` | recovery in progress                  | `BeginRecovery` | `CancelRecovery`, completion into `ISSUANCE_HALTED` |
| any other   | must be false (genesis and invariant) | —               | —                                   |

Every transition out of a carrying status also clears the flag, so the
placement invariant holds by construction: `FinalizeRetirement` from
`PENDING` and `WriteOffAsset` or zero-supply `FinalizeRetirement` from a
recovering `SUSPENDED` asset all clear it as part of the move. Messages that
set or clear the flag are governance mutations and advance the asset
version; automatic completions still do not.

### Lifecycle simplification

The seven entry points lose their scheduling steps. Status preconditions,
settlement rules, resolution records, reference rules, and the emergency
mandate are otherwise unchanged.

- `ActivateAsset`: requires the referenced feed in phase Active or Adding,
  so `[MsgAddFeed, MsgActivateAsset]` composes in one proposal. Sets
  `completion_requested`; the asset stays `PENDING` until its first fresh
  rate.
- `SuspendAsset` / `EmergencySuspendAsset`: pure status moves plus the
  existing reference-fallback promotion. The feed keeps running; status
  gating in `PricedAssetView` is the containment, as it always really was.
- `FinalizeRetirement`: every path is now an immediate single transition to
  `RETIRED`. The residual bound is checked, and any `RETIREMENT_RESIDUAL`
  record appended, at that moment. The "target already absent" preconditions
  and the linger-until-removal-activates choreography are deleted.
- `BeginRecovery` / `CancelRecovery`: set and clear `completion_requested`.
  The rule "require a pending target addition to activate before scheduling
  its removal" is deleted; it existed only because opposite-direction
  transitions could not coexist in flight.
- Recovery completion: requires `SUSPENDED`, `completion_requested`, no
  settlement plan, a fresh rate on the referenced feed, and the Market and
  Treasury prerequisites — then `ISSUANCE_HALTED`, clearing the flag.
- `PENDING` completion: requires `completion_requested`, a fresh positive
  rate on the referenced feed, and required policy — then `ACTIVE`, clearing
  the flag.

### Derived membership

`PricedLiveDenoms` becomes: `price_source` set and status in
{`ACTIVE`, `ISSUANCE_HALTED`}. Feed freshness stays a use-time concern
surfaced through `RateSet` and `PricedAssetView`, never a membership
concern, so membership cannot flap with feed health. Reference eligibility
(consolidation design) re-reads as: `price_source` configured, live status,
referenced feed active. `PricedAssetView` resolves asset → `price_source` →
`RateSet[feed_id]`; with denom-named genesis feeds the resolved keys equal
today's, so behavior is identical and the indirection is structural.

### Deleted from x/asset

- `OracleTargets` item (`x/asset/keeper/keeper.go:30`), its proto messages,
  types, fold/phase/validation code, and keeper scheduling/promotion — all
  re-homed to `x/oracle` as the feed registry.
- The derived-oracle-state phase table and the status/oracle-state
  compatibility matrix in the plan.
- `GetOracleTargets`/`AdvanceOracleTargets` on the ABCI asset interface. The
  activation-time interface shrinks to a completions hook.
- Every status↔target genesis validation rule, replaced by:
  `completion_requested` placement (table above), and referenced-feed
  existence checked against oracle genesis through the expected keeper
  (oracle initializes before asset; already a wiring requirement).

## ABCI and preblock

`arkabcitypes.OracleKeeper` (`abci/types/interfaces.go:18`) keeps its shape:
`GetVoteTargets(ctx, voteHeight)` becomes `GetFeeds(ctx, voteHeight)`
returning the fold `{version, feed_ids}`, and `AdvanceVoteTargets` becomes
`AdvanceFeeds`, still pruning rates internally — the cross-module
removed-denoms return channel the asset-owned design needed is not built at
all.

The consume-before-promote ordering in `abci/preblock/preblock.go:88-99`
(`ProcessVoteExtensions` for vote height V reads the pre-promotion fold;
records due at V are absorbed after) is promoted to a documented consensus
invariant with a pinning test **in this milestone**, per the 2026-07-28
correctness argument's promotion-lag clause.

Unchanged: the vote-extension wire (`target_version` means the fold's
version, exact-match validated), aggregation, dispersion scoring, rate
application, fleet-wide-OR participation, the functioning-block threshold,
and windowed attendance settlement — now graded over feed IDs.

The planned preblock steps "advance asset target state, then prune returned
denoms" collapse into `AdvanceFeeds`. The lifecycle-completions step remains
specced for activation behind the shrunken asset interface.

## Sidecar

The sidecar keeps querying `x/oracle`; the planned client-type switch to
asset queries is cancelled. The response reshapes from active-plus-pending to
active-plus-transitions, and the warm-up union
(`oracle/sidecar/chainstate/polling.go`) becomes "active ∪ ADD-direction
transitions" — the same few-line change the 2026-07-28 design described. Feed
IDs are opaque to the sidecar: its provider-route config resolves them
exactly as it resolves denoms today, and the sidecar remains wall-clock
polled and version-blind.

## Genesis and cutover

Prelaunch chain, clean genesis break, no migration handler.

- Oracle genesis: `VoteTargets`/`Pending` fields are replaced by
  `Feeds{feed_ids, version, transitions}`, seeded with the eight denom-named
  feeds and the carried-over version. Importing mid-transition is legal; the
  `<=` comparison absorbs overdue records on the first block.
- Oracle genesis validation: exchange-rate keys ⊆ feed IDs; Tobin denoms ⊆
  active-or-adding feed IDs (coherence rule 2); the ported fold/transition
  invariants.
- Asset genesis (dormant): drops all target state and rules; adds
  `completion_requested` placement and referenced-feed existence.
- Module init order: oracle before asset, already required.

Blast radius statement: this milestone does not touch Market, Treasury, rate
store keys, the vote wire, attendance semantics, Tobin tax values, or any
asset lifecycle rule not named above.

## Testing

Ported from the 2026-07-28 plan, landing live in this milestone:

- Types (oracle): fold tables (none, before/at/after activation, same-height
  batch, consecutive-height batches, mixed add/remove), version arithmetic,
  `Validate` mutate-pattern per invariant, `Phase` per value.
- Keeper (oracle): schedule idempotency and opposite-direction conflict; two
  feeds in one block, one batch; N-feed bulk add; promotion with pruning;
  two batches due in one block advancing the version twice.
- ABCI: the single-boundary transition case re-pointed at the fold; the new
  consecutive-boundary case; the ordering pin that fails if preblock is
  reordered; aggregation across a version sequence.
- Genesis: roundtrip with transitions in flight; import with an overdue
  record.

New in this design:

- Referent guards, both directions: a Tobin entry blocks `MsgRemoveFeed`;
  removal succeeds after the entry is gone; a params update introducing a
  Tobin entry without an active-or-adding feed is rejected.
- Completion intent gating: a `PENDING` asset referencing an already-live
  shared feed does not activate without `ActivateAsset`; a `SUSPENDED` asset
  with a fresh feed does not recover without `BeginRecovery`.
- Asset entry points: retirement paths are immediate; suspension schedules
  nothing; `AmendRegistration` price-source rules.
- Attendance preservation across the cutover: fleet-wide-OR participation,
  the functioning-block threshold, and windowed settlement behave
  identically, per the Phase-3 verification requirement, applied to this
  cutover instead.

## Documentation updates

- `docs/ASSET_MODULE_PLAN.md`: ownership model (oracle owns feeds and their
  epochs; asset owns `price_source`), core decisions (the deferral of
  generalized feed identifiers is replaced by this design; record the
  reversal), price contract (rates keyed by feed ID), state model, lifecycle
  operations, the derived-oracle-state section (deleted), target-epoch
  section (re-homed), sidecar mapping, genesis, application wiring, and
  phases: Phase 3 shrinks to asset wiring, completions, and Bank-metadata
  ownership — the target-ownership move no longer exists.
- `docs/superpowers/specs/2026-07-28-oracle-target-transitions-design.md`:
  superseded-by note stating the mechanism survives verbatim, re-homed to
  `x/oracle` and re-keyed by feed ID by this design.
- `docs/superpowers/specs/2026-07-29-asset-consolidation-design.md`:
  reference-eligibility wording (feed-active instead of target-phase).
- `abci/preblock/README.md`, `abci/voteextension/README.md`,
  `abci/oracle/README.md`: the single-pending-epoch description passes.

## Follow-on specs

This spine guarantees its two dependents:

1. **Baskets (spec #2):** component prices are feeds; basket definitions and
   deterministic post-aggregation composition are oracle-domain; the flagship
   currency is an ordinary `x/asset` asset whose `price_source` is the
   reserved `basket_id` arm. Weights change by governance parameter, atomic
   at a height, with zero validator coordination.
2. **Reserve positions (spec #3):** `{asset_id → feed_id, quantity,
   cost_basis}` positions with committee-gated acquire/dispose; cost-basis
   accounting works with no oracle dependency, and mark-to-market becomes a
   valuation-function swap over the same state, using feeds plus the
   established conservative incomplete-valuation branch.
