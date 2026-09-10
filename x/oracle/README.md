# On-chain oracle

Oracle owns the feed registry, consensus exchange rates, protocol reference denomination, validator accounting, and reward settlement. The ABCI pipeline aggregates reports and calls this keeper; the off-chain pricefeed is a separate subsystem. Feed registration creates an observation, not an asset liability.


[Attendance settlement](#11-attendance-settlement) · [Feed registry](#12-feed-registry) · [Protocol reference](#13-protocol-reference-denom) · [Rate orientation](#14-rate-orientation-noah-per-unit) · [Development](#development)

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/feeds.go](keeper/feeds.go) | Height-addressable feed sets and scheduled promotion. |
| [keeper/feed_guards.go](keeper/feed_guards.go) | Consumer referents checked before feed removal. |
| [keeper/reference_denom.go](keeper/reference_denom.go) | Atomic reference change and consumer rebases. |
| [keeper/accounting.go](keeper/accounting.go) | Operator-keyed attendance/reward accounting and settlement. |
| [keeper/abci.go](keeper/abci.go) | Window settlement. |
| [keeper/conversion.go](keeper/conversion.go) | Rate access and conversion views. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`ExchangeRate` is keyed by denomination; `RewardWeight` and `Attendance` are keyed by validator operator address. Consensus addresses are resolved at the reporting boundary. `Feeds`, `ReferenceDenom`, `Accounting`, and `Params` are items. App wiring injects foreign feed guards and Market/Treasury reference consumers after construction to avoid dependency cycles.

## 1. Oracle state and lifecycle

### 1.1 Attendance settlement

[ABCI oracle processing](../../abci/oracle/README.md#participation-and-functioning-blocks) defines report participation
and the functioning-block gate. The keeper persists the resulting eligible/attended counts independently of reward
weights; it never reconstructs participation from stored prices.

Attendance drives only the oracle module's periodic settlement, which jails — never slashes — validators whose attended
share of eligible blocks falls below `min_attendance_per_window`. Accuracy and coverage incentives come entirely from
band-gated rewards, not from this counter.

Every record is judged at settlement, however few eligible blocks it holds. `min_attendance_per_window` is the whole
grace: a ratio is scale-free, so a validator present for part of a window is held to the same share of the blocks it was
actually present for, and an eligible-block floor on top would silently soften the ratio governance set. Correlated
outages need no such floor, because non-functioning blocks never reach the record in the first place — which means a
sparse record is evidence the validator was absent while the fleet worked, not evidence that grading is unsafe.

Newly activated targets get no special grading: attendance is unconditional per window, by decision. Rollout slack comes
from layers that already exist — the sidecar prices scheduled targets throughout their pending window, pricing the
surviving targets keeps a validator above the participation floor unless one activation batch more than doubles the
target set (the worst case at the 50% threshold cap; the 20% default tolerates a 5x expansion), a majority unable to
participate grades nobody, and the windowed ratio leaves a lagging
operator most of a window to ship provider support. The deliberately accepted residual is a validator pricing nothing
for the better part of a window while a majority prices: it is jailed at settlement, which also restores quorum by
shrinking total power toward the capable share.

Settlement is implemented by `x/oracle/keeper/accounting.go` and the window driver in `keeper/abci.go`.
`Attendance` is keyed by operator address. Unbonded and already-jailed validators are skipped; counters are cleared after
settlement. An attendance-window change takes effect after the active window settles, while the minimum ratio is read
live. `EventOracleJail` records the validator, counts, and accounting window. Zero minimum attendance disables jailing.

### 1.2 Feed registry

The implementation is in `types/feeds.go`, `keeper/feeds.go`, and `keeper/feed_guards.go`.

**Why feeds are not asset lifecycle.** Every priced observation was once a registered asset denomination:
membership derived from lifecycle transitions, targets validated as native denoms. The trigger for
generalising fired on both clauses the module plan had deferred it under: a basket-indexed flagship currency
needs component prices that are observations, not assets, and composite reporting in the sidecar makes
every weight change a fleet coordination event; reserve holdings such as gold need prices and will never be
lifecycle-managed assets; and once a basket component, a reserve position, and an asset can share one
price, tying its existence to one asset's lifecycle is wrong by construction. The layering:

    x/oracle   observations: feed registry, reporting, aggregation, rates, freshness, pruning, and the
               protocol reference (it names a feed, and every rule it has is a feed rule)
    x/asset    economics: lifecycle, settlement, resolution
    consumers  Market, Treasury, the Reserve, later baskets, reading through lifecycle-aware views

`x/oracle` does not depend on `x/asset`.

**Why per-denom records.** Membership was scheduled through a single chain-global pending epoch, so a
second denom's request could never equal the staged set: one single-denom change per two blocks. Bulk
proposals failed at the second message; an emergency multi-suspend reverted whole because scheduling was
embedded in the suspension; two proposals executing in the same window failed the second permanently.
Global mutual exclusion was stronger than consensus safety requires. Safety needs only that a scheduled
change is **immutable once written** and **activates at schedule height + 2**; mutual exclusion was surplus
strictness and the sole source of all three problems.

**State.** One item, `Feeds{denoms, version, transitions}`: a materialised active set (sorted, unique), a
version that bumps once per activation batch, and immutable records `{denom, direction,
activation_vote_height}`, at most one per denom, sorted by height then denom. Records are short-lived
(written at H, absorbed at H+2, then deleted), so the in-flight set is normally empty. Membership is decided
once, at write time; reads touch only the materialised set plus the small record list. `Validate`: version
positive; active denoms valid feed keys, never the numeraire; ADD requires absent, REMOVE requires present;
positive activation heights; `len(denoms) + #ADD ≤ MaxFeeds` (256) so the cap holds at every future height.

**A feed is keyed by the denomination it prices.** The design as first approved used opaque feed IDs with
an explicit `price_source` on each asset; on 2026-07-30 that indirection was dropped. Feed keys and native
denominations share one pattern (`^a[a-z0-9]{2,15}$`), the numeraire is excluded because NOAH has no feed,
and the rate store, the feed set, and the registry all key by denom. A feed with no asset is a
denomination-shaped key nobody has registered. External symbols ([Reserve external symbols](../reserve/README.md#external-symbols-and-freshness)) later made the feed's meaning the
*series*: `ausd` is "the USD series", consumed by the asset because it tracks USD and by a reserve holding
because it is USD held elsewhere.

**Height-addressable reads.** `AtHeight(V)` applies every record with activation ≤ V in sorted order,
bumping the version once per distinct activation height (a batch). `Phase(denom)` is Off, Adding, Active,
or Removing from active membership plus that denom's own record, truthful per denom.

**Scheduling.** Same-direction record in flight: no-op (governance retries are safe). Opposite direction in
flight: `ErrFeedTransitionPending`, scoped to this denom, because amending a record would break the
height-addressable read. ADD of active or REMOVE of absent: no-op. Cap exceeded: error. Otherwise append
`{denom, direction, blockHeight + FeedActivationDelayBlocks}` and emit `EventFeedTransitionScheduled` with
the resulting version. Removals are never cap-blocked. N schedules in one block share one activation height:
one batch, one version bump.

**Promotion.** `AdvanceFeeds` runs in the preblocker after vote extensions are processed, drains every due
batch in height order (set delta, one version bump, records deleted, removed feeds' rates pruned, one
`EventFeedsActivated{version, added, removed}` per batch). Rate pruning is per record: a denom removed in
one batch and added in a later one loses its rate and re-warms, which is correct since a rate that survived
removal would be stale on return. The asset-domain work the `x/asset` version did at promotion (status
re-validation, retirement completions, residual records) did not port; promotion is pure set arithmetic.

**Correctness argument** (the authority for the timing rules). `ExtendVote` for vote height V runs against
committed state V−1; the tally for V runs in block V+1 against state V. If a transaction could change the
set with immediate effect, the tally would validate votes against a set the voter could not have seen. Two
invariants close the gap: a record is immutable once written, and `activation_vote_height = schedule height
+ 2`. A record affects `fold(V)` only if its activation ≤ V, so it was written in a block ≤ V−2; the voter's
state V−1 and the tally's state V both contain everything committed through V−2, and immutability makes the
contents identical, so both compute the same set and version. Records written in blocks V−1 or V exist in
one party's state and not the other's, but activate at ≥ V+1, so `fold(V)` excludes them in both. The
argument is per record and holds for any number in flight. **Promotion lag**: folding is lossy in one
direction, since once a batch is absorbed `AtHeight` for an earlier height returns the newer set; this is
safe because the only historical height ever queried is V−1 during block V, and promotion for records due
at V happens after that read. Consume-before-promote is load-bearing and a preblock test pins it. Every
record is queryable at least one full height before the first `ExtendVote` that must honour it.

**Messages.** Governance `MsgAddFeed{authority, denom}` and `MsgRemoveFeed{authority, denom}`;
`MsgUpdateParams` carries no membership branch and is authority, `Validate`, `Set`. `MsgRemoveFeed` for a
denom in phase Off fails with `ErrFeedNotFound` rather than no-opping: a proposal naming a typo or an
already-removed feed passing its vote and silently doing nothing is the worse outcome; re-submitting while a
removal is in flight targets a feed in phase Removing, which exists, so idempotency is unaffected.

**Referent guards.** Feed removal is validated against consumers: at `MsgRemoveFeed` execution each
registered guard is asked what claims it holds on the denom, and any claim rejects the removal. A guard
answers by reading its own authoritative state at call time; nothing is indexed, nothing is stored, nothing
can drift. The purpose is the same as the deleted `AssetLocks` (a mutation must not strand a dependent), but
the mechanism is the one that replaced the locks: decoupling deliberately breaks the identity between feed
membership and feed consumers (a feed may have zero consumers, or several), so dependents are genuinely
foreign state, derived at validation time from each consumer's own records rather than mirrored into an
oracle-side index. A consumer-written KeySet was rejected because it would resurrect both lock failure
modes. Registration is hooks-style at app wiring (`SetFeedReferentGuards(asset, reserve)`), since
`x/asset` already depends on `x/oracle` and the reverse edge must be injected; the set is wiring-owned and
holds exactly the foreign consumers that exist, with the oracle checking its own reference denom first.
Since 2026-07-31 a guard returns every claim it holds as `FeedReferents(ctx, denom) ([]FeedReferent{consumer,
referent}, error)`, so governance sees every blocker in one error rather than one per proposal cycle; the
oracle owns attribution because it aggregates, the consumer owns the description because it owns the
predicate, and a claim-kind enum was rejected because it would have the oracle registering the claim types
consumers may hold. `Query/FeedReferents` answers from the same collector, so what an author inspects is
what governance is judged against, and it gives the wiring-owned guard set an integration-test surface. It
is not `module_query_safe` (referent descriptions are consumer-owned prose) and returns `NotFound` for a
feed in phase Off, because "nothing pins it" for a typo'd denom reads as "safe to remove". Guards prevent an
operational outage (a re-add, two-height activation, and re-warm; for the reference feed a chain-wide
conversion halt until warm); every consumer still fails closed on a missing rate.

**Wire and genesis.** The vote-extension wire is unchanged: `target_version` means the fold's version
and stays exact-match validated, with no tolerance window since voter and tally agree by construction.
The [sidecar chain-state adapter](../../pricefeed/sidecar/chainstate/README.md) owns warming and runtime updates.
Genesis carries the transitions list under the same `Validate`, validates
exchange-rate keys ⊆ feeds, and absorbs overdue records on the first block. `DefaultFeedDenoms` is the
launch feed list. Module init order is oracle before asset.

**Cost.** Reads are one item plus an in-memory fold over a near-empty list. The real cost is review and
test surface: "at most one boundary in flight" stops being a theorem, so consumers are verified against
consecutive-height changes.

### 1.3 Protocol reference denom

The protocol reference is the feed whose unit denominates Market's base pool and Treasury's reference-unit figures.
It is separate from the temporary ballot reference selected during aggregation. The on-chain item is owned by Oracle;
a reference names an Active feed and need not name a registered asset. It is re-pointed rather than cleared.

`MsgSetReferenceDenom` is the governance writer. First configuration has no old unit to rebase. Changing an existing
reference loads the conversion rates once and atomically invokes the Market and Treasury executors wired by
`SetReferenceDenomConsumers`. A missing executor or failed rebase rejects the whole action.

The incoming reference rate must be fresh. Without an explicit outgoing rate, the outgoing stored rate must also be
fresh. Governance may supply a positive bounded `outgoing_rate`, which replaces the outgoing store lookup entirely;
this lets a proposal explicitly state the conversion rate when the old feed is stale or has never priced. Zero or omission
selects the stored-rate path. The handler validates the override against `MaxOutgoingReferenceRate`.
This is the current implementation in [reference_denom.go](keeper/reference_denom.go) and
[msg_server.go](keeper/msg_server.go). An earlier automatic stale-outgoing-rate exception was superseded.

Market rebases its denomination-bearing base pool and pool delta together, preserving existing spread pressure.
Treasury re-expresses reference-unit figures, including tax-cap and fee/factor/exposure state. Quantities and prices use
opposite conversion directions as described below. The conversion mandate corridor is not silently rewritten; its policy
is recorded in [Market](../market/README.md#conversion-mandate).

There is no automatic promotion. A stale protocol reference closes NOAH conversion paths that need it; fresh cross-pair
conversion can still operate. A governance re-point requires an Active, freshly priced successor and, where needed, an
explicit outgoing rate. When no successor can be priced, no re-point restores pricing. Feed removal is refused while the
reference names that feed.

The reference moved from Asset to Oracle because eligibility, freshness, and removal are feed concerns. A stored fallback
was removed: automatically responding to an attacker-facing price outage with a unit-of-account change would couple the
outage to consumer rebases, and transient outages could repeatedly switch the unit. `Query/ReferenceDenom` and
`EventReferenceDenomUpdated` expose explicit changes.

### 1.4 Rate orientation: NOAH per unit

Implemented 2026-09-02 (D75–D77); D77 amended 2026-09-03. The [repository arithmetic rules](../../AGENTS.md#arithmetic-rules) state the orientation beside rounding requirements.
D75–D77 name entries in the [economic decision register](../../docs/ECONOMIC_DECISIONS.md).
The rejected approaches below explain the adopted orientation.

**Problem.** Rates were stored as units of the feed's denomination per one NOAH, Terra Classic's
orientation, where the oracle existed to report LUNA's market price. With NOAH the numeraire, three things
were wrong. The dominant operation divided: Reserve credit and movements, Treasury liability, settlement
entitlements, and the stable side of every NOAH-pair quote all divided by the rate, and the credit formula
was once re-derived as a multiplication and published the reciprocal of every figure until caught (D58);
an orientation the consumers have to invert on every use is one they will get wrong. Precision landed on
the wrong tail: `LegacyDec` loses a significant digit per decade below one, and the smallest rates appeared
exactly when NOAH was cheap and the asset expensive, the scenario where Market mints at volume. And
Treasury's conversion-factor table already stored NOAH per reference unit by inverting every block. If NOAH
is the numeraire, the ledger's statement about any asset is "one unit of X is worth *n* NOAH"; the old
store said the opposite.

**D75.** `rate[feed]` is NOAH per one unit of the feed's denomination (`USD/NOAH`); `rate[anoah] = 1`;
`value_in_NOAH(amount of X) = amount × rate[X]`. `RateSet.Convert` is `amount × rate[offer] / rate[ask]`,
so "multiply before dividing" holds by representation for the protocol's dominant valuation. Everything
carrying a rate in the store's orientation flipped with it: `SettlementPlan.redemption_rate` is NOAH paid
per unit of the settled asset (which reads better as a governance statement), and
`MsgSetReferenceDenom.outgoing_rate` is NOAH per unit of the outgoing reference, with
`MaxOutgoingReferenceRate` keeping 10^12 and now bounding the cheap-NOAH side (the expensive side needs no
cap: a rate is unrepresentable below 10^-18). Derived prices are held to the report bound: the flip moves
the halt-class multiplications onto the rate (a 2^128-capped attestation, outstanding supply), and while a
direct report is bounded on the wire, a derived price is a quotient of medians that can leave that bound
through eighteen-decimal rounding. `MaxExchangeRate` names the bound; the tally omits a derived price above
it, genesis refuses a stored rate above it, the store write keeps the check as the unreachable backstop,
and settlement plan rates are held to it too, since a plan enters the registry's rate sets beside oracle
rates and was the one stored rate the bound did not cover. Rejected: a reciprocal accessor (two orientations
in one codebase is the D58 defect with a name) and storing both (doubles vote bytes to save a division
consumers should not be doing).

**D76.** Prices and quantities rebase differently. `Convert` re-expresses a quantity; a stored price whose
unit is "NOAH per one unit of the reference" takes the reciprocal factor, `p_new = p_old × rate[new] /
rate[old]`, which is `Convert(p of new → old)`, the same call with the units in the opposite order. Every
rebase site states which it holds: the base pool, the tax cap, and the base-fee price are quantities; the
exposure anchor and the conversion-factor table are prices. The reversed argument order reads like a bug,
and straightening it out silently records the square of the cross rate as a market move; the function's
comment says the reversal is the operation, and a test asserts an anchor equal to the live price produces no
return after a move. A hand-written helper was implemented first and replaced, because it duplicated the
arithmetic path for the one figure the chain must not get wrong.

**D77.** The [sidecar resolver](../../pricefeed/sidecar/resolver/README.md) publishes final rates in the same
NOAH-per-unit orientation; providers retain their venue pair conventions.

**Unchanged.** The tally is orientation-invariant (cross rates and their medians land in the store's own
orientation in either convention; a test feeds fixtures and their reciprocals and asserts reciprocal
prices). Reward scoring is symmetric to second order (one part in ten thousand at the default band).
Staleness, quorum, attendance, feed phases, target versions, the denom grammar, external symbols, and every
consumer that only calls `Convert` are unchanged in behaviour. The volatility series needs no inversion
(the two returns differ at third order under the symmetric clamp). Precision: at a $0.000001 NOAH price the
worst feed keeps 21 digits under D75 against 9 before; at $100 it keeps 13 against 17, giving up digits
where the protocol is comfortable to keep them where it mints against redemptions. Migration: none, because
the flip landed before any network held state; otherwise it would have been a coordinated upgrade inverting
every stored rate with every sidecar switching at one height and a vote-extension version bump. `RawCredit`
on a zero rate credits nothing instead of erroring, since the error was a property of the division. Display
rendering landed 2026-09-03 as both readings on the exchange-rate queries (`units_per_noah` beside the
stored rate), derived in the query server and consumed by nothing on chain.

## Development

Run from the repository root:

```sh
go test ./x/oracle/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds shared test helpers and mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/oracle/](../../proto/ark/oracle/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/oracle/v1/tx.proto),
[queries](../../proto/ark/oracle/v1/query.proto), and [events](../../proto/ark/oracle/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Reward funding](../../docs/ECONOMIC_DESIGN.md).
- [Application wiring](../../app/README.md).
