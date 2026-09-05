# Asset Module Plan

Status: implemented, Phases 0 through 6 — the pricefeed decoupling re-homed feed epochs to `x/oracle`, a feed is keyed
by the denomination it prices, `x/asset` is wired into the application, Market owns conversion policy, Treasury derives
its tax base, liability partition, and cap membership from the asset registry, and Oracle's Tobin-tax surface is gone.

## Purpose

Build `x/asset` as the authoritative registry and lifecycle owner for governance-managed Bank assets other than NOAH.
The module must support stablecoins and tokenized commodities such as gold. Every registered asset is priced by the
feed its own denomination keys, and is convertible through Market: conversion is the chain's only native mint path, so
an asset outside the conversion loop could never acquire supply, and the Ark ecosystem is deliberately built around
NOAH as the sole monetary basis. Pricing without listing lives at the feed layer — a feed may exist with no asset
carrying its denomination — so observing a price never creates a liability; registering an asset is the act that does.

Normal retirement must stop issuance before removing support for outstanding supply, and must be able to close the books
on an asset whose residual supply will never be redeemed. Emergency suspension must stop unsafe economic operations
without deleting balances, treating missing valuation as zero, or silently erasing a redemption obligation.

The emergency lifecycle exists to contain the failure of an individual asset, not to defend NOAH. Ordinary conversion
prices an asset by its reference, not by its own distressed market price, so a failing asset redeems at full reference
value until suspension executes; suspension is the act of containment, and from that moment the asset's maximum
remaining claim on NOAH is a closed, governance-chosen bound. A loss of confidence in NOAH itself is outside this
module's power to contain and outside its scope: every backstop here spends bounded NOAH dilution to make one asset's
failure orderly, and none of them can work when NOAH credibility is what failed.

Every lifecycle state must have an honest path to the terminal state. No transition may require a sham operation, such
as recovering a fully settled asset solely to remove its settlement plan.

## Core decisions

- Asset is the general domain name. Stablecoin is a description, not a classification: no module stores a
  stable-versus-commodity distinction, because the axis every consumer cares about is convertibility, and every
  registered asset is convertible by invariant.
- `anoah` is the native base denomination and remains outside this lifecycle.
- Every Ark-native asset defines at least an `a<display>` base unit at exponent 0 and `<display>` at exponent 18. Sorted
  intermediate units are permitted; aliases are not.
- A feed is keyed by the denomination it prices, so an asset's price source is not a separate choice the registry
  records — it is the asset's own identity. That feed carries one rate expressed directly against NOAH today, and may
  later be a derived basket; either way two assets can never share it, and no asset can be unpriced, because conversion
  is the only mint path and conversion needs a rate.
- Every asset is convertible through Market, and only Market ever holds mint authority. No native issuance path outside
  conversion will exist: the Ark ecosystem is built around NOAH, and native money created outside the conversion loop
  would fragment the backstop that makes per-asset failure containable. Oracle-priced membership therefore *is*
  convertibility, and swap eligibility, tax-cap membership, and liability recognition all derive from it — no consumer
  enrollment set exists anywhere. This supersedes two earlier decisions: support for assets that do not use the chain
  Oracle, and the planned Treasury `StableAssets` enrollment.
- A feed registry independent of the asset registry is v1, not deferred. The deferral recorded here previously was
  reversed once the protocol acquired non-asset price consumers — a basket-indexed flagship currency and
  strategic-reserve marks. What the reversal buys is a feed layer that is a superset of the listed assets, not a
  separate identifier space: feeds and assets share one key, so a commodity can be priced under the denomination it
  would carry long before, or without ever, being listed. The reversal and its reasoning are recorded in
  `docs/DESIGN_NOTES.md` §1.2.
- `x/asset` owns identity, Bank metadata, economic lifecycle, settlement terms, resolution
  history, and lifecycle-aware valuation access.
- `x/oracle` owns the feed registry and its epochs, validator reporting, aggregation, accounting, stored rates,
  freshness, and rate pruning. Freshness has
  exactly one definition, owned by `x/oracle`; lifecycle transitions that depend on a fresh rate consume that definition
  through `RateSet` and never define their own staleness policy.
- `x/market` owns ordinary conversion eligibility, Tobin-tax policy, settlement execution, minting, and burning.
- `x/treasury` owns reference-asset policy, liability reporting, and redemption-buffer accounting.
- Asset status describes economic treatment only, and status names must describe that treatment, never an intent or a
  predicted destiny. Oracle transitions and settlement-plan presence are separate authoritative state.
- Recovery after emergency suspension restores normal pricing and redemption into `ISSUANCE_HALTED`. Issuance resumes
  only through a separate, explicit `ResumeIssuance` transition.
- Write-off never burns, transfers, or reprices holder balances. It changes recognized protocol obligation and appends
  permanent resolution history.
- Suspension closes the only unbounded transmission channel from an asset failure into NOAH: ordinary conversion at the
  reference rate. From suspension onward the asset's maximum remaining claim on NOAH is closed — zero while no
  settlement is open, at most outstanding supply times the fixed rate while one is, and zero again after write-off.
  Recovery deliberately reopens ordinary pricing and redemption; that is its meaning, and it is why completion requires
  a rate the fleet aggregated after governance began the recovery.
- A bounded committee may act where only speed is needed, never where terms are chosen. The Asset Emergency Mandate
  reuses the treasury mandate pattern — monotonic term, exact threshold-multisig committee, expiring height window,
  governance replacement at any time — to execute suspensions in minutes, bounded by one suspension per asset per
  term. Suspension is its only power: halting issuance contains nothing a crisis cares about, so it stays
  governance-only alongside settlement, write-off, recovery, and every other decision with terms attached.
- Derecognition is never automatic. No timer or height threshold may write off an obligation on its own. Explicit
  governance finalization that derecognizes a bounded residual is a decision, not an automation, and is permitted.
- The denomination is immutable forever, and because it is also the feed key, the price source is immutable with it: no
  message can re-point a registered asset at different price data. Bank metadata is immutable with them both, because
  it is derived from the denomination rather than supplied alongside it — description, name, symbol, display unit and
  exponent are all functions of the denom. A registration states one thing, so it has nothing that could be misspelled
  and needs no message to correct it.
- A denomination is shaped so that it can always key a feed, and that is the chain's only denomination rule. `a`
  followed by two to fifteen lowercase alphanumerics is deliberately tighter than the SDK denomination charset, which
  admits punctuation: a name the oracle could not carry as a feed key must not be able to exist as a denomination in
  the first place, since every asset is priced by invariant. Everything that validates a denomination anywhere — a
  registered asset, a settlement plan, a Market Tobin override, a tax cap, a resolver route, the protocol reference — is
  validating something that must be able to carry a feed, so one rule serves them all, and `anoah` is excluded by it
  for the same reason it has no feed: NOAH is not priced, it is what prices everything else. Code that legitimately
  handles NOAH compares against the constant instead of validating. The three-character floor also keeps every feed key
  a valid SDK denomination, so rates travel as `DecCoins` without a key that could not be one.
- Governance corrects its own mistakes at defined points, and holder-facing commitments bind after them. A settlement
  plan is freely cancellable before activation and binding from it. Assurance to redeeming holders outweighs
  mid-window correctability by design, so the correction window before activation must be real: plans activate only
  after a minimum delay.

## Scope boundaries

Version 1 does not provide:

- feed keys drawn from any namespace other than Ark-native base denominations;
- arbitrary non-asset Oracle observations;
- an on-chain provider or exchange-symbol registry;
- issuer, custody, proof-of-reserve, or physical redemption machinery;
- a general-purpose decentralized exchange;
- lifecycle control over `anoah`;
- native assets with display exponents other than 18;
- lifecycle control or repricing of IBC assets;
- timer-driven or height-driven automatic derecognition of any obligation;
- a generic mandate or committee module: the shared `ark.mandate.v1` envelope and `pkg/mandate` helpers are the
  permanent boundary, and authority powers stay domain-owned;
- any native mint path outside Market conversion, permanently: only Market holds `Minter`, and tests inspect the
  configured module-account permissions to hold that line;
- any systemic backstop for NOAH itself: the emergency lifecycle contains individual asset failures, and no mechanism in
  this plan attempts to contain a loss of confidence in the base denomination;
- live-chain compatibility unless deployment requires an upgrade from committed state.

Provider markets and resolver routes remain generalized off-chain. The sidecar may combine inputs such as `XAU/USD` and
`USD/NOAH`, but consensus receives one final denomination-specific NOAH-relative rate.

## Price contract

The Oracle rate contract is:

    rate[denom] = base-denom units of denom per one anoah
    rate[anoah] = 1

Conversion remains:

    ask amount = offer amount * ask rate / offer rate

All Ark-native assets use exponent 18, so a provider price expressed in display units has the same numeric value at the
base-unit consensus boundary. Target transport therefore does not require exponent metadata.

Rates are `LegacyDec` values with 18 fractional digits and the integer bounds enforced by `pkg/decimal`. This defines a
finite band of representable and precision-safe rates. The plan requires a documented supported band, arithmetic that
fails closed outside it, and treatment of any proposal to list an asset whose expected rate sits near either edge as an
explicit review item rather than an arithmetic surprise.

A governance settlement uses a different contract, quoted in the same orientation as an Oracle rate so a plan can serve
as a rate set unchanged:

    redemption_rate[denom] = NOAH paid per one unit of the settled asset
    output anoah = floor(input asset amount × redemption_rate)

The fixed settlement rate is a one-way entitlement while its plan is active. It is not an Oracle observation, must not
enter ordinary conversion, and never permits asset issuance. Rounding down prevents payout amplification through
transaction splitting, and zero-output redemptions fail. The entitlement is prospective, not perpetual: governance may
close the plan once its announced window has run or supply is exhausted, and executed redemptions always stand.

## Ownership model

### x/asset

Owns:

- asset registration and Bank metadata;
- asset economic status and version;
- the protocol `Reference` feed;
- the `PricedLiveVersion` membership epoch;
- active `SettlementPlans`;
- append-only `ResolutionRecords`;
- the `EmergencyMandate`, per-term `EmergencySuspensions` usage, and the committee execution path;
- lifecycle-aware priced-asset and retirement-readiness views.

Does not own:

- validator reports, rate aggregation, stored rates, or freshness;
- Market curves, Tobin taxes, minting, or burning;
- Treasury stable-liability enrollment or buffer balances.

### x/oracle

Owns denomination-keyed reporting, aggregation, accounting, rate storage, freshness, and pruning. It does not decide
whether an asset may be issued, redeemed, settled, suspended, written off, or retired.

### x/market

Owns supported converter assets, per-asset conversion policy, and execution of ordinary and settlement redemption. It
consumes lifecycle-checked views from `x/asset`. State it holds in reference units — the base-pool `DecCoin` and the
pool delta — rebases through the oracle module's reference executor when the protocol reference moves.

### x/treasury

Owns reference-tax-cap policy, liability classification, redemption accounting, and the shared buffer. Tax-cap
membership and liability inclusion are derived from `x/asset` rather than enumerated locally: membership is
`OraclePricedDenoms`, and the liability set is every Oracle-priced asset whose status is not `RETIRED`.
State it holds in reference units rebases through the oracle module's reference executor when the protocol reference
moves.

The base-pool reference and the reference-tax-cap denomination are the same unit by design, permanently. A single
chain-level protocol reference therefore names that denomination — set by governance only, read as a feed key, with
each consumer's reference-unit state rebased through executor interfaces when it moves. It lives in `x/oracle`, which
keys it by the feed that prices it: every rule it has is a feed rule. This reverses three earlier decisions:
consumer-owned independent fallbacks, the asset-keyed reference carrying a stored fallback, and asset ownership of the
reference itself. All three reversals and their reasoning are recorded in
`docs/DESIGN_NOTES.md` §1.3 and §2.1.

## State model

The canonical collections are:

    Assets               collections.Map[string, types.Asset]
    SettlementPlans      collections.Map[string, types.SettlementPlan]
    ResolutionRecords    collections.Map[collections.Pair[string, uint64], types.ResolutionRecord]
    EmergencyMandate     collections.Item[types.EmergencyMandate]
    EmergencySuspensions collections.KeySet[string]
    PricedLiveVersion    collections.Item[uint64]

`Asset` contains:

    message Asset {
      string denom = 1;
      cosmos.bank.v1beta1.Metadata metadata = 2;
      AssetStatus status = 3;
      uint64 version = 4;
    }

The denomination is immutable after registration, and it is also the key of the feed that prices the asset, so the
record stores no separate price source: there is nothing to name and nothing to keep consistent. Metadata is stored
rather than derived on read, so an exported genesis describes itself and Bank's record and the registry's cannot drift
— but it is validated against the derivation, so the stored copy is the derivation or the state is invalid.

The record carries no pending-transition state. Every status change is a governance act that verifies its own feed
precondition and takes effect in the block it executes, so there is nothing to mark as in progress and no automatic
path that can move an asset between blocks. Registration is one of those acts, which is why there is no status between
it and admission.

Feed membership is not asset state. `x/oracle` owns the `Feeds` registry — the materialized active set, its version,
and immutable per-feed transition records — and governance moves it through `MsgAddFeed` and `MsgRemoveFeed`. The two
registries share one key namespace, and the feed set is deliberately the larger of the two: a feed must already be
Active before an asset may activate under its denomination, so every feed spends a window un-referenced, and some are
priced permanently without ever being listed. Registration itself asks nothing of the feed, so the two registries may
be populated in either order.

`SettlementPlan` contains a positive NOAH-per-asset rate, an activation height, and a mandatory earliest closing
height. Every term is set when the plan opens and fixed thereafter; there is no amendment. The closing height is the
commitment to holders, and `WriteOffAsset` is the one message refused before it. It is authoritative only while attached to a `SUSPENDED` asset.
Settlement mutations advance the owning asset's version.

`ResolutionRecords` are immutable and keyed by denom and asset version. Each record carries a kind:

    WRITE_OFF            emergency derecognition of all recognized obligation
    RETIREMENT_RESIDUAL  derecognition of bounded residual supply at final retirement

A record stores the supply outstanding at resolution and, when a settlement plan existed, its final terms. A later
recovery never erases or reinterprets an earlier record.

## Version and concurrency semantics

`version` exists so that a governance proposal drafted against one asset configuration cannot silently act on another.
Rules:

- `RegisterAsset` stores version 1. Every successful governance mutation of the asset advances the version by one:
  metadata replacement, every status transition message, settlement open, cancel, and close, and write-off.
- Nothing else advances it. There is no automatic transition, so the version moves only when a governance message
  lands, which keeps `expected_version` fully predictable for proposal authors.
- Status preconditions are the authoritative safety check on every operation. `expected_version` is defense in depth
  against stale proposals, not the primary guard. Every operation must therefore state its required status explicitly.
- Committee actions under the Asset Emergency Mandate are governance-equivalent mutations: they advance the asset
  version. Committee messages carry the mandate term instead of `expected_version`; staleness protection moves from the
  asset object to the authority object.

## Five-state lifecycle

Only these economic states are persisted:

    ACTIVE
    ISSUANCE_HALTED
    SUSPENDED
    WRITTEN_OFF
    RETIRED

The primary flow is:

    ACTIVE -> ISSUANCE_HALTED -> RETIRED
       ^            |
       +------------+ ResumeIssuance

There is no state between registration and admission. `RegisterAsset` demands the asset's feed and stores `ACTIVE` at
version 1, so an asset exists exactly when it is listed. A status that waited for the feed would hold nothing:
admission never demanded a *rate*, only a feed, so `ACTIVE` without a rate is a state the chain reaches whenever a feed
is young or has gone stale, and consumers handle both identically.

The feed must already be Active, so a feed addition and the registration against it never share a proposal: governance
proposes the feed, watches it print rates, then proposes the registration. Proving the feed first is the admission rule
rather than a way of composing proposals, and the cycle it costs falls where it is cheapest — registration is the one
admission path with no holders waiting on it, and the one where the price data is least proven, since the asset goes
convertible on the first rate its feed ever produces.

Emergency resolution is:

    ACTIVE or ISSUANCE_HALTED -> SUSPENDED -> WRITTEN_OFF
                                     |              |
                                     +<-------------+ reinstate (recovery or settlement)
                                     |              |
                                     |              +-> RETIRED (finalize, residual permitted)
                                     |
                                     +-> ISSUANCE_HALTED (RecoverAsset)
                                     |
                                     +-> RETIRED (zero supply)

### ACTIVE

- Normal policy-dependent issuance, conversion, valuation, liability accounting, and redemption are allowed.
- The registry record and Bank metadata exist, both derived from the denomination and neither replaceable.
- The asset's feed was Active when the asset was admitted, and the asset pins that feed for as long as it stays
  oracle-priced. Whether a rate has arrived yet, and whether it is currently fresh, are use-time questions each
  consumer answers for itself — Active is not a promise that a rate exists, since a feed carries none for the block it
  activates in.

### ISSUANCE_HALTED

- New issuance stops immediately.
- Existing supply remains transferable.
- Ordinary asset-to-NOAH redemption, Oracle pricing, and liability accounting remain available.
- Market may consume the asset but cannot produce more of it.
- The status carries no intent. An asset winding down toward retirement and an asset that has just recovered from
  suspension are both `ISSUANCE_HALTED`; the difference lives in proposal history, not in status.
- It is never produced by an emergency act. Entering it always took a governance vote, so the
  status cannot mean "the committee smelled smoke" — which is what keeps a wind-down halt from reading as a distress
  signal and starting the run it was never meant to announce.
- Governance may return to `ACTIVE` via `ResumeIssuance`; the status precondition is the whole guard.

### SUSPENDED

- Issuance, ordinary Market conversion, and ordinary protocol redemption are disabled.
- Existing Bank balances and transfers remain unchanged.
- A still-stored Oracle rate is not trusted for ordinary economic operation.
- Without a settlement plan, outstanding supply is disclosed as untrusted exposure.
- With a settlement plan, only fixed one-way asset-to-NOAH redemption is available.
- The asset's feed keeps running: the chain still observes and stores that denomination's rate, which may even be
  denominating the protocol reference, and the status gate each consumer applies — Market refusing the asset either
  side of a conversion, Treasury dropping it from oracle-priced membership, the asset queries withholding its rate — is
  the containment, not the absence of a rate.
- Recovery completes into `ISSUANCE_HALTED`, never directly into issuance-enabled `ACTIVE`.

### WRITTEN_OFF

- Outstanding supply may remain non-zero.
- Governance recognizes no current protocol redemption obligation.
- Issuance, conversion, Oracle pricing, and redemption are disabled.
- Supply is excluded from recognized liability but disclosed separately.
- Transfers remain available unless a separate Bank-level freeze is introduced.
- Reinstatement returns the asset to `SUSPENDED`. Final retirement is available directly.

### RETIRED

- No recognized obligation exists. Residual supply may remain when a matching `WRITE_OFF` or `RETIREMENT_RESIDUAL`
  resolution record discloses it; transfers of residual supply remain possible.
- No settlement plan remains.
- The feed keyed by the tombstone's denomination may still exist and may still be priced; a retired asset simply makes
  no claim on it.
- The registry record remains as an immutable denomination tombstone.
- `RETIRED` is terminal. No message leads out of it, and the denomination is spent permanently: the tombstone keeps
  both the registry entry and the Bank metadata, each of which registration refuses to collide with. A successor asset
  uses a new denomination.

## Settlement state

Settlement is derived from `SettlementPlans.Has(denom)`, not from `Asset.status`.

| Status           | Plan    | Meaning                                           |
| ---------------- | ------- | ------------------------------------------------- |
| `SUSPENDED`      | absent  | untrusted suspended exposure                      |
| `SUSPENDED`      | present | fixed settlement available at or after activation |
| any other status | present | invalid                                           |

Every plan carries a mandatory earliest closing height, and exactly one message reads it: `WriteOffAsset`, which is
refused before it. That is the whole guarantee, and it is sayable in one line — once your redemption opens, nothing can
derecognize you before the announced height.

The window gates only write-off because write-off is the only act that can leave a holder with nothing. The other
plan-ending paths need no gate to be safe. `CancelSettlement` runs strictly before activation, when no holder could
have redeemed and none was relying on a live window. `RecoverAsset` closes the plan by restoring the ordinary exit the
settlement was substituting for. `FinalizeRetirement` can only reach a plan on a `SUSPENDED` asset, and suspended
retirement already demands zero supply, so an attached plan is necessarily one nobody can still redeem against.

A committed window never threatens the system: supply cannot grow while an asset is suspended and the rate is fixed, so
the plan's maximum NOAH entitlement was knowable when it opened. The window's length affects the timing of redemptions,
never the bound — which is why no emergency brake inside it is needed.

## Lifecycle operations

### RegisterAsset

- Validate native denomination and Bank metadata.
- Reject `anoah` and any previously registered denom, including a tombstone.
- Require zero existing Bank supply and no pre-existing Bank denom metadata for the denomination, so an asset starts
  at zero by construction rather than by assumption.
- Require the feed keyed by the denomination in phase Active — the same rule recovery and genesis import answer to.
  Adding, Removing, and absent feeds are all refused: an asset admitted against any of them has no running price
  source to be admitted against. `MsgAddFeed` and `MsgRegisterAsset` therefore cannot share a proposal; governance
  proposes the feed, watches it print rates, and proposes the registration in a second cycle. That cost falls where it
  is cheapest — registration is the one admission path with no holders waiting on it, and the one where the price data
  is least proven, since the asset goes convertible on the first rate its feed ever produces.
- Check the feed last of the preconditions, so a denomination already spoken for is reported as the collision it is. A
  re-registration whose feed has since been removed is an identity conflict first; naming the feed would send
  governance to fix the wrong thing.
- Take the denomination as the whole input and derive the Bank metadata from it. Every field is a function of the
  denom, so a proposal supplies no value that could be wrong, and there is nothing for a later message to correct —
  which is why no amendment message exists. Deriving also closes the gap a supplied record left open: a denomination
  and a metadata that disagreed, or two assets described in different styles.
- Store `ACTIVE` at version 1, which pins the feed from this block.
- Set Bank metadata as its sole owner without minting supply or granting mint authority.

### HaltIssuance

- Require `ACTIVE` and expected version.
- Move immediately to `ISSUANCE_HALTED`.
- Treat this as the issuance cutoff while preserving pricing, liability accounting, transfers, and redemption.
- A governance act only, never an emergency one. Halting contains nothing in distress: the exit leg keeps converting
  at the full oracle rate, so the only crisis effect is a run-starting signal through a door the halt holds open.
  Distress response is `SuspendAsset`. Halting is reserved for the cases where preserving every exit is the point — a
  deliberate wind-down, or an inflow-side problem such as a mint-path exploit — and none is minutes-scale.

### ResumeIssuance

- Require `ISSUANCE_HALTED` and expected version.
- Return to `ACTIVE` without changing Oracle, Market, or Treasury policy.

### FinalizeRetirement

Every path requires expected version and ends in `RETIRED`. A settlement plan does not block retirement and is closed
on the way out: plans exist only on `SUSPENDED` assets, and suspended retirement already demands zero supply, so an
attached plan is necessarily one nobody can still redeem against. Requiring a separate closure first would force
recovering a fully settled asset purely to clear the record — the sham operation this design forbids.

There is no path here for a registration governance regrets. Registration admits the asset outright, so an unwanted one
unwinds the way every other asset does — halted first, so redemption stays open across the decision — and the Bank
metadata a registration writes is permanent whichever path ends it, so no cancellation could have returned the
denomination anyway.

- From `ISSUANCE_HALTED`: the message carries `max_residual_supply`. Require current supply at or below that bound.
  Redemption has been continuously available in this status, so governance may judge the remainder unredeemable and
  derecognize it explicitly. Move directly to `RETIRED`: retirement is immediate on every path, because feed membership
  is not asset state, so nothing is scheduled and no status lingers waiting for an epoch. On entering `RETIRED` with
  positive supply, append a `RETIREMENT_RESIDUAL` record with the actual residual.
- From `SUSPENDED`: require zero supply and move directly to `RETIRED`. Holders of a
  suspended asset may have had no exit, so positive residual from `SUSPENDED` must go through `WriteOffAsset`, which
  names the derecognition honestly.
- From `WRITTEN_OFF`: move directly to `RETIRED`. Residual supply is permitted; its
  derecognition is already recorded by the `WRITE_OFF` record, and no new record is appended.

IBC-escrowed supply counts toward outstanding supply: tokens stranded on a dead
counterparty chain are residual exactly like lost wallets, and the residual bound is how governance closes the books on
them.

### SuspendAsset

- From `ACTIVE` or `ISSUANCE_HALTED`, immediately move to `SUSPENDED`.
- Never touches `ReferenceState`: the reference names a feed, not an asset, so an asset carrying the reference
  denomination suspends like any other while its feed keeps denominating. Suspension is a pure status move.
- Also executable by the emergency committee under a live mandate (see Asset Emergency Mandate).
- Stop issuance, ordinary conversion, and ordinary redemption immediately.
- Preserve balances, transfers, metadata, and liability disclosure.
- Leave the asset's feed untouched: the rate keeps arriving and keeps serving every reader that is not this asset's
  economics, and containment is the status gate.

### OpenSettlement

- Require `SUSPENDED` or `WRITTEN_OFF`, positive supply, expected version, and Market settlement eligibility.
- A written-off asset becomes `SUSPENDED`, re-recognizing its exposure.
- Store a positive fixed NOAH-per-asset rate, an activation height of `Params.SettlementActivationDelayBlocks` past the
  opening height, and an earliest closing height after activation. The closing height is mandatory: a plan without one
  could be written off the block after it activates, so the guarantee would be opt-in by the same governance that might
  want to skip it.
- The closing height is the only height governance states. Activation is derived rather than proposed, because the
  delay is a correction window only governance draws on: every block before activation is one where a holder sits in a
  suspended asset, unable to redeem, against a plan that can still be cancelled, so a longer-than-required activation
  is a cost borne entirely by holders for governance's benefit. Deriving it also removes a way for a passing proposal
  to fail — an absolute height fixed at drafting time falls inside the delay if the vote runs long, and the settlement
  is then refused at execution.
- Every term is fixed for the plan's life. There is no amendment: a mutable rate would make the maximum entitlement
  unknowable at commit time, which is the surprise the absence of an emergency brake depends on not existing, and it
  would make redemption timing strategic — holders would wait for a better number instead of redeeming.
- The activation delay is the designed correction window: a mistaken plan is cancelled before it activates, because once
  activated a plan cannot be cancelled at all.
- The delay is a governance parameter rather than a constant, because the window it must outlast is one: its only
  meaning is relative to how long a correcting proposal currently takes to land, and x/gov's voting period moves without
  consulting x/asset. It defaults to a day of blocks, is refused at zero, and is capped at a chain year — past which it
  stops being a delay and becomes a standing ban on opening settlement. The delay is read when a plan is written and
  never again, so changing it never reaches terms holders have already been shown.
- Settlement creation never adds an Oracle rate or permits issuance.

### CancelSettlement

- Require `SUSPENDED`, expected version, an existing plan, and that the plan has not activated.
- Remove the plan and remain `SUSPENDED`, emitting a cancellation.
- This is the whole of the correction window. From the activation height onward there is nothing here to withdraw: the
  plan ends only through `RecoverAsset`, `FinalizeRetirement`, or a `WriteOffAsset` past the announced closing
  height.

### RecoverAsset

- Require `SUSPENDED` or `WRITTEN_OFF` and expected version.
- Close any settlement plan in the same act, without consulting the announced window. The plan's fixed rate cannot
  coexist with the live Oracle rate now governing conversion, and holders are not losing an exit: they are getting the
  ordinary one back, which is the outcome the settlement substituted for.
- Require the asset's feed in phase Active, the same rule registration and genesis import answer to. Recovery is a
  referent-creating path: the feed may have been legitimately removed while the asset was suspended, and this
  transition takes effect immediately.
- Move to `ISSUANCE_HALTED` in the block the transition executes, restoring ordinary pricing and redemption without
  restoring issuance. Governance may later use `ResumeIssuance` to return to `ACTIVE`. The two-step is not caution for
  its own sake: restoring pricing and permitting new exposure are different bets on different evidence, a halted asset
  has a bounded holder set so a premature recovery is re-suspendable with the exposure unchanged, and recovered assets
  sharing the status with winding-down ones is what keeps `ISSUANCE_HALTED` free of intent. Land recovery in `ACTIVE`
  and the halt has one population left, which makes it a distress signal.
- Never compare the plan's redemption rate against the live one. They are not the same kind of quantity: a settlement
  rate is a number governance froze at some past block, so above the live rate it overpays and dilutes NOAH holders to
  do it, and below the live rate it shortchanges the settling holder. It is the degraded instrument whichever way it
  currently points, which is the whole reason recovery ends it. Gating on the frozen number winning would hold holders
  in the substitute to protect a figure that was never the asset's worth, and deny them the optionality recovery
  restores — hold, transfer, or convert either direction, rather than one one-way exit.
- Recovery does not arm and wait. Governance watches the feed and satisfies itself the price is stable before proposing,
  so the decision and its effect describe the same evidence; an armed recovery would instead fire on whatever rate
  happened to arrive afterwards, moving the asset into Treasury's liability accounting and Market's convertible set on a
  price nobody voted on. Freshness stays a use-time concern: Market and Treasury already reject or omit a rate past its
  maximum age, matching x/oracle's own eligibility rule for naming the protocol reference denomination.

### WriteOffAsset

- Require `SUSPENDED`, expected version, and explicit governance resolution.
- Require the announced earliest closing height to have been reached. This is the only message the window constrains,
  and the check subsumes the not-yet-activated case because a closing height always falls after activation: governance
  that wants to derecognize before holders could ever redeem cancels the plan first.
- Close and remove any settlement plan, emitting its full terms.
- Append a `WRITE_OFF` resolution record with current supply and any final settlement terms.
- Move to `WRITTEN_OFF` without burning, transferring, or repricing balances.
- Never trigger write-off from elapsed time alone.

### Terminality

There is no message out of `RETIRED`, and the `ReactivateAsset` RPC that once provided one is removed with its name
burned. An asset that might return does not need a way back, because the lifecycle already carries two reversible
pairs: `HaltIssuance`/`ResumeIssuance` for a pause, and `SuspendAsset`/`RecoverAsset` for distress, the latter reaching
back even from `WRITTEN_OFF`. Staying halted costs nothing and gives up nothing — pricing, redemption, and liability
accounting all continue, and only new issuance stops — so retirement is what governance reaches for when none of that
is wanted any longer, and an undo would only blur the one act that means finished.

The comeback this replaced was gated on zero current supply, which could never have served more than the clean
wind-down anyway. Nothing burns a retired asset's residual — Market refuses to price it and no module burns holder
balances — so a retirement that derecognized anything was already permanent, and the tombstones an undo could still
reopen were exactly the ones holding no exposure worth reopening.

Terminality also disposes of the relist hazards a comeback carried. Market rate overrides survive retirement as inert
dust, and a relist under the same denomination would have had to review them rather than silently inherit them: a
stale downward override resurrecting on a relisted asset was the one dangling-state hazard with teeth. A successor
under a new denomination inherits none of it.

### Feed epoch activation

After consuming reports produced against the old epoch, `x/oracle` promotes every batch whose activation height has
arrived, advancing the feed version once per batch and pruning the rates of removed feeds in the same call.

Promotion touches no asset. That is a consequence of the decoupling rather than an omission: feed membership and asset
lifecycle are separate clocks, so a feed leaving the active set never retires the asset sharing its denomination, and
a suspended asset whose feed is removed simply stays suspended until governance adds one back. The removal guard is
what keeps the two coherent — a feed an oracle-priced asset needs cannot be scheduled for removal in the first place, so
the only feeds that ever leave are ones no live asset depends on.

Vote accounting stays unconditional across target changes: a newly activated target is graded like any established one,
with no grace keyed to first aggregation. Adding a target before every sidecar can price it is an operational reality,
but the slack lives in layers that already exist, not in a scoring exemption. The sidecar prices scheduled targets
throughout their pending window, so a capable fleet aggregates a new target from its first active block; pricing any one
target keeps a validator fully attended; the functioning-block threshold exempts correlated incapacity, since a majority
unable to price grades nobody; and the windowed attendance ratio — default 5% of a week-long window, judged only at
settlement — leaves a lagging operator most of a week after activation to deploy provider support. The accepted residual
is a validator attending almost nothing for the better part of a window while a majority prices: it is jailed at
settlement by design, which also restores quorum by shrinking total power toward the capable share.
`MinAttendancePerWindow` is read live at settlement, so governance can zero it before a settlement fires if a rollout
goes wrong fleet-wide. This protection is parameter-shaped: the leniency of the attendance defaults is the defense
against correlated jailing, and tightening the window or ratio shrinks the recovery span — `(1 - ratio) × window` — so
any such change must be weighed against sidecar rollout lag, not just individual hygiene.

## Asset Emergency Mandate

The treasury mandate pattern applied to its pause-only guardian archetype: a governance-appointed threshold-multisig
committee that can remove capabilities in minutes and restore nothing.

`EmergencyMandate` contains exactly the shared envelope: a chain-derived monotonic term, the exact committee address (an
ordinary threshold-multisig account appointed by governance), and a half-open activation and expiry height window. An
empty committee is the canonical disabled mandate while retaining the latest term. `SetEmergencyMandate` is a governance
message; every replacement advances the term, and governance may replace or disable the mandate at any time. Mandate
mutations never touch asset versions.

There is deliberately no numeric per-term action allowance. A count bounds nothing the per-asset rule below does not
already bound, and it fails closed in exactly the correlated-failure case the mandate exists to serve: an exhausted
mandate routes to the governance path, paying suspension latency in unbounded NOAH dilution at the worst possible
moment. Governance bounds the committee through the expiry window and through replacement, not through a counter.

Per-term suspension usage is separate consensus state, not a mandate field. With a single power there is no action
dimension to key on, so the denomination is the whole record:

    EmergencySuspensions collections.KeySet[string]

`SetEmergencyMandate` clears it. Because every replacement advances the term, clearing on replacement is exactly
term-scoping, so no term needs to be stored alongside each entry and no counter can drift from the set it summarizes.

The term, committee, and window form the shared mandate envelope: one `ark.mandate.v1` proto message embedded by this
mandate and by Treasury's Claims Mandate, with validation, window-activity, term, and disabled checks in `pkg/mandate`.
Envelope semantics — term monotonicity, half-open windows, empty committee as disabled — are defined once and cannot
drift between modules. The envelope is a shared type and helper library, deliberately not a module, and that is the
permanent boundary: allowances, usage, and every power stay domain-owned.

The committee power is exactly one, capability removal:

- `EmergencySuspendAsset` applies `SuspendAsset` semantics.

There is deliberately no emergency halt. Suspension is the only act with containment value, because ordinary conversion
is the contagion channel and a halt leaves it wide open: halting blocks only the ask leg, and in a depeg the flow that
matters is the exit, which a halted asset serves exactly as fast as an active one. An emergency halt would therefore pay
the full suspension latency — every block of it priced in unbounded NOAH dilution — while broadcasting a
committee-confirmed distress signal through a door it deliberately held open. That is a run accelerant with a
first-mover advantage: early exiters leave at oracle par and late holders eat the settlement haircut. The error
asymmetry settles it. A wrong suspension is recovered by ordinary governance and moves no value; a wrong halt is paid in
dilution that nothing returns, so the committee's single tool must be the one whose failure mode is reversible. Issuance
halts are wind-down policy, and policy runs at governance speed.

Execution rules:

- A committee message carries the denom and the exact current term, and executes only inside the mandate window.
  Committee actions skip `expected_version`; the term is the staleness guard, and status preconditions still apply. A
  version mismatch discovered after mustering the committee would be the worst possible failure mode of the emergency
  path.
- Committee actions execute immediately. There is deliberately no cancellation period: the Claims delay window protects
  irreversible value outflow, while suspension moves no value and is reversible through ordinary recovery. Governance's
  protection is after the fact — recover the asset and replace the mandate.
- Each asset admits at most one suspension per term. Re-suspending an asset governance has recovered within the same
  term is griefing, and requires governance. This rule is the mandate's binding abuse bound: without it a committee
  re-suspends faster than governance can recover, and governance cannot win that race.
- Suspension never touches the protocol reference: the reference names a feed, and the feed keeps running through any
  asset's suspension. No committee action reads or writes `ReferenceState`.
- Committee actions are governance-equivalent mutations: they advance the asset version and emit full audit events
  including the term.

The committee cannot recover, resume issuance, open, cancel, or close settlement, write off, retire, register,
change metadata, schedule feed transitions, choose the protocol reference, or modify any mandate. Every
restoration path is ordinary governance. This boundary is what makes the mandate safe: its worst abuse suspends one
healthy asset for the days governance needs to recover it and replace the committee, and no committee path can mint,
move, or re-enable anything.

## Emergency execution requirements

Suspension is the act of containment. Until it executes, ordinary conversion keeps redeeming the failing asset at its
full reference-definitional rate, so every block of latency is paid for in unbounded NOAH dilution — the one cost this
design refuses everywhere else. The latency budget is a design property, not an accident:

- The primary path is the Asset Emergency Mandate: a live committee musters its multisig threshold and suspends in
  minutes, with no governance round-trip.
- The governance path remains fully supported as the backstop and the universal authority. `SuspendAsset` must be
  executable in a single expedited proposal, in one block; it requires no reference move and no state that only a later
  block can produce. Suspension touches neither feed state nor `ReferenceState`, so neither the delayed feed-epoch
  machinery nor a reference re-point is on this path.
- The governance path is the only path when the mandate is disabled, expired, or exhausted, when an asset needs a
  second action of the same kind within a term, and for
  everything with terms attached: settlement, write-off, recovery, and resolution.
- Committee operations are part of the design: a mandate whose committee cannot muster its threshold within minutes at
  any hour is theater. Membership, threshold, and rotation are governance choices made with that test in mind.
- Detection remains additive to every path. Monitoring for oracle vote dispersion, unavailable-target quorum events, and
  one-directional conversion flow is required operational work.
- An early-tally modification to expedited governance — passing once the outcome is mathematically decided — remains
  optional future work; the mandate removes its urgency.
- A required integration scenario exercises the full emergency path as one proposal: suspend the asset in a single
  message, with `ReferenceState` untouched and no rebase on the path, and verify issuance and ordinary redemption stop
  in that block.

## Protocol reference

> **Amendment (2026-08-01):** the protocol reference moved to `x/oracle` and is a plain `reference_denom` string rather
> than a `ReferenceState` message. Every rule it has is a feed rule — eligibility is a feed phase, identity is the
> feed-key rule, the rebase preconditions are rate freshness — so the feed registry owns it. The semantics below are
> unchanged; read `x/asset` as `x/oracle` throughout this section, `ReferenceState` as the stored reference denom, and
> the referent guard as `x/oracle` checking its own state before consulting consumer guards. Reasoning is recorded in
> `docs/DESIGN_NOTES.md` §1.3 and §2.1.

The protocol reference names the single reference denomination shared by Market's base pool and Treasury's reference
tax cap. It is governance-set state, not an index maintained by consumers — and what it names is a feed, not
an asset. The reference is a unit of account, and nothing either consumer does requires that unit to be a tradable
asset: Market needs the reference rate to size and maintain the pool, Treasury needs it to convert the cap, and both
are feed reads. Suspending or retiring a stablecoin that happens to carry the reference denomination therefore never
touches the reference; the feed keeps running and the unit keeps denominating.

At launch no asset is listed against the reference denomination at all: the registry ships without it, so the unit of
account is a pure feed. That makes the separation structural rather than merely permitted — there is no claim whose
failure could be mistaken for the unit's, and nothing in the asset lifecycle can reach the reference even by accident.
The rules below still hold for a reference later re-pointed at a denomination some asset does carry.

Rules:

- The named feed must be in phase Active when governance sets it. Adding-phase feeds have no rate yet; Removing-phase
  feeds are excluded so naming the reference can never race an in-flight removal. Freshness is not an eligibility
  predicate — it is a use-time concern, load-bearing only as a precondition of the rebase — so genesis may import a
  reference before the chain has aggregated a single rate.
- Feed phase is the entire eligibility rule. The asset registry is never consulted: the reference may name a
  denomination no asset carries, and an asset that does carry it may be `SUSPENDED`, `WRITTEN_OFF`, or
  `RETIRED` without disturbing the unit. Peg failure of an asset and failure of the unit's price data are different
  axes with different responses, and fusing them was the mistake the feed-keyed reference corrects.
- `MsgSetReferenceDenom` is the only mutation, and re-pointing is always an explicit governance act. There is no automatic
  promotion on feed failure, and no stored fallback: with promotion explicit, the proposal names the successor, and a
  stored contingency would be state nothing reads. An automatic trigger was rejected as attacker-facing — whatever
  wedges the reference aggregate would force a unit-of-account move at a chosen moment — and as flap-prone, buying
  only governance latency off a halt that is already safe.
- Reference-feed failure is a liveness event, not a bleed. Every Noah conversion prices through the reference feed, so
  a stale reference rate fails all Noah conversion closed chain-wide, while stablecoin cross conversion between fresh
  pairs continues and Treasury's cap rebuild skips and retries. That fail-closed halt is the designed response.
  Recovery is one `MsgSetReferenceDenom` proposal naming any Active feed with a fresh rate. In a common-mode failure with
  no fresh feed, no legal re-point exists and none would help; conversion stays halted until data returns.
- Changing a configured reference rebases every consumer's reference-unit state in the same transaction; a missing
  executor or executor error fails the whole action. Market's reference-unit state is the base-pool `DecCoin` (denom
  and amount) plus the pool delta, converted together. The incoming feed's rate must be fresh; the outgoing rate is
  read raw at any stored age — the single deliberate exception to fail-closed freshness, confined to this
  governance-invoked action, whose converted quantities are a spread-pressure gauge and a policy cap rather than
  holder obligations. The delta is converted, never zeroed.
- `MsgRemoveFeed` is rejected while `ReferenceState` names the feed — the stronger of the two claims `x/asset` makes
  through its referent guard, and the one checked first. Governance re-points first, then removes. No asset lifecycle
  path consults the reference: the protection lives entirely at the feed layer.
- Committee-speed re-pointing, if ever wanted, ships together with a governance-pre-approved successor slot, so the
  committee executes a unit change governance already chose; neither exists until the case does.

Consumer membership needs no index. Swap eligibility and tax-cap membership are `OraclePricedDenoms`, and liability
inclusion is every Oracle-priced asset whose status is not `RETIRED`, so nothing dangles when an asset
leaves the live set. `PricedLiveVersion` advances whenever the oracle-priced set changes, and consumers compare it each
BeginBlocker instead of scanning their own state for drift.

The superseded `AssetLock` index is deleted: `AssetLockKind` values 1 through 4, collection prefix 1, and error code 5
belonged to it and stay burned.

## Feed epochs

Feed membership lives in `x/oracle`, not here. The registry preserves:

- vote-height-based epoch selection;
- the two-height activation boundary, per transition record;
- immutable transition records, each activating at its scheduled height;
- one scheduled transition per feed, with contention scoped to that feed;
- old-epoch aggregation before promotion;
- rate pruning after promotion;
- explicit feed versioning, advancing once per activation batch;
- empty feed sets as valid protocol state.

The per-feed transition model is specified in `docs/DESIGN_NOTES.md` §1.2, including the correctness argument for multiple
transitions in flight and the consume-before-promote ordering the preblock depends on. That design was written against
`x/asset`; the mechanism shipped verbatim, re-homed to `x/oracle` and re-keyed to the feed registry's own denomination
keys, recorded in the same section.

The ABCI boundary is a single oracle keeper again, because targets never left the module that owns rates:

    type ABCIOracleKeeper interface {
        GetParams(ctx context.Context) (oracletypes.Params, error)
        SetExchangeRateWithEvent(ctx context.Context, rate oracletypes.ExchangeRate) error
        RecordVoteAccounting(...)
        GetFeeds(ctx context.Context, voteHeight int64) (oracletypes.FeedSet, error)
        AdvanceFeeds(ctx context.Context) error
    }

`AdvanceFeeds` prunes the rates of removed feeds internally, so no removed-denom list crosses a module boundary.

Feed removal is validated against referent guards: a wiring-owned, conjunctive veto set holding exactly the consumers
that exist — empty on the pre-activation chain, one `x/asset` guard from activation, basket and reserve guards with
their specs. A guard derives its answer from its consumer's own state at call time; nothing is indexed, and a guard's
lifetime equals its consumer's lifetime.

`x/asset` is one consumer with one claim on a feed. The protocol reference is not among them: it is `x/oracle`'s own
state, and `x/oracle` checks it directly rather than asking a consumer to speak for it. The asset claim pins a feed
exactly while the asset carrying its denomination is oracle-priced:
`ACTIVE` or `ISSUANCE_HALTED`. Nothing waits on a feed any more, so an asset pins from the block it is registered,
`SUSPENDED` does not (`RegisterAsset` and `RecoverAsset` re-check the phase themselves), and
`WRITTEN_OFF` and `RETIRED` do not — a
tombstone is a status, not a claim on price data, the same dust rule as Market overrides. Because the asset registry and the feed registry share one key, the
asset claim resolves as a point lookup rather than a walk of the registry. There is deliberately no transitional guard
for the params-driven swap path that exists before activation.

## Sidecar feed mapping

The sidecar continues to resolve generalized provider pairs off-chain and publishes final rates keyed by feed. For
example, an `agold` feed maps to a resolver-produced `GOLD/NOAH` route price — NOAH per one GOLD, the orientation the
store keeps (D75, D77) — with nothing inverted at the feed boundary. Because every feed key is an Ark-native
base denomination, the sidecar needs no second namespace: route keys, the runtime feed list, and pair derivation
validate exactly what consensus validates, and a commodity feed configures like any stablecoin feed. The denomination
rule is what buys this — it was chosen so that the off-chain and on-chain views of a feed key can never diverge.

`Query/Feeds` exposes only the active feed set and its scheduled transitions. It does not duplicate asset metadata
because all native assets use the same exponent and the sidecar does not consume metadata. The warm-up union is the
active set plus every scheduled addition.

## Lifecycle-aware pricing

`RateSet` is the Oracle-owned in-memory set of fresh rates used for conversion. "Fresh" for consumption — valuation,
conversion, liability — means fresh by the Oracle module's freshness parameter as surfaced through `RateSet`; `x/asset`
defines no staleness policy of its own, and no lifecycle transition consults a rate at all. Transitions gate on feed
phase; freshness is left to the point of use.

There is no bundled record-plus-rate boundary between the registry and its consumers. Rates are keyed by denomination,
so `GetRateSet(ctx, denom)` needs no indirection to resolve, and each consumer already owns an admission rule the
registry cannot state for it: Market's is asymmetric across a conversion (an `ISSUANCE_HALTED` denomination may be
offered but never produced), and both Market and Treasury price through the protocol reference, which need not itself
be a listed asset. A shared view would have to be loosened past both to be usable, leaving each consumer's real gate
in place anyway. So consumers read `RateSet` directly and apply their own status gate — Market per conversion leg,
Treasury through oracle-priced membership.

Joining records to rates is a query-layer concern, where `Query/Asset` and `Query/Assets` return each asset with the
fresh rate the protocol would value it at. That join is per-denom: one unpriced member leaves its own rate unset
rather than failing the response, which is right for a reader and wrong for the fail-closed consumers above — the
second reason the two do not share one boundary.

Settlement access remains separate. A fixed governance rate must never appear in ordinary `RateSet` conversion.

## Market policy

Market owns no membership set. Every oracle-priced asset is convertible by the conversion invariant, so eligibility is
derived from the registry; what Market stores is rate policy only:

    DefaultTobinTax   (params)                      // rate applied to every member
    TobinTaxOverrides collections.Map[string, Dec]  // sparse per-denom exceptions

Tobin rates were never derivable — they are governance judgment — so a newly activated asset swaps at the default
immediately and overrides are rare governance acts. This restores Terra's original factoring: before Columbus-4 the
market module held one `tobin_tax` default plus an `illiquid_tobin_tax_list` override (MNT at 8x the 0.25% default),
and the per-denom whitelist Ark's port inherited was that migration's flattening. The override list's name is also its
sizing principle: overrides exist for markets whose plausible oracle error between updates exceeds the default — the
Tobin tax is the buffer against oracle-staleness arbitrage, so illiquid or gap-prone markets (a tokenized commodity
with weekend closures, for example) warrant a raise above the fiat default.

Override membership rules, decided here so Phase 4 inherits them:

- Setting an override requires the denom to identify a registered asset. Overrides are policy annotations on members,
  not membership, so a dangling entry is unreachable dust — but a typo means the intended protection silently does not
  exist on the real denom, which for an illiquid listing is exactly the unprotected window the override was meant to
  close. The existence check turns that silent failure into a loud proposal failure. Status is not consulted, so
  the safe listing flow composes in one proposal — add the feed, register the asset, set the override, in whichever
  order reads best, since only the override depends on the registration — and because activation is a separate
  proposal, the asset never spends a block live at the default rate.
- Retirement deliberately does not delete overrides. A cleanup hook would be an asset-lifecycle-writes-Market-state
  edge — the coupling class the `AssetLocks` deletion removed — purchased only for tidiness: derivation already makes
  an ex-member's entry unreachable on every execution path.
- Removing an override requires the entry to exist, so a typo fails rather than reporting success on a denomination
  that was never overridden, but it deliberately does not check the registry. Since retirement leaves entries behind,
  requiring membership to delete one would make the dust it creates permanently unremovable.
- The asymmetry against feed referent guards is intentional and is the rule for future per-denom policy maps (a
  Treasury tax exemption, if ever wanted): validate strictly where a dangling reference changes execution (a removed
  feed breaks live members chain-wide), tolerate inert dust where derivation guarantees it cannot be read.
- Genesis carries the overrides in both directions. Nothing re-derives them — they are the one piece of Market state
  that is pure governance judgment — so an export omitting them would silently return every exception to the default
  on the next boot. That is the only failure this map has that is quiet rather than loud, which is why it is stated
  here as a rule rather than left to the genesis implementation.

Ordinary conversion:

- offer assets require `ACTIVE` or `ISSUANCE_HALTED`;
- ask assets require `ACTIVE`;
- `ISSUANCE_HALTED` assets may be consumed but never produced;
- `SUSPENDED`, `WRITTEN_OFF`, and `RETIRED` assets are excluded;
- every issuance path rechecks status immediately before minting.

Settlement:

- requires `SUSPENDED` plus an active `SettlementPlan`;
- accepts only asset-to-NOAH redemption;
- burns the offered asset;
- draws Treasury's shared buffer proportionally;
- mints only the uncovered NOAH entitlement;
- never exposes the settlement rate to ordinary routing or reverse issuance.

Settlement is its own message and its own execution path rather than a special case of conversion, because the two
answer different questions: conversion asks what the market says a denomination is worth, settlement asks what
governance committed to pay holders of a denomination the market can no longer price honestly. It is signed by the
holder rather than by governance — it is their exit, not a policy act — and it is the plan's rate, not the oracle's,
that Treasury is handed for the buffer draw and the liability record. Passing the committed rate is what makes the
resulting accounting settlement-priced rather than a fiction built on the price that stopped being trustworthy.

The virtual pool is denominated in the protocol reference, so its unit is not Market's to set: `MsgUpdateParams` may
resize depth but rejects a denomination outright, and `RebaseBasePool` is the only path that moves it. Genesis holds
the same rule as an import check — a market genesis whose pool disagrees with the configured reference is not a
launchable configuration — because the alternative is a chain that prices conversion in one unit while Treasury caps
tax in another.

## Treasury policy

Treasury keeps no enrollment set. The transfer tax is functionally an insurance premium on protocol-convertible money:
its proceeds fund the waterfall that backs conversion redemptions, so the tax base and the redemption-liability base are
the same derived set — every oracle-priced asset. There is no stable-versus-commodity classifier, because the axis that
matters is convertible-versus-not, and by the conversion invariant every registered asset is convertible. A future
exemption, should one ever be wanted, is a sparse override in the Tobin pattern, added when the case exists.

Liability reporting is partitioned:

    priced_liability
    settlement_liability
    stale_priced_liability
    stale_member_supply[coin]
    untrusted_suspended_supply[coin]
    written_off_exposure[{outstanding_supply, write_off_version}]

- `ACTIVE` and `ISSUANCE_HALTED` supply with fresh rates is Oracle-priced.
- `SUSPENDED` supply with an active settlement is settlement-priced.
- Other `SUSPENDED` supply is explicit untrusted exposure, never zero: the asset's feed may still publish, but a
  suspended asset's rate is not trusted for its economics.
- `WRITTEN_OFF` supply is excluded from recognized liability but remains separately disclosed.
- Residual supply on `RETIRED` assets is fully derecognized and visible through asset resolution history rather than the
  liability report.
- A total covers every recognized liability exactly when `untrusted_suspended_supply` and `stale_member_supply` are both
  empty. The response carries no separate flag for this: the two lists are the answer, and `written_off_exposure` is
  deliberately not among them because a write-off extinguishes the obligation rather than leaving it unvalued.
- Policy requiring a complete total uses conservative behavior while valuation is incomplete.

The shared buffer remains proportional:

    coverage = min(buffer_noah / aggregate_recognized_liability_noah, 1)
    buffer_paid = redemption_entitlement_noah * coverage
    minted_noah = redemption_entitlement_noah - buffer_paid

When aggregate recognized liability is incomplete, Treasury preserves the buffer and Market mints the full promised
settlement entitlement. This is a deliberate choice: pausing settlement while valuation is incomplete would close the
only exit during exactly the stress that makes valuation incomplete, and incompleteness can persist indefinitely. The
buffer is the healthy system's shared capital, and a failing asset's exit must never drain it blind; full minting is
bounded by the fixed entitlement, so the cost of an orderly single-asset failure lands as bounded NOAH dilution, which
is where this design deliberately places it. One consequence is cross-asset: while any suspended asset remains
unvalued, concurrent settlements of other assets also mint in full. That is accepted for the same reason — each of those
exits is separately bounded by its own plan, so the dilution stays the sum of committed entitlements rather than
anything the incompleteness itself can widen.

## Protobuf surface

Asset protobufs:

- `proto/ark/asset/module/v1/module.proto`
- `proto/ark/asset/v1/asset.proto`
- `proto/ark/asset/v1/genesis.proto`
- `proto/ark/asset/v1/tx.proto`
- `proto/ark/asset/v1/query.proto`
- `proto/ark/asset/v1/event.proto`

`EmergencyMandate` embeds the shared envelope from `proto/ark/mandate/v1/envelope.proto`, the same message Treasury's
Claims Mandate embeds.

Governance messages:

- `RegisterAsset` (carries the denomination alone; metadata is derived)
- `HaltIssuance`
- `ResumeIssuance`
- `FinalizeRetirement` (carries `max_residual_supply`)
- `SuspendAsset`
- `OpenSettlement`
- `CancelSettlement`
- `RecoverAsset`
- `WriteOffAsset`
- `SetEmergencyMandate`

Committee messages (signed by the mandate committee, term-checked):

- `EmergencySuspendAsset`

Queries:

- `Asset`
- `Assets`, returned whole rather than paged: the registry is governance-bounded
- `SettlementPlan`
- paginated `ResolutionHistory`
- `EmergencyMandate`

Feed epochs are queried through `ark.oracle.v1 Query/Feeds`, and the protocol reference through that module's
`Query/ReferenceDenom`; both left `x/asset` with the state they read. `Asset` and `Assets` carry the fresh Oracle
exchange rate alongside each record, so a caller needs one query rather than a join across two modules that would
have to replicate the status and freshness rules to get right. The rate is unset when the protocol would not value
the asset — a status that does not admit ordinary Oracle pricing, or no fresh rate behind a status that does — and
status is answered first, so a feed still running under a suspended asset never surfaces as a price.

## Module implementation layout

    x/asset/
      keeper/
        keeper.go
        msg_server.go
        grpc_query.go
        genesis.go
        assets.go
        lifecycle.go
        settlement.go
        emergency_mandate.go
        feed_guard.go
        oracle_priced.go
        pricing.go
      types/
        asset.go
        codec.go
        errors.go
        expected_keepers.go
        genesis.go
        keys.go
        params.go
        pricing.go
        settlement.go
        emergency_mandate.go
      module/
        module.go
        depinject.go
        autocli.go
        simulation.go

The keeper needs narrow Bank access:

- `GetSupply`
- `SetDenomMetaData`
- `GetDenomMetaData` (registration precondition)

It needs Oracle `GetRateSet` for valuation. `x/oracle` does not depend on `x/asset`; the application receives both
keepers separately. `x/asset` needs no module account or mint permission.

Whenever the protocol reference moves, the keeper rebases consumer state held in reference units through narrow
executor interfaces:

    type MarketReferenceKeeper interface {
        RebaseBasePool(ctx context.Context, from string, to string) error
    }

    type TreasuryReferenceKeeper interface {
        RebaseTaxCap(ctx context.Context, from string, to string) error
    }

`x/oracle` owns the target choice and passes it — `from` and `to` are reference denominations, each keying the feed its
rate is read from — so consumers re-denominate rather than each picking a destination. `MsgSetReferenceDenom` is the only
caller.

Market and Treasury already depend on `x/oracle`, so these references are injected after construction, hooks-style, via
`SetReferenceDenomConsumers`, to avoid a dependency cycle. These are the only oracle-to-consumer edges and exist solely to
re-denominate reference-unit state atomically with a reference move.

## Genesis

Clean prelaunch genesis:

- seeds the existing eight stable assets as `ACTIVE` and Oracle-required;
- moves Bank metadata ownership into `x/asset`;
- leaves feed epochs and feed-keyed rates in `x/oracle`; feed keys are denominations by rule rather than by convention,
  so rate-store keys are unchanged;
- seeds Market's default Tobin rate and any overrides separately;
- seeds the protocol reference feed and the oracle-priced membership epoch;
- seeds the emergency mandate or its canonical disabled state, with no recorded emergency actions.

Validation requires:

- assets, settlement plans, and resolution records in deterministic unique order;
- every oracle-priced asset's feed to be in phase Active — the stricter of the two runtime admission rules — checked at
  the keeper boundary because the feed registry is `x/oracle` state. A launching chain lists its feeds in the active
  set outright and the two-block activation delay is an artefact of runtime transitions alone, so holding import to the
  stricter rule costs nothing and keeps genesis from expressing asset state the running chain could not reach. A
  suspended, written-off, or retired asset may carry a denomination whose feed does not exist yet or governance has
  since removed;
- settlement plans only on `SUSPENDED` assets;
- the current `WRITTEN_OFF` version to have a matching `WRITE_OFF` record;
- `RETIRED` assets with positive supply to have a matching `WRITE_OFF` or `RETIREMENT_RESIDUAL` record;
- the emergency mandate window and term to be internally consistent;
- every recorded emergency action to identify a registered asset and a known action kind;
- metadata, versions, rates, heights, and resolution history to validate independently.

## Application wiring

Genesis order:

    bank -> oracle -> asset -> market -> treasury

Oracle initialises before asset because asset's `InitGenesis` validates every live or waiting asset's feed against the
feed registry; the reverse order would validate against an empty one.

`x/asset` needs no BeginBlocker or EndBlocker. The Oracle application preblock pipeline:

1. selects the feed epoch for the committed vote height;
2. aggregates and stores rates in `x/oracle`;
3. records Oracle accounting;
4. advances due feed transitions after consuming the old epoch, pruning removed feeds' rates in the same call;
5. primes the Treasury liability snapshot.

x/asset has no ABCI boundary at all. Every lifecycle transition is a governance message that checks its own feed
precondition and applies in the block it executes, so no preblock hook, no rate arrival, and no elapsed height can move
an asset between statuses. Freshness stays a use-time concern: Market and Treasury reject or omit a rate past
`MaxExchangeRateAge` at the point of use, which is where a stale price actually matters.

Step 5 must follow step 4: Treasury's liability partition is enumerated from the asset registry and priced from the
block's feeds, so the primed snapshot has to see the promoted feed set.

Expected touchpoints include app dependency injection, genesis ordering, ABCI interfaces and mocks, vote extension,
proposal, aggregation, preblock, sidecar target polling, CLI queries, and Market/Treasury consumer interfaces.

## Implementation phases

The phases are repository commit sequencing for a prelaunch chain, and only the end state is a launchable
configuration. No interim protections are built for the states between commits: the params-driven swap path carries no
feed-removal guard while it waits to be replaced, and suspension's conversion containment is complete only once Market
is lifecycle-aware in Phase 4. Anything that must hold on a running chain is a completion criterion, not a
phase-boundary property.

### Phase 0: Contain the confirmed defect

Implemented, then retired: Oracle parameter updates rejected every current-target removal, including emptying a
non-empty set, until the parameter-owned membership path it contained was itself deleted. The pricefeed decoupling
moved membership behind `MsgAddFeed` and `MsgRemoveFeed`, so the defect's mechanism no longer exists. Its successor —
the coherence rule requiring a Tobin entry to name a feed in phase Active or Adding — was deleted in turn with the
Tobin parameter itself in Phase 6, once parameters carried no feed referents at all. What protects a feed today is the
consumer-side removal guard, which asks the asset registry and the protocol reference directly.

### Phase 1: Define the x/asset contract

- asset identity, metadata mutability, the six-state lifecycle, settlement, resolution records, the protocol reference,
  version semantics, and the emergency mandate with its shared envelope;
- governance messages, committee messages, queries, and events;
- direct NOAH-relative pricing and standardized exponent 18;
- gogo and Pulsar generation;
- type and genesis validation.

### Phase 2: Build x/asset standalone

- keeper collections and dependency interfaces;
- deterministic genesis import/export;
- reference, settlement, mandate, and resolution operations;
- lifecycle, message, query, and event handlers;
- `PricedAssetView` over Oracle `RateSet` (later removed unused — see "Lifecycle-aware pricing");
- focused keeper and query suites for all six statuses and their settlement combinations;
- standalone `AppModule` service and genesis registration;
- dependency-injection provider, governance AutoCLI descriptors, and simulation genesis/store-decoder support.

Production consumers remain on their current paths until replacement integration is complete.

### Phase 3: Activate x/asset

Feed epochs never move here: the pricefeed decoupling left them in `x/oracle`, so this phase is wiring rather than a
target-ownership transfer.

- wire AssetKeeper through app genesis and dependency injection;
- register the single `x/asset` referent guard with `x/oracle`, covering the asset claim (the protocol reference is
  `x/oracle`'s own state and needs no guard);
- move Bank metadata registration into `x/asset`;
- update CLI and mocks.

Reference executors stay unwired until Phases 4 and 5 supply `RebaseBasePool` and `RebaseTaxCap`. Until then a
reference *change* fails atomically, which the end-state principle accepts: genesis configures the first reference
without rebasing anything, and no interim configuration is launchable anyway.

Activation also settled what a rate-store key is. Rates are keyed by feed, and both the live write path and genesis
validation check that key against one rule: an Ark-native base denomination that is not the numeraire. A
governance-added commodity feed — the case the decoupling exists for — is keyed by the denomination it prices, so it
passes the same check a launch stablecoin's feed passes, and no feed can be admitted whose key would halt the block
that first priced it. The three-character floor in that rule also removes a latent panic on the read side: every feed
key is a valid SDK denomination, so `GetExchangeRates` can build `sdk.DecCoins` straight from the rate store without a
key that `sdk.NewDecCoinFromDec` would reject.

### Phase 4: Move Tobin ownership to Market

- add Market's default Tobin rate parameter and sparse per-denom override map;
- move Tobin taxes and conversion eligibility out of Oracle parameters;
- enforce lifecycle-aware offer, ask, issuance, and settlement rules;
- execute one-way settlement redemption;
- derive conversion eligibility from `OraclePricedDenoms` rather than a Market-held membership set;
- implement `MarketReferenceKeeper.RebaseBasePool` to re-denominate the base pool when the protocol reference moves.

Nothing in this phase touches the rate keyspace or the sidecar. `ExchangeRate.denom` says what it holds, a feed key is
a denomination, and `GetExchangeRate(ctx, denom)` is an honest signature for Market and Treasury rather than a
coincidence of denom-named launch feeds — so the indirection this phase would otherwise owe `PricedAssetView` does not
exist to build.

Market's dependency on `x/oracle` narrows to `GetRateSet`: rates are all it ever needed from there, and everything
else it was reading — Tobin rates, and membership inferred from which denominations had prices — now comes from its own
parameters and from the asset registry. Ownership moving did not delete Oracle's `TobinTaxes` parameter, its coherence
rule, or its `Query/TobinTax`: Treasury still derived its tax base from them until Phase 5, and Phase 6 removed them
once nothing read them.

Two rules the implementation settled that are worth stating plainly. The ask-side gate is stricter than the offer-side
gate — an offer may be `ISSUANCE_HALTED` while an ask must be `ACTIVE` — because halting issuance is meant to preserve
every exit while forbidding new supply, so the same status that keeps holders able to convert out must stop conversion
producing more. And `MsgUpdateParams` no longer accepts a base-pool denomination at all: the pool is denominated in the
protocol reference, so its unit moves only when governance re-points that reference, and accepting one here would let
Market re-anchor behind the reference's back and leave Treasury's cap expressed in a different unit.

Phase 4 left `SetReferenceDenomConsumers` unwired — it takes both executors and Treasury's `RebaseTaxCap` was Phase 5 work —
so a reference change failed atomically until Phase 5 wired it.

### Phase 5: Move Treasury classification ownership

- replace every liability enumeration derived from oracle params with the oracle-priced set;
- preserve priced liability and redemption throughout `ISSUANCE_HALTED`;
- partition priced, settlement-priced, untrusted-suspended, and written-off exposure;
- derive tax-cap membership from `OraclePricedDenoms` and refresh on the `PricedLiveVersion` epoch;
- implement `TreasuryReferenceKeeper.RebaseTaxCap` to re-express the cap when the protocol reference moves.

Treasury's dependency on `x/oracle` narrows to `GetRateSet`, completing what Phase 4 did for Market: after this phase
nothing outside `x/oracle` read `GetTobinTaxes`, its param, or its coherence rule, which was Phase 6's deletion gate.
Membership arrives through a Treasury-declared `AssetKeeper` interface — statuses, the oracle-priced list and its
epoch, ungated settlement plans, and the protocol reference — and the reference tax cap denomination is pinned the way
Market's pool denomination is: genesis validates it equals the configured reference, `MsgUpdateParams` rejects a denom
change by pointing at `MsgSetReferenceDenom`, and `RebaseTaxCap` is the only path that moves it.

Rules the implementation settled that are worth stating plainly:

- **Settlement-priced recognition begins at plan open, not activation.** The commitment is irrevocable from
  activation, and every path that ends a plan deletes the record, so plan-exists is exactly open-commitment. The activation delay gates execution, not
  obligation; Treasury reads the plan ungated while Market keeps its activation-gated view of who may redeem.
- **`FundStatus` always answers.** The old response hid everything behind `FailedPrecondition` while valuation was
  incomplete — blinding operators during exactly the stress that makes valuation incomplete. The partition is the
  degraded-mode answer: untrusted exposure listed by name, targets zero, balances real. The non-empty list is itself
  what says the valuation was incomplete.
- **The executor rate contract is fresh-in, raw-out, read once by `x/asset`.** Phase 4's `RebaseBasePool` read
  `GetRateSet(from, to)` — fresh on both sides — under which recovery from a stale reference feed was impossible, the
  exact scenario the spec's raw-outgoing exception exists for. Both executors now take a handed `RateSet`;
  `rebaseReference` builds it from `GetRateSet(to)` plus Oracle's `GetStoredExchangeRate(from)`, and the raw-read
  exception lives in the one module that owns the governance action. A from-unit with no stored rate at all still
  fails: a unit the chain never valued cannot be converted out of.
- **`RebaseTaxCap` does not rebuild the stored caps.** Each stored cap is the reference value already expressed in its
  own denomination — a unit-independent quantity — so re-expressing the params coin leaves them correct modulo
  truncation drift, which the weekly refresh trues up. Rebuilding in-transaction would demand fresh rates for every
  member, letting one stale unrelated feed block reference recovery.
- **The tax-cap epoch is recorded only on successful rebuild.** `TaxCapsEpoch` stores the `PricedLiveVersion` the caps
  were built against (genesis seeds it after building or validating the imported caps); a refresh skipped on stale
  rates leaves it behind so the rebuild retries every block until rates return. The per-block drift walk is deleted.
- The reference unit need not itself be oracle-priced: rates exist for any feed, so the cap converts into member units
  whether or not an asset is listed under the reference denomination, and a memberless chain simply carries no caps.

`SetReferenceDenomConsumers` is wired in app construction beside the feed referent guards, and the end-to-end re-point —
one `MsgSetReferenceDenom` re-denominating Market's pool and Treasury's cap in the same transaction, both events emitted —
is covered at the app level.

### Phase 6: Remove legacy coupling

- removed the Tobin tax parameter, its coherence rule, and its accessors from `x/oracle` once Market (Phase 4) and
  Treasury (Phase 5) stopped reading them — metadata ownership had already moved to `x/asset` at activation;
- added full lifecycle and rate-contract integration coverage, including the single-proposal emergency scenario.

Deleted proto fields were removed and renumbered rather than reserved, matching the pre-launch convention recorded in
the consolidation spec: no deployed state exists to migrate, so burning numbers would preserve compatibility with
nothing. `Params` lost `tobin_taxes` at field 5 and its tail renumbered contiguously — `attendance_window` 6→5 through
`participation_threshold` 10→9. The `TobinTax` message, the `TobinTax` and `TobinTaxes` query RPCs, and their four
request and response messages left the module with prose comments burning their names.

What the deletion settled:

- **The launch feed list needed its own home.** `DefaultFeeds` derived the launch feed set from the Tobin table — the
  last place that parameter was load-bearing rather than vestigial. `DefaultFeedDenoms` in `x/oracle/types` now
  declares those eight denominations directly and `DefaultFeeds()` takes no arguments. `MaxFeeds` survives untouched:
  it bounds the feed set across ABCI vote validation, sidecar polling, and the cached client, and only incidentally
  bounded the Tobin list.
- **`MsgUpdateParams` shrank to authority, validate, store.** The deleted coherence rule — a new Tobin entry must name
  a feed in phase Active or Adding — was the last feed referent parameters carried. Membership moves only through
  `MsgAddFeed` and `MsgRemoveFeed`, and every consumer-side referent is checked by the removal guard, so the handler
  has nothing left to cross-check. `ErrUnknownFeed` was that rule's only raiser and went with it; code 9 stays burned.
- **One benchmark axis was measuring the deleted parameter.** Market's `BenchmarkStableToStableQuote` varied "the
  number of denominations Oracle prices" by growing the Tobin list, and it was a real axis for the wrong reason: every
  quote read `Params`, and the Tobin list lived inside that record, so decoding cost grew with the priced set.
  `GetRateSet` itself is a point lookup per requested denomination. The axis now varies stored exchange rates and
  expects a flat result, kept as a regression guard against reintroducing registry-scaled work in the quote path.

Market's Tobin surface — `DefaultTobinTax`, the sparse overrides, and its own `Query/TobinTax` — is untouched: it is
the owner's, not legacy.

## Required integration scenarios

These landed as four app-level scripts driving the real application through full blocks — real vote extensions, real
preblock, real BeginBlocker — in `app/asset_retirement_test.go`, `app/asset_resolution_test.go` (suspension/settlement
plus write-off), and `app/asset_emergency_test.go`.

Each script asserts what crosses a module boundary: what Bank owns, when consensus completes a transition, what Market
permits, what Treasury recognizes, and what the feed layer releases. Single-module state-machine transitions stay with
the keeper suites, which is where their rejection matrices already live; a scenario step below annotated *keeper*
is verified there rather than through app fixtures. Two limits are worth stating rather than leaving to be discovered:
the settlement activation delay is a day of blocks, which no fixture walks, so the executable phase is reached by
bringing an announced plan forward in place and the delay itself is keeper-tested; and the fixture validator prices
every feed every block, so tests that need a starved feed withhold one denomination explicitly.

### Normal activation and retirement

1. Add the feed for an 18-decimal priced asset such as `agold` through `MsgAddFeed`; verify registration in that same
   block is rejected against the still-Adding feed and leaves no registry row, then let the transition activate and
   verify the asset registers `ACTIVE` at version 1. Verify registration also rejects a denom with no feed at all, one
   whose feed is Removing, and one with pre-existing supply or metadata.
2. Verify Bank's record is the metadata derived from `agold` — `ArkGOLD`, `arkGOLD`, display `gold` at exponent 18 — and
   that no message can rewrite it in any status.
3. Verify the asset pins its feed from the block it registers in — `MsgRemoveFeed` is vetoed from there on — and
   that its pricing verdict is feed-unavailable until the first aggregation lands.
4. Verify a validator omitting the new feed while pricing any established one stays fully attended, and one pricing
   nothing accrues eligible-but-not-attended units while blocks function (*attendance grading: `abci/oracle`*).
5. Configure Market and Treasury policy.
6. Issue non-zero supply.
7. Halt issuance and verify pricing, liability, transfers, and redemption continue.
8. Verify finalization fails with supply above `max_residual_supply`, and succeeds regardless of what the reference
   state names — retirement never consults it (*reference indifference: keeper*).
9. Redeem most supply, clear dependencies, and finalize with a residual bound covering the unredeemed dust; verify the
   asset is `RETIRED` in that block with a `RETIREMENT_RESIDUAL` record and residual balances remain transferable.
10. Remove the now-unreferenced feed through `MsgRemoveFeed`; verify the old epoch is consumed before rate pruning and
    validators stop pricing it.
11. Verify the `RETIRED` tombstone cannot be reused: the denomination is refused re-registration, and no transition
    carries the asset back out of the tombstone.

### Suspension, settlement, and recovery

1. Suspend an `ACTIVE` or `ISSUANCE_HALTED` asset in one governance message, including one carrying the reference
   denomination; verify issuance and ordinary redemption stop in that block, `ReferenceState` is untouched, and no
   rebase executor runs.
2. Preserve balances and disclose outstanding supply as untrusted exposure.
3. Consume the old epoch before pruning its rate while the asset remains `SUSPENDED` (*epoch and pruning: `x/oracle`*).
4. Open a fixed settlement with an earliest closing height and recognize its maximum NOAH entitlement — and verify the
   split the activation delay exists to create: Treasury owes the entitlement from the block the plan opens while
   Market still refuses to pay it.
5. Redeem by burning the asset, drawing the buffer proportionally, and minting only the uncovered NOAH.
6. Verify write-off is rejected at every height inside the committed window, and permitted from the closing height
   onward, closing the plan as it lands.
7. Verify cancellation is refused from the activation height onward, and that recovery closes an open plan in the same
   act without consulting the window — holders regain the ordinary exit rather than losing one.
8. Verify a fully settled asset closes its plan at zero supply without waiting for the commitment height and finalizes
   to `RETIRED` without recovery, write-off, or any oracle transition (*keeper*).
9. Verify recovery is refused on a separate suspended asset while its feed is not active.
10. Verify recovery requires the settlement closed, then recovers to `ISSUANCE_HALTED` in that block.
11. Require a separate `ResumeIssuance` before issuance resumes.

### Emergency mandate

1. Committee-suspend the asset carrying the reference denomination: verify `ReferenceState` is untouched, no executor
   runs, suspension lands, the action is recorded for the term, the asset version advances, and events carry the term.
2. Halt issuance on an ambiguous signal, then suspend the same asset later in the same term; verify the escalation is
   permitted.
3. Committee-suspend, let governance recover and resume the asset, and verify a second committee suspension of that
   asset in the same term is rejected while the governance path succeeds.
4. Verify committee actions are rejected under a disabled, expired, or not-yet-active mandate (*keeper*) and with a
   stale term. Verify mandate replacement clears recorded usage, so the same action becomes available again under the
   new term.
5. Replace the mandate; verify the term advances and messages carrying the old term are rejected.

### Write-off and reinstatement

1. Write off `SUSPENDED` supply without changing balances.
2. Keep the quantity and append-only record queryable.
3. Reinstate through settlement or Oracle recovery into `SUSPENDED`.
4. Verify historical records remain unchanged.
5. Finalize a `WRITTEN_OFF` asset directly to `RETIRED` with residual supply and verify no new record is appended — the
   derecognition was already recorded, so there is nothing left to disclose.
6. Permit final retirement once no settlement plan remains, with no reference or feed precondition on the path
   (*preconditions: keeper*).

A second write-off cycle on the same asset is covered too: reinstatement through recovery and a second write-off append
rather than replace, which is the append-only promise under the one case that could break it.

Additional cases cover tokenized commodities, registration and retirement cancellation, stale versions,
feed-removal referent guards, numeraire rejection, metadata rules, reference
eligibility, arithmetic boundaries at the documented rate band edges, stale rates, and incomplete liability.

## Live-chain upgrade variant

If deployed after a live genesis:

- add the `x/asset` store through a named upgrade;
- retain old decoders until migration completes;
- seed assets, metadata, feed versions and heights, the protocol reference, and Market and Treasury policy from
  committed state;
- import active settlements and resolution history exactly;
- verify every live or waiting asset's denomination keys a present feed;
- reject or explicitly resolve unsafe pending removals;
- delete obsolete Oracle state only after validation succeeds.

For a prelaunch chain, use a clean genesis break and avoid compatibility state.

## Verification

Run:

    make proto-format
    make proto-lint
    make proto-gen
    GOCACHE=/private/tmp/ark-gocache go test ./x/asset/...
    GOCACHE=/private/tmp/ark-gocache go test ./x/market/...
    GOCACHE=/private/tmp/ark-gocache go test ./x/treasury/...
    GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/...
    GOCACHE=/private/tmp/ark-gocache go test ./abci/...
    GOCACHE=/private/tmp/ark-gocache go test ./oracle/...
    GOCACHE=/private/tmp/ark-gocache go test ./app/...
    GOCACHE=/private/tmp/ark-gocache go test ./...
    go build ./...
    git diff --check

## Suggested commit sequence

1. Temporary Oracle target-removal containment. Completed, then retired with the params-driven target path; end-state
   protection is the `x/asset` referent guard, which arrives with the consumers it protects.
2. Contract alignment across protobuf, types, and keeper: renames, `SetAssetMetadata`, `CloseSettlement`, residual
   finalization, `ResolutionRecord`, version semantics, registration preconditions, and the emergency mandate.
3. Target ownership, ABCI, sidecar, metadata, and pricing-consumer cutover.
4. Market policy ownership and settlement execution.
5. Treasury derived tax caps and partitioned liabilities.
6. Legacy Oracle removal and full integration, including the single-proposal emergency scenario.
7. Live-chain migrations only if required.

## Completion criteria

The design is complete when:

- every asset is priced by the feed its denomination keys, carrying a NOAH-relative rate;
- no module infers asset existence or liability from Oracle parameters;
- a feed may exist with no asset carrying its denomination, and no two assets can ever share one;
- activation waits for a rate aggregated after the request;
- normal retirement preserves pricing and redemption for outstanding supply, and can close the books on a bounded,
  disclosed residual without a sham transition;
- every lifecycle state reaches `RETIRED` through an honest path;
- suspension never treats unavailable valuation as zero;
- from suspension onward, an asset's maximum remaining claim on NOAH is a closed, governance-chosen bound;
- suspension executes in a single-message proposal, with no reference move on the path;
- a live mandate suspends a failing asset in one transaction, and every mandate bound — term, window, one suspension per
  asset per term — is enforced;
- committee powers only remove capabilities: no committee path restores issuance, redemption, pricing, or recognition,
  or moves value;
- settlement is one-way, explicit, auditable, and cannot enable issuance; its window closes only after any announced
  commitment passes or supply is exhausted, and write-off is the only earlier override;
- every governance mistake outside a committed settlement window is correctable without write-off;
- recovery restores `ISSUANCE_HALTED` before issuance can resume;
- write-off, reinstatement, and residual retirement preserve balances and immutable history;
- final retirement requires no settlement plan and residual within the governance-approved bound, and consults neither
  the reference state nor the feed registry;
- feed removal remains height-correct and consumes the old epoch before pruning;
- attendance is unconditional per window: pricing any target attends a validator, correlated incapacity grades nobody,
  and sustained whole-report absence against a pricing majority jails at settlement;
- every registered asset is priced and convertible: no unpriced or unconvertible native asset can exist, and no
  denomination can be registered that could not have carried a feed;
- commodities may be priced at the feed layer without being listed; listing an asset is the act of creating a
  convertible liability;
- an asset's metadata is derived from its denomination in every status, and no message can rewrite it;
- the supported rate precision band is documented and arithmetic fails closed outside it;
- the full normal and emergency integration scenarios pass.
