# Asset Module Plan

Status: Phases 0 through 2 are implemented against an earlier draft of this contract and require alignment before Phase
3; the Phase 3 target-ownership cutover is pending.

## Purpose

Build `x/asset` as the authoritative registry and lifecycle owner for governance-managed Bank assets other than NOAH.
The module must support stablecoins, tokenized commodities such as gold, and assets that do not use the chain Oracle
without treating every registered asset as a stablecoin.

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

- Asset is the general domain name. Stablecoin is a Treasury policy classification.
- `anoah` is the native base denomination and remains outside this lifecycle.
- Every Ark-native asset defines at least an `a<display>` base unit at exponent 0 and `<display>` at exponent 18. Sorted
  intermediate units are permitted; aliases are not.
- Every Oracle-priced asset has one denomination-keyed rate expressed directly against NOAH.
- Generalized feed identifiers remain deferred until the protocol needs non-asset observations or multiple independent
  prices for one asset.
- `x/asset` owns identity, Bank metadata, economic lifecycle, Oracle-target epochs, dependency locks, settlement terms,
  resolution history, and lifecycle-aware valuation access.
- `x/oracle` owns validator reporting, aggregation, accounting, stored rates, freshness, and rate pruning. Freshness has
  exactly one definition, owned by `x/oracle`; lifecycle transitions that depend on a fresh rate consume that definition
  through `RateSet` and never define their own staleness policy.
- `x/market` owns ordinary conversion eligibility, Tobin-tax policy, settlement execution, minting, and burning.
- `x/treasury` owns stable-liability enrollment, reference-asset policy, liability reporting, and redemption-buffer
  accounting.
- Asset status describes economic treatment only, and status names must describe that treatment, never an intent or a
  predicted destiny. Oracle transitions and settlement-plan presence are separate authoritative state.
- Recovery after emergency suspension restores normal pricing and redemption into `ISSUANCE_HALTED`. Issuance resumes
  only through a separate, explicit `ResumeIssuance` transition.
- Write-off never burns, transfers, or reprices holder balances. It changes recognized protocol obligation and appends
  permanent resolution history.
- Suspension closes the only unbounded transmission channel from an asset failure into NOAH: ordinary conversion at the
  reference rate. From suspension onward the asset's maximum remaining claim on NOAH is closed — zero while unpriced, at
  most outstanding supply times the fixed rate while a settlement is open, and zero again after write-off. Recovery
  deliberately reopens ordinary pricing and redemption; that is its meaning, and it is why completion requires a fresh
  rate and full policy prerequisites.
- A bounded committee may act where only speed is needed, never where terms are chosen. The Asset Emergency Mandate
  reuses the treasury mandate pattern — monotonic term, exact threshold-multisig committee, expiring height window,
  governance replacement at any time — to execute issuance halts and suspensions in minutes, bounded by one action per
  asset per kind per term. Settlement, write-off, recovery, and every other decision with terms attached remains
  governance-only.
- Derecognition is never automatic. No timer or height threshold may write off an obligation on its own. Explicit
  governance finalization that derecognizes a bounded residual is a decision, not an automation, and is permitted.
- The denomination is immutable forever. Metadata is mutable only while the asset is `PENDING` and frozen in every other
  status. Before first activation there are no holders and no economic reliance, so pre-activation immutability protects
  nothing.
- Governance corrects its own mistakes at defined points, and holder-facing commitments bind between them. A settlement
  plan is freely cancellable before activation and closable after its announced earliest closing height; between those
  points the window is a hard commitment that only `WriteOffAsset` overrides. Assurance to redeeming holders outweighs
  mid-window correctability by design, so the correction window before activation must be real: plans activate only
  after a minimum delay.

## Scope boundaries

Version 1 does not provide:

- generalized on-chain feed identifiers separate from asset denoms;
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
- any systemic backstop for NOAH itself: the emergency lifecycle contains individual asset failures, and no mechanism in
  this plan attempts to contain a loss of confidence in the base denomination;
- live-chain compatibility unless deployment requires an upgrade from committed state.

Provider markets and resolver routes remain generalized off-chain. The sidecar may combine inputs such as `NOAH/USD` and
`USD/XAU`, but consensus receives one final denomination-specific NOAH-relative rate.

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

A governance settlement uses a different contract:

    redemption_rate[denom] = NOAH paid per unit of the settled asset
    output anoah = floor(input asset amount * redemption_rate)

The fixed settlement rate is a one-way entitlement while its plan is active. It is not an Oracle observation, must not
enter ordinary conversion, and never permits asset issuance. Rounding down prevents payout amplification through
transaction splitting, and zero-output redemptions fail. The entitlement is prospective, not perpetual: governance may
close the plan once its announced window has run or supply is exhausted, and executed redemptions always stand.

## Ownership model

### x/asset

Owns:

- asset registration and Bank metadata;
- asset economic status and version;
- whether normal operation requires Oracle pricing;
- active and pending Oracle-target epochs;
- `AssetLocks`;
- active `SettlementPlans`;
- append-only `ResolutionRecords`;
- the `EmergencyMandate`, per-term `EmergencyActions` usage, and the committee execution paths;
- lifecycle-aware priced-asset and retirement-readiness views.

Does not own:

- validator reports, rate aggregation, stored rates, or freshness;
- Market curves, Tobin taxes, minting, or burning;
- Treasury stable-liability enrollment or buffer balances;
- emergency reference fallbacks, which belong to the consumer that owns the reference being replaced.

### x/oracle

Owns denomination-keyed reporting, aggregation, accounting, rate storage, freshness, and pruning. It does not decide
whether an asset may be issued, redeemed, settled, suspended, written off, or retired.

### x/market

Owns supported converter assets, per-asset conversion policy, the base-pool reference, the base-pool emergency fallback,
and execution of ordinary and settlement redemption. It consumes lifecycle-checked views from `x/asset`.

### x/treasury

Owns stable enrollment, reference-tax-cap policy, the reference-tax-cap emergency fallback, liability classification,
redemption accounting, and the shared buffer. It enumerates its own stable set and never infers liabilities from Oracle
targets.

Each consumer owns the fallback for the reference it owns. One asset may hold both live-reference locks, and the right
replacement for the base pool is not necessarily the right replacement for the reference tax cap; storing one fallback
per asset in `x/asset` would force a single answer to two independent policy questions, and `x/asset` cannot judge
whether a candidate is a viable base pool.

## State model

The canonical collections are:

    Assets            collections.Map[string, types.Asset]
    AssetLocks        collections.KeySet[collections.Pair[string, types.AssetLockKind]]
    OracleTargets     collections.Item[types.OracleTargets]
    SettlementPlans   collections.Map[string, types.SettlementPlan]
    ResolutionRecords collections.Map[collections.Pair[string, uint64], types.ResolutionRecord]
    EmergencyMandate  collections.Item[types.EmergencyMandate]
    EmergencyActions  collections.KeySet[collections.Pair[string, types.EmergencyAction]]

`Asset` contains:

    message Asset {
      string denom = 1;
      cosmos.bank.v1beta1.Metadata metadata = 2;
      AssetStatus status = 3;
      uint64 version = 4;
      bool oracle_required = 5;
    }

The denomination is immutable after registration. Metadata is mutable only while the asset is `PENDING` and frozen in
every other status.

`oracle_required` describes normal pricing mode. It does not describe current target membership, Market eligibility,
Treasury classification, or settlement.

`OracleTargets` contains the complete active target set, the active target version, and a sorted list of immutable
per-denom transition records, each carrying its denom, direction, and activation vote height.

`SettlementPlan` contains a positive NOAH-per-asset rate, an activation height, and an optional earliest closing height.
The earliest closing height may be set when the plan is opened, as a commitment to holders, and may be set or raised —
never lowered or cleared — when recovery begins. While supply remains outstanding, the commitment is hard: nothing but
`WriteOffAsset` ends the plan before that height. It is authoritative only while attached to a `SUSPENDED` asset.
Settlement mutations advance the owning asset's version.

`ResolutionRecords` are immutable and keyed by denom and asset version. Each record carries a kind:

    WRITE_OFF            emergency derecognition of all recognized obligation
    RETIREMENT_RESIDUAL  derecognition of bounded residual supply at final retirement

A record stores the supply outstanding at resolution and, when a settlement plan existed, its final terms. Later
reinstatement or reactivation never erases or reinterprets an earlier record.

## Version and concurrency semantics

`version` exists so that a governance proposal drafted against one asset configuration cannot silently act on another.
Rules:

- `RegisterAsset` stores version 1. Every successful governance mutation of the asset advances the version by one:
  metadata replacement, pricing-mode change, every status transition message, settlement open, cancel, and close,
  write-off, and reactivation.
- Automatic completions never advance the version: `PENDING` to `ACTIVE` on the first fresh rate, recovery completion
  into `ISSUANCE_HALTED`, and epoch-activation transitions into `RETIRED`. This keeps `expected_version` predictable for
  proposal authors and allows pipelined proposals that intentionally target the state an automatic completion will
  produce, such as a `ResumeIssuance` proposal voted during recovery.
- Status preconditions are the authoritative safety check on every operation. `expected_version` is defense in depth
  against stale proposals, not the primary guard. Every operation must therefore state its required status explicitly.
- Committee actions under the Asset Emergency Mandate are governance-equivalent mutations: they advance the asset
  version. Committee messages carry the mandate term instead of `expected_version`; staleness protection moves from the
  asset object to the authority object.

## Six-state lifecycle

Only these economic states are persisted:

    PENDING
    ACTIVE
    ISSUANCE_HALTED
    SUSPENDED
    WRITTEN_OFF
    RETIRED

The primary flow is:

    PENDING -> ACTIVE -> ISSUANCE_HALTED -> RETIRED
       |          ^            |
       |          +------------+ ResumeIssuance
       +-------------------------> RETIRED (cancelled registration)

Emergency resolution is:

    ACTIVE or ISSUANCE_HALTED -> SUSPENDED -> WRITTEN_OFF
                                     |              |
                                     +<-------------+ reinstate (recovery or settlement)
                                     |              |
                                     |              +-> RETIRED (finalize, residual permitted)
                                     |
                                     +-> ISSUANCE_HALTED (recovery completion)
                                     |
                                     +-> RETIRED (zero supply)

### PENDING

- The registry record and Bank metadata exist.
- Metadata may be replaced.
- Normal issuance, conversion, and redemption are disabled.
- Supply must remain zero.
- An Oracle-priced asset may be off-target, awaiting target addition, targeted while awaiting its first fresh rate, or
  awaiting target removal after registration cancellation.

### ACTIVE

- Normal policy-dependent issuance, conversion, valuation, liability accounting, and redemption are allowed.
- An Oracle-required asset has active, non-removing target membership and a fresh rate.
- Live-reference and policy locks may be attached.

### ISSUANCE_HALTED

- New issuance stops immediately.
- Existing supply remains transferable.
- Ordinary asset-to-NOAH redemption, Oracle pricing, and liability accounting remain available.
- Market may consume the asset but cannot produce more of it.
- The status carries no intent. An asset winding down toward retirement and an asset that has just completed emergency
  recovery are both `ISSUANCE_HALTED`; the difference lives in proposal history, not in status.
- Governance may return to `ACTIVE` via `ResumeIssuance` only before final target removal is scheduled.
- Finalized priced retirement remains `ISSUANCE_HALTED` while target removal is pending.

### SUSPENDED

- Issuance, ordinary Market conversion, and ordinary protocol redemption are disabled.
- Existing Bank balances and transfers remain unchanged.
- A still-stored Oracle rate is not trusted for ordinary economic operation.
- Without a settlement plan, outstanding stable supply is disclosed as unpriced exposure.
- With a settlement plan, only fixed one-way asset-to-NOAH redemption is available.
- Oracle target removal, absence, addition, and fresh-rate recovery are derived from target state rather than new asset
  statuses.
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
- No `AssetLocks` or settlement plan remain.
- The asset is absent from current and pending Oracle target sets.
- The registry record remains as an immutable denomination tombstone.
- Reactivation requires zero current supply and starts a new `PENDING` flow. A tombstone with residual supply is
  permanent; a successor asset must use a new denomination.

## Derived Oracle state

For an Oracle-required asset, active membership and the asset's own scheduled transition derive the phase:

| Active set | Scheduled transition | Meaning                 |
| ---------- | -------------------- | ----------------------- |
| absent     | none                 | Oracle off              |
| absent     | addition             | target addition pending |
| present    | none                 | Oracle active           |
| present    | removal              | target removal pending  |

A denom with no scheduled transition keeps its active membership. Transitions scheduled for other denoms never affect
this denom's phase. An asset without an Oracle requirement must be absent from the active set and hold no transition.

Allowed combinations are:

| Asset status      | Oracle state                                         |
| ----------------- | ---------------------------------------------------- |
| `PENDING`         | off, adding, active awaiting first rate, or removing |
| `ACTIVE`          | active                                               |
| `ISSUANCE_HALTED` | active or removing                                   |
| `SUSPENDED`       | off, adding, active during recovery, or removing     |
| `WRITTEN_OFF`     | off or removing                                      |
| `RETIRED`         | off                                                  |

`WRITTEN_OFF` admits `removing` for the same reason `SUSPENDED` does: write-off schedules target removal and moves in
the same block rather than waiting for an epoch. Write-off is the only early exit from a committed settlement window, so
gating it behind a two-height target transition would defeat its purpose.

These descriptions may be exposed through a derived query view, but they are not additional consensus fields.

## Settlement state

Settlement is derived from `SettlementPlans.Has(denom)`, not from `Asset.status`.

| Status           | Plan    | Meaning                                           |
| ---------------- | ------- | ------------------------------------------------- |
| `SUSPENDED`      | absent  | unpriced suspended exposure                       |
| `SUSPENDED`      | present | fixed settlement available at or after activation |
| any other status | present | invalid                                           |

An activated plan ends in exactly one of two ways: `EndSettlement` or `WriteOffAsset`. Recovery completion and final
retirement both require the plan to be closed first; neither closes it implicitly. When an earliest closing height is
set, `EndSettlement` is rejected before it while any supply remains outstanding. The announced window is a hard
commitment: holders never need to race a governance vote to redeem, and `WriteOffAsset` is the only earlier exit. A
committed window never threatens the system: the plan's maximum NOAH entitlement was fixed when it opened, so the
window's length affects the timing of redemptions, never the bound.

## Lifecycle operations

### RegisterAsset

- Validate native denomination and Bank metadata.
- Reject `anoah` and any previously registered denom, including a tombstone.
- Require zero existing Bank supply and no pre-existing Bank denom metadata for the denomination, so the `PENDING`
  zero-supply invariant holds by construction rather than by assumption.
- Store `PENDING`, version 1, and the requested `oracle_required`.
- Set Bank metadata as its sole owner without minting supply or granting mint authority.

### AmendRegistration

- Require `PENDING`, expected version, and zero supply.
- Replace Bank metadata and `oracle_required` together under the same denomination and unit rules as registration.
- Never change the denomination.
- Changing the pricing mode additionally requires the denom to be absent from active and pending targets: pricing mode
  never moves while an asset has live economic obligations or an in-flight target transition. Metadata alone stays
  correctable while a scheduled target addition is pending. A `RETIRED` asset must be reactivated into `PENDING` before
  its registration may change.

### ActivateAsset

- Require `PENDING` and expected version.
- An unpriced asset moves directly to `ACTIVE`.
- A priced asset schedules target addition and remains `PENDING`.
- Target activation alone does not activate the asset.
- After the first fresh positive rate and required policy are available, complete `PENDING -> ACTIVE`.
- Leaving `PENDING` freezes metadata.

### HaltIssuance

- Require `ACTIVE` and expected version.
- Move immediately to `ISSUANCE_HALTED`.
- Treat this as the issuance cutoff while preserving pricing, liability accounting, transfers, and redemption.
- Also executable by the emergency committee under a live mandate (see Asset Emergency Mandate).

### ResumeIssuance

- Require `ISSUANCE_HALTED`, expected version, and no target removal pending.
- Return to `ACTIVE` without changing Oracle, Market, or Treasury policy.

### FinalizeRetirement

Every path requires expected version, no locks, and no settlement plan, and every path ends in `RETIRED`.

- From `PENDING`: cancel the registration at zero residual. If never targeted, move directly to `RETIRED`. If already
  targeted while awaiting a first rate, schedule removal and remain `PENDING`; removal activation moves it to `RETIRED`.
- From `ISSUANCE_HALTED`: the message carries `max_residual_supply`. Require current supply at or below that bound.
  Redemption has been continuously available in this status, so governance may judge the remainder unredeemable and
  derecognize it explicitly. From unpriced `ISSUANCE_HALTED`, move directly to `RETIRED`. From priced `ISSUANCE_HALTED`,
  schedule target removal and remain `ISSUANCE_HALTED` with redemption open; removal activation moves it to `RETIRED`.
  Supply can only shrink meanwhile, so the approved bound continues to hold. On entering `RETIRED` with positive supply,
  append a `RETIREMENT_RESIDUAL` record with the actual residual.
- From `SUSPENDED`: require zero supply and the target already absent, and move directly to `RETIRED`. Holders of a
  suspended asset may have had no exit, so positive residual from `SUSPENDED` must go through `WriteOffAsset`, which
  names the derecognition honestly.
- From `WRITTEN_OFF`: require the target absent, and move directly to `RETIRED`. Residual supply is permitted; its
  derecognition is already recorded by the `WRITE_OFF` record, and no new record is appended.

Reject conflicting target transitions. IBC-escrowed supply counts toward outstanding supply: tokens stranded on a dead
counterparty chain are residual exactly like lost wallets, and the residual bound is how governance closes the books on
them.

### SuspendAsset

- From `ACTIVE` or `ISSUANCE_HALTED`, immediately move to `SUSPENDED`.
- Require live `MARKET_BASE_POOL` and `TREASURY_REFERENCE_TAX_CAP` dependencies to have moved first — in the same
  governance proposal per Emergency execution requirements, or atomically through each consumer's pre-approved fallback.
- Also executable by the emergency committee under a live mandate (see Asset Emergency Mandate).
- Stop issuance, ordinary conversion, and ordinary redemption immediately.
- Preserve balances, transfers, metadata, policy enrollment, dormant locks, and liability disclosure.
- Schedule target removal when Oracle pricing is configured.

### CancelRecovery

- Require `SUSPENDED` and expected version.
- Leave any settlement commitment untouched; an announced earliest closing height is never lowered or cleared while its
  plan exists.
- Schedule target removal when recovery restored Oracle participation.
- Remain `SUSPENDED`; return a no-op when recovery is already cancelled.
- Require a pending target addition to activate before scheduling its removal.

### OpenSettlement

- Require `SUSPENDED` or `WRITTEN_OFF`, positive supply, expected version, and Market settlement eligibility. Target
  state is not a precondition: a settlement may be opened while recovery is underway, because the two coexist by design
  and recovery completion is already blocked while a plan exists. Opening against a recovering priced asset must
  announce an earliest closing height, for the same reason `BeginRecovery` must.
- A written-off asset becomes `SUSPENDED`, re-recognizing its exposure.
- Store a positive fixed NOAH-per-asset rate, an activation height at least `SettlementActivationDelayBlocks` in the
  future, and optionally an earliest closing height as a commitment to holders.
- The activation delay is the designed correction window: a mistaken plan is cancelled before it activates, because once
  activated a committed window cannot be closed early.
- Settlement creation never adds an Oracle rate or permits issuance.

### EndSettlement

- Require `SUSPENDED`, expected version, and an existing plan.
- Before activation, remove the never-activated plan and remain `SUSPENDED`.
- At or after activation, close prospectively. When the plan announces an earliest closing height, require that height
  to have been reached or outstanding supply to be zero. A window with no holders left protects nobody; a window with
  holders is a hard commitment.
- Remove the plan and remain `SUSPENDED`; outstanding supply reverts to unpriced suspended exposure.
- Executed redemptions stand; closure is prospective only.
- A close emits the plan's full terms and its open and close heights.
- Replacement terms require a new plan and advance the asset version.

### BeginRecovery

- Require `SUSPENDED` or `WRITTEN_OFF` and expected version.
- A written-off asset first becomes `SUSPENDED`.
- Schedule target addition when Oracle pricing is required.
- Preserve an existing settlement and set or raise its earliest closing height; a commitment is never lowered.
- Keep issuance and ordinary redemption disabled while recovery is incomplete.
- Reject conflicting target transitions.

### Recovery completion

- Require `SUSPENDED` and no active settlement plan; governance closes the plan explicitly with `EndSettlement` once its
  commitment allows, so recovery cannot complete before an announced window has run.
- For a priced asset, require active target membership and a fresh rate obtained after target restoration.
- Require Market and Treasury policy prerequisites.
- Move to `ISSUANCE_HALTED`, restoring ordinary pricing and redemption without restoring issuance.
- Governance may later use `ResumeIssuance` to return to `ACTIVE`.

### WriteOffAsset

- Require `SUSPENDED`, expected version, and explicit governance resolution.
- Close and remove any settlement plan; the override is exempt from the earliest-closing-height commitment and is the
  only way to end a committed window early.
- Append a `WRITE_OFF` resolution record with current supply and any final settlement terms.
- Move to `WRITTEN_OFF` without burning, transferring, or repricing balances.
- Never trigger write-off from elapsed time alone.

### ReactivateAsset

- Require `RETIRED`, zero current supply, and expected version.
- Move to `PENDING`, where the registration becomes mutable again through `AmendRegistration`.
- Require explicit policy reconstruction and a new target activation when priced.

### Target epoch activation

After consuming reports produced against the old epoch, for every batch whose activation height has arrived:

- promote the batch, advancing the target version once;
- keep added `PENDING` assets pending until their first fresh rate;
- keep added `SUSPENDED` assets suspended during recovery;
- move removed `PENDING` assets to `RETIRED`;
- move removed `ISSUANCE_HALTED` assets to `RETIRED`, appending a `RETIREMENT_RESIDUAL` record when residual supply
  remains;
- keep removed `SUSPENDED` assets suspended;
- return removed denoms to the application for Oracle-owned rate pruning.

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

Per-term action usage is separate consensus state, not a mandate field:

    EmergencyActions collections.KeySet[collections.Pair[string, types.EmergencyAction]]

`SetEmergencyMandate` clears it. Because every replacement advances the term, clearing on replacement is exactly
term-scoping, so no term needs to be stored alongside each entry and no counter can drift from the set it summarizes.

The term, committee, and window form the shared mandate envelope: one `ark.mandate.v1` proto message embedded by this
mandate and by Treasury's Claims Mandate, with validation, window-activity, term, and disabled checks in `pkg/mandate`.
Envelope semantics — term monotonicity, half-open windows, empty committee as disabled — are defined once and cannot
drift between modules. The envelope is a shared type and helper library, deliberately not a module, and that is the
permanent boundary: allowances, usage, fallbacks, and every power stay domain-owned.

Committee powers are exactly two, both capability removal:

- `EmergencyHaltIssuance` applies `HaltIssuance` semantics. This is the graduated first response for ambiguous signals:
  holders are unaffected and redemption continues, so a committee with a lighter option acts earlier instead of
  hesitating over the sledgehammer.
- `EmergencySuspendAsset` executes each consumer's pre-approved reference fallback and `SuspendAsset` semantics in one
  transaction.

Execution rules:

- A committee message carries the denom and the exact current term, and executes only inside the mandate window.
  Committee actions skip `expected_version`; the term is the staleness guard, and status preconditions still apply. A
  version mismatch discovered after mustering the committee would be the worst possible failure mode of the emergency
  path.
- Committee actions execute immediately. There is deliberately no cancellation period: the Claims delay window protects
  irreversible value outflow, while suspension moves no value and is reversible through ordinary recovery. Governance's
  protection is after the fact — recover the asset and replace the mandate.
- Each asset admits at most one action per kind per term. Halt-then-suspend escalation of one failing asset within a
  term is the graduated response working as intended; re-suspending an asset governance has recovered within the same
  term is griefing, and requires governance. This rule is the mandate's binding abuse bound: without it a committee
  re-suspends faster than governance can recover, and governance cannot win that race.
- Suspending an asset that holds a live reference requires the owning consumer's fallback entry. Each consumer stores
  its own fallback, moves its reference to it, and maintains its lock atomically, then the suspension applies, all in
  one transaction. Every fallback is validated at execution by its owner: it must identify a distinct `ACTIVE` asset
  able to accept that consumer's live-reference lock. An invalid or missing fallback fails the whole action atomically
  and the governance path takes over — the correct escalation for correlated failures where the pre-approved contingency
  is itself unhealthy.
- Committee actions are governance-equivalent mutations: they advance the asset version and emit full audit events
  including the term, the action, and any fallbacks applied.

The committee cannot recover, resume issuance, open, cancel, or close settlement, write off, retire, register,
reactivate, change metadata or pricing mode, schedule targets, choose references, or modify any mandate. Every
restoration path is ordinary governance. This boundary is what makes the mandate safe: its worst abuse halts one healthy
asset for the days governance needs to recover it and replace the committee, and no committee path can mint, move, or
re-enable anything.

## Emergency execution requirements

Suspension is the act of containment. Until it executes, ordinary conversion keeps redeeming the failing asset at its
full reference-definitional rate, so every block of latency is paid for in unbounded NOAH dilution — the one cost this
design refuses everywhere else. The latency budget is a design property, not an accident:

- The primary path is the Asset Emergency Mandate: a live committee musters its multisig threshold and suspends in
  minutes, with no governance round-trip.
- The governance path remains fully supported as the fallback and the universal authority. Every governance handler on
  the suspension path — moving the Market base-pool reference, moving the Treasury reference tax cap, and `SuspendAsset`
  itself — must be executable in a single multi-message expedited proposal, in order, in one block. No handler on this
  path may require state that only a later block can produce. The delayed target epoch machinery is exempt because
  suspension schedules removal rather than waiting for it.
- The governance path is the only path when the mandate is disabled, expired, or exhausted, when a fallback entry is
  missing or invalid at execution, when an asset needs a second action of the same kind within a term, and for
  everything with terms attached: settlement, write-off, recovery, and resolution.
- Committee operations are part of the design: a mandate whose committee cannot muster its threshold within minutes at
  any hour is theater. Membership, threshold, and rotation are governance choices made with that test in mind.
- Detection remains additive to every path. Monitoring for oracle vote dispersion, unavailable-target quorum events, and
  one-directional conversion flow is required operational work.
- An early-tally modification to expedited governance — passing once the outcome is mathematically decided — remains
  optional future work; the mandate removes its urgency.
- A required integration scenario exercises the full emergency path as one proposal: move both live references and
  suspend the asset in a single proposal, and verify issuance and ordinary redemption stop in that block.

## Asset locks

`AssetLock` is an inverse dependency index maintained atomically by the module that owns the underlying policy. It is
not a balance, rate, or governance-supplied label.

Initial lock kinds:

- `MARKET_ASSET_POLICY`
- `MARKET_BASE_POOL`
- `TREASURY_STABLE_POLICY`
- `TREASURY_REFERENCE_TAX_CAP`

Rules:

- Live-reference locks are accepted only by `ACTIVE` or non-finalized `ISSUANCE_HALTED` assets that are Oracle-priced
  with active target membership:
  - `MARKET_BASE_POOL`
  - `TREASURY_REFERENCE_TAX_CAP`

  Both references are inherently price-dependent — `MARKET_BASE_POOL` is what Market prices conversion through, and
  `TREASURY_REFERENCE_TAX_CAP` is the denomination Treasury converts the cap from — so an unpriced asset, or a priced
  asset without an active target, cannot serve either. Locks are reconstructed from consumer genesis rather than
  validated here on import, so `SuspendAsset` rechecks live references unconditionally as a backstop.

- Dormant policy locks may remain on `SUSPENDED` and `WRITTEN_OFF` assets:
  - `MARKET_ASSET_POLICY`
  - `TREASURY_STABLE_POLICY`
- `PENDING` or `ISSUANCE_HALTED` assets awaiting final target removal cannot acquire any new lock.
- `RETIRED` assets cannot acquire any lock.
- Final retirement requires every lock to be removed.
- Lock updates and the consumer policy mutation they protect occur in the same transaction.

Asset locks are reconstructed from Market and Treasury genesis rather than imported independently.

## Oracle target epochs

`x/asset` absorbs the existing target scheduler while preserving:

- vote-height-based epoch selection;
- the two-height activation boundary, now per transition record;
- immutable transition records, each activating at its scheduled height;
- one scheduled transition per denom, with contention scoped to that denom;
- old-epoch aggregation before promotion;
- rate pruning after promotion;
- explicit target versioning, advancing once per activation batch;
- empty target sets as valid protocol state.

The per-denom transition model is specified in `docs/superpowers/specs/2026-07-28-oracle-target-transitions-design.md`,
including the correctness argument for multiple transitions in flight and the consume-before-promote ordering the
preblock depends on. It replaces two invariants this list previously carried — a single immutable pending target set at
a time, and complete sorted snapshots rather than deltas. Immutability is retained per record and is what the
height-addressable read depends on; the one-at-a-time constraint is what the redesign removed, because it made unrelated
assets contend for a single chain-global slot.

Primary ABCI boundaries remain narrow:

    type ABCIAssetKeeper interface {
        GetOracleTargets(ctx context.Context, voteHeight int64) (assettypes.OracleTargetSet, error)
        AdvanceOracleTargets(ctx context.Context) ([]string, error)
    }

    type ABCIOracleKeeper interface {
        GetParams(ctx context.Context) (oracletypes.Params, error)
        SetExchangeRateWithEvent(ctx context.Context, rate oracletypes.ExchangeRate) error
        RemoveExchangeRate(ctx context.Context, denom string) error
        RecordVoteAccounting(...)
    }

`x/asset` returns removed denoms because it owns membership; `x/oracle` prunes rates because it owns rate storage. The
application owns their consensus-safe ordering.

## Sidecar target mapping

The sidecar continues to resolve generalized provider pairs off-chain and publishes final rates keyed by target denom.
For example, `agold` maps to a resolver-produced `NOAH/GOLD` economic rate and validators report `agold` units per
`anoah`.

The target query exposes only the active target set and its scheduled transitions. It does not duplicate asset metadata
because all native assets use the same exponent and the sidecar does not consume metadata.

## Lifecycle-aware pricing

`RateSet` is the Oracle-owned in-memory set of fresh rates used for conversion. "Fresh" everywhere in this plan means
fresh by the Oracle module's freshness parameter as surfaced through `RateSet`; `x/asset` defines no staleness policy of
its own.

`PricedAssetView` combines immutable asset records with a `RateSet` and admits only lifecycle states appropriate for the
caller. Market and Treasury consume this boundary instead of interpreting raw target membership or duplicating freshness
checks.

Settlement access remains separate. A fixed governance rate must never appear in ordinary `RateSet` conversion.

## Market policy

Market owns:

    AssetPolicies collections.Map[string, types.AssetPolicy]

Policy presence means the native converter supports the asset. A registered and priced commodity does not automatically
receive a Market policy.

Ordinary conversion:

- offer assets require Market policy and `ACTIVE` or `ISSUANCE_HALTED`;
- ask assets require Market policy and `ACTIVE`;
- `ISSUANCE_HALTED` assets may be consumed but never produced;
- `SUSPENDED`, `WRITTEN_OFF`, `PENDING`, and `RETIRED` assets are excluded;
- every issuance path rechecks status immediately before minting.

Settlement:

- requires `SUSPENDED` plus an active `SettlementPlan`;
- accepts only asset-to-NOAH redemption;
- burns the offered asset;
- draws Treasury's shared buffer proportionally;
- mints only the uncovered NOAH entitlement;
- never exposes the settlement rate to ordinary routing or reverse issuance.

## Treasury policy

Treasury keeps:

    StableAssets collections.KeySet[string]

Registration does not make an asset a stablecoin. Stable enrollment is separate policy.

Liability reporting is partitioned:

    priced_liability_noah
    settlement_liability_noah
    unpriced_stable_assets[{denom, outstanding_supply}]
    written_off_assets[{denom, outstanding_supply, write_off_version}]
    total_liability_available

- `ACTIVE` and `ISSUANCE_HALTED` stable supply with fresh rates is Oracle-priced.
- `SUSPENDED` stable supply with an active settlement is settlement-priced.
- Other `SUSPENDED` stable supply is explicit unpriced exposure, never zero.
- `WRITTEN_OFF` supply is excluded from recognized liability but remains separately disclosed.
- Residual supply on `RETIRED` assets is fully derecognized and visible through asset resolution history rather than the
  liability report.
- A total is available only when every recognized liability is valued.
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
is where this design deliberately places it. One consequence is cross-asset: while any suspended asset remains unpriced,
concurrent settlements of other assets also mint in full. This is accepted for the same reason. The alternative was
considered and rejected.

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

- `RegisterAsset`
- `AmendRegistration`
- `ActivateAsset`
- `HaltIssuance`
- `ResumeIssuance`
- `FinalizeRetirement` (carries `max_residual_supply`; from `PENDING` it cancels the registration)
- `ReactivateAsset`
- `SuspendAsset`
- `CancelRecovery`
- `OpenSettlement`
- `EndSettlement`
- `BeginRecovery`
- `WriteOffAsset`
- `SetEmergencyMandate`

Committee messages (signed by the mandate committee, term-checked):

- `EmergencyHaltIssuance`
- `EmergencySuspendAsset`

Queries:

- `Asset`
- paginated `Assets`
- `AssetLocks`
- `OracleTargets`
- `SettlementPlan`
- `ResolutionHistory`
- `EmergencyMandate`

`OracleTargets` is the single public target-epoch query. A public valuation query remains deferred until the internal
`PricedAssetView` integration is complete.

## Module implementation layout

    x/asset/
      keeper/
        keeper.go
        msg_server.go
        grpc_query.go
        genesis.go
        lifecycle.go
        lifecycle_completion.go
        settlement.go
        emergency_mandate.go
        asset_locks.go
        oracle_targets.go
        valuation.go
      types/
        asset.go
        codec.go
        constants.go
        errors.go
        expected_keepers.go
        genesis.go
        keys.go
        oracle_targets.go
        settlement.go
        emergency_mandate.go
        valuation.go
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

For committee suspension, the keeper holds narrow emergency consumer interfaces:

    type EmergencyMarketKeeper interface {
        ExecuteBasePoolFallback(ctx context.Context, from string) error
    }

    type EmergencyTreasuryKeeper interface {
        ExecuteTaxCapFallback(ctx context.Context, from string) error
    }

Each consumer stores, validates, and applies its own fallback, so `x/asset` supplies only the asset being suspended.

Market and Treasury already depend on `x/asset`, so these references are injected after construction, hooks-style, to
avoid a dependency cycle. Each implementation moves its own reference and maintains its own lock in the same
transaction. These are the only asset-to-consumer edges and exist solely for atomic fallback execution.

## Genesis

Clean prelaunch genesis:

- seeds the existing eight stable assets as `ACTIVE` and Oracle-required;
- moves target epochs and Bank metadata ownership into `x/asset`;
- leaves denomination-keyed rates in `x/oracle`;
- seeds Market policies and Treasury stable enrollment separately;
- reconstructs locks from consumer genesis;
- imports no independent lock list;
- seeds the emergency mandate or its canonical disabled state, with no recorded emergency actions;
- seeds each consumer's emergency reference fallback with that consumer's own genesis.

Validation requires:

- assets, settlement plans, and resolution records in deterministic unique order;
- every target denom to identify a registered Oracle-required asset;
- `ACTIVE` priced assets in current and effective-next targets;
- `ISSUANCE_HALTED` priced assets in the current target until removal activates;
- `PENDING` target combinations to represent activation or cancellation safely;
- `PENDING` assets to have zero supply;
- `SUSPENDED` target combinations to represent removal, absence, or recovery;
- `WRITTEN_OFF` and `RETIRED` assets absent from current and pending targets;
- settlement plans only on `SUSPENDED` assets;
- recovering settlement plans to carry an earliest closing height;
- the current `WRITTEN_OFF` version to have a matching `WRITE_OFF` record;
- `RETIRED` assets with positive supply to have a matching `WRITE_OFF` or `RETIREMENT_RESIDUAL` record;
- the emergency mandate window and term to be internally consistent;
- every recorded emergency action to identify a registered asset and a known action kind;
- metadata, versions, rates, heights, and resolution history to validate independently.

## Application wiring

Intended genesis order:

    bank -> asset -> oracle -> market -> treasury

`x/asset` needs no BeginBlocker or EndBlocker. The Oracle application preblock pipeline:

1. selects the epoch for the committed vote height;
2. aggregates and stores rates in `x/oracle`;
3. records Oracle accounting;
4. advances due `x/asset` target state after consuming the old epoch;
5. asks `x/oracle` to prune returned removed denoms;
6. completes eligible `PENDING` activation and `SUSPENDED` recovery only when a fresh rate and all policy prerequisites
   exist.

Expected touchpoints include app dependency injection, genesis ordering, ABCI interfaces and mocks, vote extension,
proposal, aggregation, preblock, sidecar target polling, CLI queries, and Market/Treasury consumer interfaces.

## Implementation phases

### Phase 0: Contain the confirmed defect

Implemented: Oracle parameter updates reject every current-target removal, including emptying a non-empty set. This
guard remains until the old parameter-owned target path is removed.

### Phase 1: Define the x/asset contract

- asset identity, metadata mutability, the six-state lifecycle, settlement, resolution records, locks, target epochs,
  version semantics, and the emergency mandate with its shared envelope;
- governance messages, committee messages, queries, and events;
- direct NOAH-relative pricing and standardized exponent 18;
- gogo and Pulsar generation;
- type and genesis validation.

### Phase 2: Build x/asset standalone

- keeper collections and dependency interfaces;
- deterministic genesis import/export;
- `AssetLock`, settlement, mandate, and resolution operations;
- target scheduling and activation;
- lifecycle, message, query, and event handlers;
- `PricedAssetView` over Oracle `RateSet`;
- completion of eligible `PENDING` activation and `SUSPENDED` recovery;
- focused keeper and query suites for all six statuses and derived target/settlement combinations;
- standalone `AppModule` service and genesis registration;
- dependency-injection provider, governance AutoCLI descriptors, and simulation genesis/store-decoder support.

Production consumers remain on their current paths until replacement integration is complete.

### Phase 3: Cut target ownership over to x/asset

- wire AssetKeeper through app genesis and dependency injection;
- move target epoch state from `x/oracle`;
- split ABCI dependencies into narrow Asset and Oracle interfaces;
- update vote extension, proposal, aggregation, preblock, sidecar, validation, CLI, and mocks;
- move Bank metadata registration into `x/asset`;
- preserve epoch versioning and activation timing exactly;
- verify attendance accounting is preserved exactly across cutover: fleet-wide-OR participation, the functioning-block
  threshold, and windowed settlement.

### Phase 4: Move Market policy ownership

- add Market `AssetPolicies`;
- move Tobin tax and conversion eligibility out of Oracle parameters;
- enforce lifecycle-aware offer, ask, issuance, and settlement rules;
- execute one-way settlement redemption;
- add the Market-owned base-pool emergency fallback and its governance message;
- implement the Market emergency fallback executor with atomic lock maintenance;
- maintain Market locks atomically.

### Phase 5: Move Treasury classification ownership

- add Treasury `StableAssets`;
- replace every target-derived liability enumeration;
- preserve priced liability and redemption throughout `ISSUANCE_HALTED`;
- partition priced, settlement-priced, unpriced, and written-off exposure;
- add the Treasury-owned reference-tax-cap emergency fallback and its governance message;
- implement the Treasury emergency fallback executor with atomic lock maintenance;
- maintain Treasury locks atomically.

### Phase 6: Remove legacy coupling

- remove Tobin taxes, target state, scheduling, metadata ownership, and obsolete queries from `x/oracle`;
- reserve removed established protobuf field numbers where required;
- remove the temporary removal guard;
- add full lifecycle and rate-contract integration coverage, including the single-proposal emergency scenario.

## Required integration scenarios

### Normal activation and retirement

1. Register an 18-decimal priced asset such as `agold`; verify registration rejects a denom with pre-existing supply or
   metadata.
2. Correct a metadata mistake with `AmendRegistration` while `PENDING`; verify metadata freezes on activation.
3. Activate its target through the delayed epoch.
4. Verify it remains `PENDING` until its first fresh rate; verify a validator omitting the new target while pricing any
   established one stays fully attended, and one pricing nothing accrues eligible-but-not-attended units while blocks
   function.
5. Configure Market and Treasury policy.
6. Issue non-zero supply.
7. Halt issuance and verify pricing, liability, transfers, and redemption continue.
8. Verify finalization fails with locks, and with supply above `max_residual_supply`.
9. Redeem most supply, clear dependencies, and finalize with a residual bound covering the unredeemed dust.
10. Verify target removal activates, the old epoch is consumed before rate pruning, validators stop pricing the denom,
    the asset is `RETIRED` with a `RETIREMENT_RESIDUAL` record, and residual balances remain transferable.
11. Verify the `RETIRED` tombstone cannot be reused and, with residual supply, cannot be reactivated.

### Suspension, settlement, and recovery

1. In one governance proposal, move the base-pool reference, move the reference tax cap, and suspend an `ACTIVE` or
   `ISSUANCE_HALTED` asset; verify issuance and ordinary redemption stop in that block.
2. Preserve balances and disclose outstanding stable supply as unpriced exposure.
3. Consume the old epoch before pruning its rate while the asset remains `SUSPENDED`.
4. Open a fixed settlement with an earliest closing height and recognize its maximum NOAH entitlement.
5. Redeem by burning the asset, drawing the buffer proportionally, and minting only the uncovered NOAH.
6. Close the settlement after its earliest closing height; verify executed redemptions stand and remaining supply
   reverts to unpriced exposure.
7. Verify closing an activated settlement before its earliest closing height is rejected while supply remains, that
   recovery cannot lower the commitment, and that write-off remains the only earlier exit; verify replacement after
   closure requires a new plan and version.
8. Verify a fully settled asset closes its plan at zero supply without waiting for the commitment height and finalizes
   to `RETIRED` without recovery, write-off, or any oracle transition.
9. Begin recovery on a separate suspended asset and keep it `SUSPENDED` through target addition and fresh-rate
   collection.
10. Verify recovery completion requires the settlement closed, then recovers to `ISSUANCE_HALTED`.
11. Require a separate `ResumeIssuance` before issuance resumes.

### Emergency mandate

1. Committee-suspend an asset holding both live references: verify each consumer's pre-approved fallback executes, locks
   move atomically, suspension lands in the same transaction, the action is recorded for the term, the asset version
   advances, and events carry the term.
2. Halt issuance on an ambiguous signal, then suspend the same asset later in the same term; verify the escalation is
   permitted.
3. Committee-suspend, let governance recover and resume the asset, and verify a second committee suspension of that
   asset in the same term is rejected while the governance path succeeds.
4. Verify committee actions are rejected under a disabled, expired, or not-yet-active mandate and with a stale term.
   Verify mandate replacement clears recorded usage, so the same action becomes available again under the new term.
5. Make a fallback invalid by suspending the fallback asset first; verify the committee action fails atomically and the
   single-proposal governance path still executes.
6. Replace the mandate; verify the term advances and messages carrying the old term are rejected.

### Write-off and reinstatement

1. Write off `SUSPENDED` supply without changing balances.
2. Keep the quantity and append-only record queryable.
3. Reinstate through settlement or Oracle recovery into `SUSPENDED`.
4. Verify historical records remain unchanged.
5. Finalize a different `WRITTEN_OFF` asset directly to `RETIRED` with residual supply and verify no new record is
   appended.
6. Permit final retirement only after locks, plans, and targets are cleared.

Additional cases cover unpriced assets, non-stable tokenized commodities, registration and retirement cancellation,
stale versions, pipelined proposals across automatic completions, conflicting target epochs, native-denom rejection,
metadata rules, each lock kind, arithmetic boundaries at the documented rate band edges, stale rates, and incomplete
liability.

## Live-chain upgrade variant

If deployed after a live genesis:

- add the `x/asset` store through a named upgrade;
- retain old decoders until migration completes;
- seed assets, metadata, target versions and heights, Market policy, Treasury enrollment, and locks from committed
  state;
- import active settlements and resolution history exactly;
- verify every target maps to one Oracle-required asset;
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

1. Temporary Oracle target-removal containment. Completed.
2. Contract alignment across protobuf, types, and keeper: renames, `SetAssetMetadata`, `CloseSettlement`, residual
   finalization, `ResolutionRecord`, version semantics, registration preconditions, and the emergency mandate.
3. Target ownership, ABCI, sidecar, metadata, and pricing-consumer cutover.
4. Market policy ownership and settlement execution.
5. Treasury stable enrollment and partitioned liabilities.
6. Legacy Oracle removal and full integration, including the single-proposal emergency scenario.
7. Live-chain migrations only if required.

## Completion criteria

The design is complete when:

- every priced asset has one denom-keyed NOAH-relative rate;
- no module infers asset existence or liability from Oracle parameters;
- activation waits for a fresh rate;
- normal retirement preserves pricing and redemption for outstanding supply, and can close the books on a bounded,
  disclosed residual without a sham transition;
- every lifecycle state reaches `RETIRED` through an honest path;
- suspension never treats unavailable valuation as zero;
- from suspension onward, an asset's maximum remaining claim on NOAH is a closed, governance-chosen bound;
- suspension and its policy prerequisites execute in one proposal;
- a live mandate suspends a failing asset in one transaction, and every mandate bound — term, window, one action per
  asset per kind per term, fallback validity — is enforced;
- committee powers only remove capabilities: no committee path restores issuance, redemption, pricing, or recognition,
  or moves value;
- settlement is one-way, explicit, auditable, and cannot enable issuance; its window closes only after any announced
  commitment passes or supply is exhausted, and write-off is the only earlier override;
- every governance mistake outside a committed settlement window is correctable without write-off;
- recovery restores `ISSUANCE_HALTED` before issuance can resume;
- write-off, reinstatement, and residual retirement preserve balances and immutable history;
- final retirement requires no locks, no plan, no target, and residual within the governance-approved bound;
- target removal remains height-correct and consumes the old epoch before pruning;
- attendance is unconditional per window: pricing any target attends a validator, correlated incapacity grades nobody,
  and sustained whole-report absence against a pricing majority jails at settlement;
- unpriced assets cannot accidentally enter Oracle-dependent monetary policy;
- commodities may be priced without becoming stable liabilities;
- the metadata of an unactivated asset is correctable and the metadata of an activated asset is immutable;
- the supported rate precision band is documented and arithmetic fails closed outside it;
- the full normal and emergency integration scenarios pass.
