# Asset registry

This module owns the registry and lifecycle of governance-managed Bank assets other than NOAH. It supplies lifecycle-aware views to Market and Treasury and reports feed referents to Oracle. A feed can exist without an asset; registering an asset is a separate economic action.


[Overview](#1-the-registry-in-one-page) · [Decisions and scope](#2-core-decisions) · [State](#6-state) · [Lifecycle](#7-the-lifecycle) · [Operations](#8-lifecycle-operations) · [Emergency mandate](#9-the-emergency-mandate) · [Consumer views](#11-consumer-pricing-views) · [Development](#development)

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/assets.go](keeper/assets.go) | Registry access and status/event helpers. |
| [keeper/lifecycle.go](keeper/lifecycle.go) | Governance-driven lifecycle transitions. |
| [keeper/settlement.go](keeper/settlement.go) | Settlement plans and resolution records. |
| [keeper/pricing.go](keeper/pricing.go) | Lifecycle-aware pricing views. |
| [keeper/emergency_mandate.go](keeper/emergency_mandate.go) | Emergency mandate and per-term suspension tracking. |
| [keeper/feed_guard.go](keeper/feed_guard.go) | Claims that prevent removal of a required oracle feed. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`Assets`, `SettlementPlans`, `ResolutionRecords`, `EmergencyMandate`, and `EmergencySuspensions` are declared with the schema in `keeper/keeper.go`. The module has no block hook: transitions execute through messages. Oracle feed promotion does not complete asset transitions.

D-numbers refer to the [economic decision register](../../docs/design/ECONOMIC_DECISIONS.md).

## 1. The registry in one page

`x/asset` is the authoritative registry and lifecycle owner for every governance-managed Bank asset other than NOAH.
It supports stablecoins and tokenised commodities alike, because no consumer cares which an asset is: the axis that
matters is convertibility, and every registered asset is convertible by invariant. Conversion through Market is the
chain's only native mint path, so an asset outside the conversion loop could never acquire supply, and the ecosystem is
deliberately built around NOAH as the sole monetary basis.

An asset is priced by the feed its own denomination keys. Feeds live in `x/oracle` and may exist without an asset, so
observing a price never creates a liability; registering an asset is the act that does. Registration takes the
denomination alone, derives the Bank metadata from it, requires the feed to be Active, and stores the asset `ACTIVE` at
version one. From that block the asset is convertible, taxable, and counted as liability, with no enrolment set
anywhere: Market's eligibility, Treasury's tax base, and Treasury's liability partition are all derived from the
registry.

Five economic statuses describe how the protocol treats an asset's supply. Ordinary wind-down runs `ACTIVE` to
`ISSUANCE_HALTED` to `RETIRED`, preserving pricing and redemption until governance closes the books on a bounded,
disclosed residual. Distress runs through `SUSPENDED`, which closes the one unbounded channel from an asset's failure
into NOAH, ordinary conversion at the reference rate; from suspension onward the asset's maximum remaining claim on
NOAH is a closed, governance-chosen bound, zero with no settlement open and outstanding supply times a fixed rate with
one. Write-off derecognises without touching a balance; recovery restores pricing and redemption but never issuance
directly. A governance-appointed committee holds exactly one power, suspension, because it is the only act with
containment value whose failure mode is reversible.

The emergency lifecycle contains the failure of one asset. It does not defend NOAH: every backstop here spends bounded
NOAH dilution to make one asset's failure orderly, and none can work when NOAH's credibility is what failed.

## 2. Core decisions

- **Asset is the domain name.** No module stores a stable-versus-commodity distinction. Every registered asset is
  convertible, so swap eligibility, tax-cap membership, and liability recognition all derive from Oracle-priced
  membership, `ACTIVE` or `ISSUANCE_HALTED`. This superseded two earlier decisions, support for assets outside the chain
  Oracle and a Treasury enrolment set.
- **`anoah` is outside the lifecycle.** NOAH is not priced; it is what prices everything else.
- **A feed is keyed by the denomination it prices.** The price source is the asset's identity, not a recorded choice.
  Two assets can never share a feed, no asset can be unpriced, and no message can re-point an asset at other price data.
- **The feed registry is independent of the asset registry and is the larger of the two.** It shares the asset key
  space, so a commodity can be priced under the denomination it would carry long before, or without ever, being listed.
  The protocol reference and the Reserve's external symbols are the non-asset consumers that made this v1 rather than
  deferred (`x/oracle/README.md` §1.2).
- **The denomination is immutable and everything about it is derived.** A denomination is `a` followed by two to
  fifteen lowercase alphanumerics, tighter than the SDK charset, because a name the Oracle could not carry as a feed key
  must not exist as a denomination. Description, name, symbol, display unit and exponent are functions of the
  denomination, so a registration states one thing, has nothing that could be misspelled, and needs no amendment
  message. Every native asset has an `a<display>` base unit at exponent zero and `<display>` at exponent eighteen.
- **Status describes economic treatment, never intent.** Oracle feed phase and settlement-plan presence are separate
  authoritative state, and a status name says what the protocol does with the supply, not where governance means it
  to end up.
- **Recovery restores pricing, not issuance.** Recovery from suspension lands in `ISSUANCE_HALTED`; issuance returns
  only through a separate `ResumeIssuance`.
- **Write-off never burns, transfers, or reprices a holder's balance.** It changes recognised obligation and appends
  permanent history.
- **Derecognition is never automatic.** No timer or height writes off an obligation. Governance finalising a bounded
  residual is a decision, and is permitted.
- **A committee acts where only speed is needed, never where terms are chosen.** The emergency mandate reuses the
  shared envelope, monotone term, exact account address, expiring window, governance replacement at any time, to
  suspend in minutes, bounded to one suspension per asset per term. Halts, settlement, write-off, recovery, and
  retirement carry terms and stay with governance.
- **Governance corrects itself at defined points, and holder commitments bind after them.** A settlement plan is
  cancellable before it activates and binding from then; the correction window before activation is a real, governed
  delay.
- **Every state has an honest path to `RETIRED`,** and no transition needs a sham act such as recovering a fully
  settled asset only to remove its plan.

## 3. Scope

The module does not provide, and by decision will not: feed keys outside Ark-native base denominations; arbitrary
non-asset Oracle observations; an on-chain provider or exchange-symbol registry; issuer, custody, proof-of-reserve, or
physical redemption machinery; a general exchange; lifecycle control over `anoah` or over IBC assets; native assets
with a display exponent other than eighteen; any timer-driven derecognition; a generic mandate module, since the
shared envelope and `pkg/mandate` are the permanent boundary and powers stay domain-owned; any native mint path outside
Market conversion, which tests hold by inspecting module-account permissions; or any systemic backstop for NOAH itself.

Provider markets and resolver routes stay off-chain and generalised. The sidecar may combine `XAU/USD` and `USD/NOAH`,
but consensus receives one final NOAH-relative rate per feed.

## 4. Price contract

An Oracle rate is NOAH per one unit of the feed's denomination, and `RateSet.Convert` is `amount × rate[offer] /
rate[ask]`, so valuing in NOAH multiplies (D75). Every Ark-native asset uses exponent eighteen, so a provider price in
display units has the same numeric value at the base-unit boundary and transport needs no exponent metadata. Rates are
`LegacyDec` values inside the bounds `pkg/decimal` enforces, a finite, documented band; arithmetic fails closed outside
it, and a proposal to list an asset whose expected rate sits near either edge is a review item rather than an
arithmetic surprise.

A settlement plan quotes in the same orientation, NOAH paid per one unit of the settled asset, so a plan can serve as
a rate set unchanged: `output anoah = floor(input × redemption_rate)`. The fixed rate is a one-way entitlement while
the plan is active. It is not an Oracle observation, never enters ordinary conversion, and never permits issuance;
rounding down prevents payout amplification through splitting, and a zero-output redemption fails.

## 5. Ownership

| Module | Owns | Never owns |
| --- | --- | --- |
| `x/asset` | Registration and Bank metadata; economic status and version; settlement plans; append-only resolution records; the emergency mandate and per-term suspension usage; lifecycle-aware pricing views | Validator reports, aggregation, stored rates, freshness; Market curves and Tobin policy; minting and burning; Treasury enrolment or balances |
| `x/oracle` | The feed registry and its transitions, reporting, aggregation, accounting, stored rates, freshness, pruning, the protocol reference denomination | Whether an asset may be issued, redeemed, settled, suspended, written off, or retired |
| `x/market` | Conversion eligibility derived from the registry, Tobin policy, ordinary and settlement redemption, mint and burn; its pool in reference units, rebased through the Oracle's reference executor | Membership |
| `x/treasury` | Tax caps and liability derived from the registry; its cap in reference units, rebased the same way | Membership |

Freshness has exactly one definition, the Oracle's, consumed through `RateSet`; no lifecycle transition defines a
staleness policy of its own, and none consults a rate at all. Transitions gate on feed phase and leave freshness to the
point of use.

The base-pool unit and the tax-cap unit are the same protocol reference denomination, permanently, and it lives in
`x/oracle` because every rule it has is a feed rule: eligibility is a feed phase, identity is the feed-key rule, and the
rebase preconditions are rate freshness. This reversed three earlier decisions, consumer-owned fallbacks, an
asset-keyed reference with a stored fallback, and asset ownership of the reference ([Oracle reference policy](../oracle/README.md#13-protocol-reference-denom)).

## 6. State

```text
Params                collections.Item[Params]                                  // settlement_activation_delay_blocks
Assets                collections.Map[denom, Asset]                             // denom, metadata, status, version
SettlementPlans       collections.Map[denom, SettlementPlan]                    // rate, activation, closing, opened
ResolutionRecords     collections.Map[(denom, version), ResolutionRecord]       // kind, height, supply, final plan
EmergencyMandate      collections.Item[EmergencyMandate]                        // the shared envelope
EmergencySuspensions  collections.KeySet[denom]                                 // this term's suspensions
```

An `Asset` carries no pending-transition state. Every status change is a governance act that verifies its own feed
precondition and takes effect in the block it executes, so nothing is in progress between blocks and no automatic path
moves an asset. Metadata is stored rather than derived on read, so an exported genesis describes itself and Bank's
record cannot drift from the registry's, and it is validated against the derivation, so the stored copy is the
derivation or the state is invalid. The description names the currency from a table in `pkg/chain`; because the check
runs at every import, a table entry is added only before its denomination is registered and never changed after.

Feed membership is not asset state. `x/oracle` owns the `Feeds` registry, its version, and its transition records, moved
by `MsgAddFeed` and `MsgRemoveFeed`; a feed must be Active before an asset may register under its denomination, so the
two registries are populated in that order and the feed set is always the larger.

A `SettlementPlan` fixes a positive NOAH-per-asset rate, an activation height derived from the opening height plus the
governed delay, and a mandatory earliest closing height. Every term is set at opening and never amended, and the plan
is authoritative only while attached to a `SUSPENDED` asset. `ResolutionRecords` are immutable, keyed by denomination
and version, and carry a kind, `WRITE_OFF` or `RETIREMENT_RESIDUAL`, the supply outstanding at resolution, and the
plan's final terms when one existed. A later recovery never erases or reinterprets a record.

`version` exists so a proposal drafted against one configuration cannot act on another. Registration stores one, and
every governance mutation advances it: each status transition, settlement open, cancel, and close, and write-off.
Nothing else does, so `expected_version` is fully predictable for a proposal author. Status preconditions remain the
authoritative guard on every operation; the version is defence in depth. Committee actions are governance-equivalent
and advance the version too, but carry the mandate term instead of a version, moving staleness protection from the
asset to the authority.

## 7. The lifecycle

Five statuses are persisted:

```text
ACTIVE -> ISSUANCE_HALTED -> RETIRED
   ^            |
   +------------+  ResumeIssuance

ACTIVE or ISSUANCE_HALTED -> SUSPENDED -> WRITTEN_OFF
                                 |              |
                                 +<-------------+  reinstate (recovery, or a new settlement)
                                 |              |
                                 |              +-> RETIRED (finalise, residual permitted)
                                 +-> ISSUANCE_HALTED (RecoverAsset)
                                 +-> RETIRED (zero supply)
```

There is no status between registration and admission. Registration demands the feed, not a rate, so `ACTIVE` without
a rate is a state the chain reaches whenever a feed is young or stale, and every consumer handles both identically. A
feed addition and the registration against it never share a proposal: governance proposes the feed, watches it print,
then registers. That cycle falls where it is cheapest, on the one admission path with no holders waiting and the least
proven price data.

- **`ACTIVE`.** Issuance, conversion, valuation, liability accounting, and redemption all run. The asset pins its feed
  against removal for as long as it stays Oracle-priced.
- **`ISSUANCE_HALTED`.** Issuance stops; transfers, pricing, redemption, and liability accounting continue, and Market
  may consume the asset but never produce it. The status carries no intent: an asset winding down and one just
  recovered from suspension look identical, and it is never produced by an emergency act, so it cannot read as a
  distress signal and start the run it was never meant to announce.
- **`SUSPENDED`.** Issuance, ordinary conversion, and ordinary redemption stop; balances and transfers are untouched. A
  stored rate is not trusted for this asset's economics, but the feed keeps running and may even denominate the
  protocol reference; the status gate each consumer applies is the containment. Without a plan the supply is disclosed
  as untrusted exposure; with one, fixed one-way redemption is available.
- **`WRITTEN_OFF`.** Governance recognises no obligation. Issuance, conversion, pricing, and redemption are off;
  transfers continue; supply is excluded from liability and disclosed separately. Reinstatement returns to
  `SUSPENDED`; retirement is available directly.
- **`RETIRED`.** Terminal. No message leads out, the denomination is spent, and the record and Bank metadata remain as a
  tombstone that registration refuses to collide with. Residual supply may remain and stays transferable when a
  resolution record discloses it. A successor uses a new denomination.

Settlement is derived from plan presence, not status. A `SUSPENDED` asset without a plan is untrusted exposure; with one
it settles at or after activation; a plan on any other status is invalid state. Exactly one message reads the plan's
earliest closing height, `WriteOffAsset`, which is refused before it, and that is the whole holder guarantee: once your
redemption opens, nothing can derecognise you before the announced height. The other plan-ending paths need no gate.
Cancellation runs only before activation, when no holder relied on a window; recovery restores the ordinary exit the
plan substituted for; and finalisation reaches a plan only on a `SUSPENDED` asset at zero supply, which nobody can
still redeem against. A committed window never threatens the system, because supply cannot grow while suspended and
the rate is fixed, so the plan's maximum entitlement was knowable when it opened.

## 8. Lifecycle operations

Every governance message names the denomination and, except registration, the expected version. All are governance
messages except the one committee message in §9.

- **`RegisterAsset`.** Refuses `anoah`, any registered denomination including a tombstone, pre-existing Bank supply or
  metadata, and any feed not in phase Active. The feed is checked last, so a denomination already spoken for is
  reported as the collision it is. It derives the metadata, stores `ACTIVE` at version one, and sets Bank metadata as
  its sole owner without minting or granting mint authority.
- **`HaltIssuance`** moves `ACTIVE` to `ISSUANCE_HALTED`; **`ResumeIssuance`** moves it back. Halting is governance's
  alone: it contains nothing in distress, since the exit leg keeps converting at the full rate, and its only crisis
  effect is a run-starting signal through a door it holds open. It is for the cases where preserving every exit is the
  point, a deliberate wind-down or an inflow-side problem such as a mint-path exploit.
- **`SuspendAsset`** moves `ACTIVE` or `ISSUANCE_HALTED` to `SUSPENDED` at once, stopping issuance, conversion, and
  ordinary redemption while preserving balances, transfers, metadata, and disclosure. It never touches the protocol
  reference, which names a feed, and it leaves the feed running.
- **`OpenSettlement`** requires `SUSPENDED` or `WRITTEN_OFF`, positive supply, and Market's settlement eligibility; a
  written-off asset becomes `SUSPENDED` again. Governance states the rate and the earliest closing height only.
  Activation is derived from the governed delay, because the delay is a correction window that benefits governance
  alone while holders wait unable to redeem, and a proposed absolute height would fall inside the delay if a vote ran
  long. The delay is a parameter because the window it must outlast, the voting period, moves without consulting the
  module; it defaults to three days of blocks, a two-day vote with a day's margin, is refused at zero, and is capped at a
  year, past which it is a standing ban.
  It is read once, at opening, so changing it never reaches terms holders were shown. No term is ever amended: a
  mutable rate would make the maximum entitlement unknowable and redemption timing strategic.
- **`CancelSettlement`** requires the plan not to have activated, removes it, and leaves the asset `SUSPENDED`. It is
  the whole correction window.
- **`RecoverAsset`** requires `SUSPENDED` or `WRITTEN_OFF` and the feed in phase Active, closes any plan in the same act
  without consulting its window, and lands in `ISSUANCE_HALTED`. The plan's frozen rate cannot coexist with the live one
  now governing conversion, and it is never compared against the live rate: above it it overpays and dilutes, below it
  it shortchanges, and it is the degraded instrument whichever way it points, which is why recovery ends it. Recovery
  does not arm and wait for a rate, because an armed recovery would fire on a price nobody voted on; governance
  satisfies itself the feed is stable, then proposes. Landing halted rather than active keeps `ISSUANCE_HALTED` free of
  intent and makes a premature recovery re-suspendable with exposure unchanged.
- **`WriteOffAsset`** requires `SUSPENDED` and the earliest closing height reached, closes any plan emitting its terms,
  appends a `WRITE_OFF` record with current supply, and moves to `WRITTEN_OFF` without touching a balance.
- **`FinaliseRetirement`** ends every path in `RETIRED` and closes any plan on the way out. From `ISSUANCE_HALTED` it
  carries `max_residual_supply`, requires supply at or below it, and appends a `RETIREMENT_RESIDUAL` record when
  supply is positive: redemption was continuously available, so governance may judge the remainder unredeemable and
  say so. From `SUSPENDED` it requires zero supply, because those holders may have had no exit and a positive residual
  must go through write-off, which names the derecognition honestly. From `WRITTEN_OFF` residual is permitted and no
  new record is appended, since the `WRITE_OFF` record already discloses it. IBC-escrowed supply counts as
  outstanding: tokens stranded on a dead counterparty are residual exactly like lost wallets.

There is no path out of `RETIRED`. The lifecycle already carries two reversible pairs, halt and resume for a pause,
suspend and recover for distress, the latter reaching back even from `WRITTEN_OFF`, and staying halted costs nothing,
so retirement is what governance reaches for when none of that is wanted any longer. A comeback would also have
resurrected the one dangling-state hazard with teeth, a stale Market override on a relisted denomination; a successor
under a new denomination inherits none of it. There is likewise no path for a registration governance regrets: it
unwinds like any other asset, halted first so redemption stays open, and the Bank metadata a registration writes is
permanent whichever path ends it.

## 9. The emergency mandate

The `EmergencyMandate` is exactly the shared envelope: a chain-derived monotone term, the exact committee address, and a
half-open activation and expiry window, with an empty committee as the disabled state. `SetEmergencyMandate` is
governance's, every replacement advances the term and clears the term's suspension usage, and mandate mutations never
touch an asset's version.

The committee's power is one: `EmergencySuspendAsset`, with `SuspendAsset` semantics, carrying the denomination and the
exact current term. There is deliberately no emergency halt. Ordinary conversion is the contagion channel and a halt
leaves it open, so an emergency halt would pay the full suspension latency in unbounded dilution while broadcasting a
committee-confirmed distress signal through a door it held open, a run accelerant with a first-mover advantage. The
error asymmetry settles it: a wrong suspension is recovered by governance and moves no value, a wrong halt is paid in
dilution nothing returns, so the committee's single tool is the one whose failure mode is reversible.

Execution rules:

- A committee message executes only inside the window and skips `expected_version`; the term is the staleness guard,
  and status preconditions still apply. A version mismatch discovered after mustering the committee would be the worst
  failure mode the emergency path could have.
- It executes immediately, with no cancellation period: the Claims delay protects irreversible value outflow, while
  suspension moves no value and is reversible through recovery. Governance's protection is after the fact.
- Each asset admits one suspension per term. Re-suspending an asset governance has recovered is griefing and requires
  governance; without this rule a committee re-suspends faster than governance can recover. The usage record is a
  key set of denominations, cleared on replacement, so no counter can drift from the set it summarises. There is no
  numeric per-term allowance, because a count bounds nothing this rule does not and fails closed in exactly the
  correlated-failure case the mandate exists for.
- The action never reads or writes the protocol reference.

The committee cannot recover, resume, open, cancel, or close settlement, write off, retire, register, schedule a feed
transition, choose the reference, or modify any mandate. Its worst abuse suspends one healthy asset for the days
governance needs to recover it and replace the committee.

Suspension latency is a design property because until it executes the failing asset redeems at its full reference
rate. The mandate is the primary path, minutes at any hour; a committee that cannot muster its threshold at that speed
is theatre, and membership, threshold, and rotation are chosen with that test in mind. Governance is the backstop and
the universal authority: `SuspendAsset` takes effect when a passed governance proposal executes, touches neither feed state nor
the reference, and is the only path when the mandate is disabled, expired, or already used on that asset this term, and
for everything with terms attached. Detection, dispersion of oracle votes, unavailable-target quorum events, and
one-directional conversion flow, is operational work additive to every path.

## 10. Feed dependency

The asset guard pins its denomination's feed while the asset is `ACTIVE` or `ISSUANCE_HALTED`.
Suspension, write-off, and retirement release that claim; registration and recovery check feed phase themselves.
Feed promotion never changes asset status. The [oracle README](../oracle/README.md#12-feed-registry) owns scheduling,
removal guards, and reference changes. Suspending an asset never changes the protocol's unit of account.

## 11. Consumer pricing views

The registry supplies membership and per-denomination pricing verdicts, deciding status before freshness. A verdict
selects fresh Oracle pricing, an attached settlement plan, last-known pricing, or unvaluable exposure. Queries join
records to those verdicts; one unpriced asset leaves its own rate empty without failing the whole response.
A settlement rate never enters ordinary `RateSet` conversion.

Market applies asymmetric offer/ask eligibility; Treasury applies the liability partition. Their contracts live in
[Market](../market/README.md#conversion-policy) and [economic design](../../docs/design/ECONOMIC_DESIGN.md#71-the-liability-partition).
There is no bundled record-plus-rate API: consumers already own their admission rules and may need a reference feed
that is not a registered asset. There is no consumer-written membership or lock index to keep in sync.

## 12. Protocol surface

Governance messages: `RegisterAsset` (the denomination alone), `HaltIssuance`, `ResumeIssuance`, `SuspendAsset`,
`OpenSettlement` (rate and earliest closing height), `CancelSettlement`, `RecoverAsset`, `WriteOffAsset`,
`FinaliseRetirement` (`max_residual_supply`), `SetEmergencyMandate`, and `UpdateParams` for the activation delay. One
committee message, `EmergencySuspendAsset`, term-checked. Queries: `Params`, `Asset`, `Assets` (returned whole, since
the registry is governance-bounded), `SettlementPlan`, `ResolutionHistory` (paginated), `EmergencyMandate`; feeds and
the reference are queried from `x/oracle`. Events: `EventAssetRegistered`, `EventAssetStatusChanged` (old and new
status with the version), `EventSettlementOpened`, `EventSettlementCancelled`, `EventSettlementClosed`,
`EventAssetResolved`, `EventEmergencyMandateSet`, `EventEmergencySuspended`. The `EmergencyMandate` embeds the shared
envelope from `proto/ark/mandate/v1/envelope.proto`.

Genesis carries params, assets, settlement plans, resolution records, the emergency mandate, and the term's
suspensions. Validation requires deterministic order; every Oracle-priced asset's feed in phase Active, checked at the
keeper because the feed registry is `x/oracle` state, which is why the init order is bank, oracle, asset, market,
treasury; plans only on `SUSPENDED` assets; a matching `WRITE_OFF` record for a written-off asset's current version and
a matching record for any `RETIRED` asset with positive supply; a coherent mandate window; and every recorded
suspension naming a registered asset. The launch set is the ten stablecoins in `pkg/chain/denom.go`, all `ACTIVE` at
version one (`docs/governance/GENESIS.md` §9).

The module has no BeginBlocker, EndBlocker, or preblock hook. Every transition is a governance message that checks its
own precondition and applies in its block, so no rate arrival and no elapsed height can move an asset. It needs narrow
Bank access, supply and denomination metadata, and from the Oracle the available and last-known rate sets and the feed
phase. `x/oracle` does not depend on `x/asset`; Market and Treasury read the registry through their own declared
interfaces, membership, pricing verdicts, asset records, and active plans.

The code map above identifies the keeper, types, and module entry points.

## 13. Superseded and closed

These rejected mechanisms explain the current boundaries; they are not implementation work.

- **A `PENDING` status, a completion hook, and the `ActivateAsset`, `AmendRegistration`, `BeginRecovery`,
  `CancelRecovery`, and `ReactivateAsset` messages.** An asset exists exactly when it is listed, transitions apply in
  their block, metadata is derived so nothing needs amending, and `RETIRED` is terminal.
- **The `AssetLocks` index**, an inverse dependency map downstream modules wrote into. Consumers derive membership, so
  there was nothing to lock; the referent guard replaced the one hazard it addressed.
- **`PricedLiveVersion` and `TaxCapsEpoch`**, the membership epochs consumers compared each BeginBlocker. Treasury's
  per-block conversion-factor table replaced them.
- **The `PricedAssetView`**, a bundled record-plus-rate boundary, removed unused for the reasons in §11.
- **Asset ownership of the protocol reference, a stored fallback rate, and consumer-owned fallbacks.** The reference is
  a feed rule and lives with the feeds.
- **Oracle's Tobin-tax parameter, its coherence rule, and its queries.** Market owns Tobin policy; the launch feed list
  is declared directly in `x/oracle`.
- **The interim containment for the confirmed target-removal defect**, which refused every current-target removal from
  Oracle params until the params-driven membership path itself was deleted. What protects a feed today is the
  consumer-side removal guard.
- **A live-chain upgrade path.** The chain launches from a clean genesis (D20).

## Development

Run from the repository root:

```sh
go test ./x/asset/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds shared test helpers and mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/asset/](../../proto/ark/asset/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/asset/v1/tx.proto),
[queries](../../proto/ark/asset/v1/query.proto), and [events](../../proto/ark/asset/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Liability and conversion policy](../../docs/design/ECONOMIC_DESIGN.md).
- [Application wiring](../../app/README.md).
