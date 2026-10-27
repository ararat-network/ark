# Protocol monitoring

For network operators, committees, and governance: collect protocol-state signals, calibrate alerts, and diagnose
changes across the exposure multiplier, Reserve recognition, Oracle rates and attendance, the conversion pool,
committees, and Disbursement. This guide specifies monitoring requirements;
[future changes](../direction/FUTURE_CHANGES.md#protocol-monitoring-tooling) tracks delivery of the required tooling.

This note says what to watch in Ark's own state: which query answers a question, which event carries it, what
should wake someone, and what should not.

It does not cover node or process health. [Process monitoring](PROCESS_MONITORING.md) owns that seam: the
`[prometheus]` scrape endpoint, otel.yaml, and CometBFT's own registry are how an operator watches the machine. This
document is about what the chain believes.

Responses are keyed by the alert IDs used here and live in the [governance
runbook](../governance/GOVERNANCE_RUNBOOK.md) and the [committee runbook](../governance/COMMITTEE_RUNBOOK.md); each alert
links its own. The split: an alert states a condition, why it matters, and the
first thing to look at; the response states what a committee or governance then decides. Writing the decision into
both is how the two drift apart.

## Contents

- [1. The rule: observability is off-chain](#1-the-rule-observability-is-off-chain)
- [2. Exposure multiplier](#2-exposure-multiplier)
- [3. Reserve recognition](#3-reserve-recognition)
- [4. Oracle rates and attendance](#4-oracle-rates-and-attendance)
- [5. Conversion pool](#5-conversion-pool)
- [6. Committees](#6-committees)
- [7. Disbursement](#7-disbursement)
- [8. Source inventory](#8-source-inventory)

## 1. The rule: observability is off-chain

Every signal below is already exact on-chain state, served by a query or carried in a typed event. Emitting
Prometheus counters from consensus code would duplicate that in a place every validator pays for, and this
project keeps `pkg/metrics` minimal deliberately. So: **index the typed events, poll the queries.** Nothing here
is an argument for adding a meter to a keeper.

Two consequences worth stating once, because both recur in every section added below.

**Alert state is off-chain.** Conditions of the form "N consecutive updates" need history the chain does not
keep. Module state stores current values, overwritten in place. Whatever evaluates these alerts retains its own
series.

**Queries and events answer different questions.** An event says something happened; a query says what is true
now. A failure that consists of *nothing happening* is invisible to an event indexer, and §2.3 is the worked
example.

## 2. Exposure multiplier

Covers the exposure multiplier, implemented in `x/treasury` ([local
design](../../x/treasury/README.md#exposure-sampling-and-refresh)).

The multiplier is a controller nobody watches by default. It ships inert — every weight zero, `m = 1` — so
nothing it does is visible until a weight is voted positive, and after that its whole effect is a number that
quietly changes how much expansion principal is retained and how much capital a committee may move. There is no
user-facing symptom when it is working, and none when it is stuck either.

### 2.1 Two phases, and the first one is not alerting

**Phase A — inert (now, until activation).** The weights are zero, so `m` is pinned at one and the composite
carries no information. But the *indicators* are live from the first block: inside `SettleConversions` the flow series
folds every block and the volatility series every `volatility_sample_seconds`, and `ExposureStatus` reports both
alongside the liability ratio at each
update. This is the calibration window, and it is the whole reason the design ships disabled.

The work in Phase A is to learn the calm-state distribution of the three indicators on this chain — not to alert on
them. Collect at least a few weeks of `ExposureStatus`, then choose weights so that a calm reading composes to roughly
1.0–1.1 and a genuinely stressed one approaches the cap. Weights chosen from priors rather than from this data are
guesses, which is why [genesis](../governance/GENESIS.md#4-treasury) launches them at zero, and
the [governance runbook](../governance/GOVERNANCE_RUNBOOK.md#6-treasury) gates switching them on on this window having
produced numbers.

Panel 2 of the dashboard (§2.6) is the instrument. Building it is not preparation for calibration; it is the
calibration.

One alert is worth having even in Phase A: **A2, refresh stalled**. A multiplier that cannot recompute is broken
whether or not anyone is reading it, and Phase A is precisely when that would go unnoticed.

**Phase B — activated.** The full A-series applies. The alerts in §3–§7 apply from launch in either phase.

### 2.2 Reading the signals

[§8](#8-source-inventory) lists every source and the fields read from it. Three readings need care:

- `annualised_volatility` is derived when queried from `exposure_state.volatility_variance`; the refresh event carries
  the value the refresh used.
- `exposure_state.flow_pressure` is NOAH net of expansion, floored at zero, and moves every block;
  `EventExposureRefreshed.flow_ratio` is that pressure as a share of net liability at the refresh.
- `RequiredReserveCapital` reads the **gross** Reserve target, so `FundStatus.strategic_reserve_target` is the field
  that matches it, not `strategic_reserve_net_target`.

### 2.3 The event/query split, and why the poller is not optional

`EventExposureRefreshed` is emitted when a refresh **completes**, and on every completed refresh — including one
that leaves the multiplier unchanged. There is no equality guard before the emit, deliberately, so the event's
cadence is a heartbeat. A refresh that could not run emits nothing at all.

So silence on that event means "no refresh happened", never "nothing changed". That much an indexer could infer.
What it cannot do is tell a stalled refresh from a stalled indexer, a stopped chain, or its own broken
subscription — all four look identical from the outside, and three of them are false alarms.

`ExposureStatus.refresh_pending` is a positive answer to the actual question: the chain saying it owes an update
it could not produce. That is why A2 is a poller condition and not an indexer condition, and why the poller
cannot be dropped in favour of the event stream.

### 2.4 Alerts

#### A1 — Multiplier pinned at the cap · page

**Condition:** `EventExposureRefreshed.uncapped_multiplier > multiplier_cap` on `N` consecutive refreshes
(suggest `N = 3`, about three hours at the default cadence).

**Why it wakes someone:** the cap is the model's statement that it can express no more risk. One refresh at the
cap is a spike; sustained pinning means measured exposure has outrun the configured ceiling, and the targets are
no longer tracking the indicators — they are tracking a constant. Everything downstream, retention and committee
bounds alike, is capped with it. This is the signal the whole model exists to produce, and the only one that
should page for its own sake.

**First look:** `EventExposureRefreshed` carries all three indicators, so read which one is driving `uncapped`. The
decision that follows is governance's ([governance runbook](../governance/GOVERNANCE_RUNBOOK.md#exposure-alerts)).

#### A2 — Refresh stalled · page (and the one alert that applies in Phase A)

**Condition:** `ExposureStatus.refresh_pending == true` for longer than `3 × exposure_refresh_period_blocks`.

**Why:** the multiplier is frozen at its last value while whatever prevented recomputation persists. Two known
causes, both benign-looking: liability could not be valued from usable rates, or circulating NOAH was
non-positive. The retry is designed to be silent and automatic, so a stall that does not clear is the failure
mode with no other symptom — and stale targets during stress are worse than no targets, because they look
authoritative.

**First look:** `FundStatus` for an incomplete partition, and the oracle for the reference feed. If liability
valuation is the blocker then A4 is firing too, and A4 is the actual incident.

#### A3 — Step limit binding repeatedly · warn

**Condition:** `|uncapped_multiplier − previous_multiplier| > multiplier_max_step` on `N` consecutive refreshes
(suggest `N = 6`).

**Why:** the multiplier is chasing a target it cannot reach, so the rate limiter — not the risk model — is
setting policy. That is the limiter working as designed during a fast move, and a misconfiguration if it
persists through calm. Distinguishing the two is human judgement, hence warn rather than page.

**First look:** whether it coincides with a real move. If it does, no action. If it persists without one, the
step is too small for the chosen weights, and the pair is revisited together.

#### A4 — Liability valuation incomplete, sustained · page

**Condition:** `EventLiabilityIncomplete` observed on every settling block for more than an hour.

**Why:** not an exposure alert, but it disables most of what exposure feeds. While incomplete, the whole block's
expansion principal parks in the Reserve rather than filling targets, `RequiredReserveCapital` refuses to answer
at all — so no Reserve burn can be authorised — and the multiplier keeps refreshing from an aggregate that
understates exposure.

**First look:** the event carries `stale_member_supply` and `untrusted_suspended_supply`, which say precisely which
members and why. A stale feed and a suspension-without-plan need different fixes. Expect a committee follow-up if the
window was long ([governance runbook](../governance/GOVERNANCE_RUNBOOK.md#exposure-alerts)).

#### A5 — Reserve burnable surplus collapsed · notify the committee

**Condition:** `reserve/BurnableSurplus.burnable` falls from positive to zero, when driven by a rise in
`multiplier` rather than a fall in recognised capital.

**Why:** the committee's burn authority just went to zero without the committee doing anything, because the
requirement moved underneath it. This is the mechanism working correctly — the worst moment to destroy reserve
capital is when the risk model says it is needed — but it will look like a bug to whoever's proposal fails.

**While valuation is incomplete** the query errors rather than reporting zero. A4 pages on that condition, so A5
stays silent through it.

**Response:** [committee runbook](../governance/COMMITTEE_RUNBOOK.md#alerts).

#### A6 — Buffer coverage falling · warn

**Condition:** `FundStatus.redemption_buffer_balance ÷ FundStatus.net_liability` below a governance-chosen floor.

**Why:** this is the outcome measure — what a redeemer actually receives — and it is *not* an exposure signal.
It falls when NOAH weakens, because the denominator is NOAH-valued, which is the reflexivity the multiplier
exists to prepare for. Watching it beside `multiplier` is how you tell whether the preparation is keeping up: a
rising multiplier with flat coverage means retention is not arriving, usually because there are no expansions to
retain from.

**Response:** [committee runbook](../governance/COMMITTEE_RUNBOOK.md#alerts).

### 2.5 What not to alert on

- **`multiplier > 1`.** That is the mechanism operating, not an incident.
- **A single `EventLiabilityIncomplete`.** Transient by construction; a returning feed clears it with no
  lifecycle action.
- **The multiplier changing.** Every update changes it by design; only the guardrails binding (A1, A3) are
  informative.
- **`flow_pressure` spikes.** The EMA and the step limit already absorb them; alerting here pages on ordinary
  redemption days.

### 2.6 Dashboard

One page, four panels.

1. **Multiplier over time** — `multiplier` and `uncapped_multiplier` on one axis, with the cap drawn as a
   horizontal line. The gap between the two series *is* A1 and A3, read visually.
2. **The three indicators** — liability ratio, annualised volatility, flow pressure, each against its
   contribution to the composite (`weight × indicator`). Contribution is what matters: a large indicator under a
   zero weight is noise. This is the Phase A calibration instrument.
3. **Targets and balances** — the six targets from `FundStatus` beside the three fund balances, with
   `exposure_multiplier` shown so the numbers reconcile against the liability on the same panel.
4. **Outcome** — buffer coverage (`balance ÷ net_liability`), and retained-versus-burned per block from
   `EventExpansionAllocated`. This is where "is the cushion actually growing" gets answered.

In Phase A the same page shows panel 1 flat at one.

### 2.7 Collection requirements

- **Poller.** `treasury/ExposureStatus` once per `exposure_refresh_period_blocks / 2` — 300 blocks, about half
  an hour at defaults — so no update is missed and `refresh_pending` is sampled often enough for A2. It folds no
  registry and reads stored state only, so it stays cheap and answers even when `FundStatus` is degraded, which
  is why A2 stays reliable during an A4 incident. `reserve/BurnableSurplus` on the same cadence feeds A5; it folds
  the registry like `FundStatus`, so read its error during an A4 incident as A4's state, never as a zero bound.
- **Indexer.** The four Treasury events in [§8](#8-source-inventory).
- **Retention.** At least 30 refresh periods of update history, for the "N consecutive" conditions and for A5's
  attribution.

The collector is the indexer the [tooling direction](../direction/TOOLING_DIRECTION.md#ark-indexer) reserves: it
exports the event series for Prometheus and hosts the poller, and must preserve these semantics. Until it exists no
protocol series exist, so [rules/ark.yml](../../contrib/observability/rules/ark.yml) carries only the Disbursement
alerts the membership service's series answer (§7);
[future changes](../direction/FUTURE_CHANGES.md#protocol-monitoring-tooling) tracks the rest.

### 2.8 Calibration and operational ownership

1. **Thresholds.** `N` for A1 and A3, and the coverage floor for A6, are placeholders. Set them from Phase A
   data, in the same pass that chooses the weights.
2. **Who receives the A1, A2 and A4 pages.** An ops decision, not a protocol one; the dashboard itself joins
   [contrib/observability](../../contrib/observability/README.md) once its series exist.
3. **A5's attribution** — separating "multiplier rose" from "recognised capital fell" — needs both series
   retained. If that proves awkward, downgrade A5 to a plain bound-reached-zero notification; the committee
   still learns what it needs to.
4. **A5 reads the bound at poll time, not at execution.** The handler solves again when a burn executes, against
   the multiplier then; the
   [committee runbook](../governance/COMMITTEE_RUNBOOK.md#burns) leaves margin
   for that.

## 3. Reserve recognition

The Reserve's recognised capital, which sizes its gap, its burn bound, and where expansion principal goes, rests on two
clocks per position: the committee's attestation and the Oracle's rate. Either running out takes the position's credit
to zero without anything moving ([mechanism](../../x/reserve/README.md#mandate-custody-and-journal)).

**Signals.** `reserve/RecognisedCapital` (`arkd query reserve recognised-capital`) reports each eligibility entry's
`attested_quantity`, `stale_quantity`, `credit`, `max_attestation_age`, and `max_rate_age`; `reserve/OpenPositions`
gives each position's `attested_time`; Oracle's `StoredRates` gives each feed's `block_time`.
`EventLedgerEntryRecorded` marks every attestation.

#### R1 — Attestation lapsing · notify the committee

**Condition:** for an open position, `attested_time + max_attestation_age` within a lead time of now (suggest 14 days).

**Why:** past it the position's whole holding counts for nothing, the Reserve's gap reopens, and expansion principal
is pulled back in. A custodian statement takes days to obtain, so the warning has to come before the window, not at it.

**First look:** the position's venue and the reference its last attestation cited.

**Response:** [committee runbook §5](../governance/COMMITTEE_RUNBOOK.md#deployment-and-attestation).

#### R2 — Stale quantity · page

**Condition:** any entry's `stale_quantity` positive.

**Why:** credit has already lapsed. With no Reserve committee seated nobody can restate it, since governance has no
restatement message.

**First look:** which positions R1 warned about, and whether a committee is seated (C1).

**Response:** [committee runbook §5](../governance/COMMITTEE_RUNBOOK.md#deployment-and-attestation); appointment in the
[governance runbook](../governance/GOVERNANCE_RUNBOOK.md#3-committees) if none is seated.

#### R3 — Recognition rate stale · warn

**Condition:** an eligible entry's feed has a stored `block_time` older than the entry's `max_rate_age`.

**Why:** the same lapse from the price side: the entry's credit is zero until the rate is fresh again.

**First look:** O2 for the feed.

## 4. Oracle rates and attendance

Every consumer fails closed on a missing or stale rate, so a rate problem shows up as conversions refused, valuation
incomplete, and credit lapsed, each looking like its own incident
([mechanism](../../x/oracle/README.md#1-oracle-state-and-lifecycle)).

**Signals.** `oracle/StoredRates` (`arkd query oracle stored-rates`) reports every rate with the `block_time` and
`block_height` that set it; `oracle/Params` gives `max_exchange_rate_age`; `oracle/Feeds` the active set and scheduled
transitions; `oracle/ReferenceDenom` the reference. Events: `EventOracleJail`, `EventFeedTransitionScheduled`,
`EventFeedsActivated`, `EventReferenceDenomUpdated`.

#### O1 — Reference rate stale · page

**Condition:** now minus the reference feed's stored `block_time` exceeds `max_exchange_rate_age`.

**Why:** a stale reference closes every NOAH conversion path that needs it, and Treasury's fee factors freeze at their
last values until a live rate returns. No re-point restores pricing unless a successor feed is fresh.

**First look:** process monitoring's N4 for fleet participation, and S3 on the sidecars for the reference's route legs.

**Response:** [governance runbook §4](../governance/GOVERNANCE_RUNBOOK.md#4-oracle) if the reference itself must move.

#### O2 — Active feed stale · warn

**Condition:** any active feed's stored `block_time` older than `max_exchange_rate_age`, sustained (suggest five
minutes).

**Why:** that denomination stops converting; a registry member's stale feed also makes liability valuation incomplete,
which is A4.

**First look:** the feed's route legs on the sidecars (S1–S3).

#### O3 — Validator jailed for oracle absence · notify

**Condition:** `EventOracleJail`.

**Why:** the validator is out of the set for `jail_duration`, a week at launch, and its power has left the oracle
quorum. Several in one settlement point to a shared cause, such as a provider most operators use.

**First look:** the event's `attended_blocks` against `eligible_blocks`, and N4.

#### O4 — Feed scheduled · notify operators

**Condition:** `EventFeedTransitionScheduled` adding a feed.

**Why:** the feed activates two blocks after it is scheduled, and a sidecar without markets for it earns nothing for
it from activation.

**Response:** [pricefeed operations §6](PRICEFEED_OPERATIONS.md#6-feed-changes).

## 5. Conversion pool

**Signals.** `market/Pool` (`arkd query market pool`) reports `base_pool` and `ark_pool_delta`; `EventPoolUpdated` marks
every depth change and `EventSwap` every conversion.

#### M1 — Expansion flow above the floor · notify the conversion committee

**Condition:** `ark_pool_delta` more negative than a hundredth of `base_pool`, held (suggest a day).

**Why:** expansion is converting faster than the pool recovers, so it pays above the spread floor: the depth rule's
trigger.

**Response:** [committee runbook §6](../governance/COMMITTEE_RUNBOOK.md#sizing-depth).

#### M2 — Redemption pressure · warn

**Condition:** `ark_pool_delta` positive and rising, held (suggest a day).

**Why:** redemptions are outrunning recovery. Depth must not be raised into it, and the Buffer pays part of every
redemption from inventory.

**First look:** A6 for Buffer coverage, and asset statuses for a failing member.

## 6. Committees

**Signals.** Each mandate query reports `active`, the term, and the window's `expiry_time`; those with allowances report
what the term has used: `security-mandate`, `asset emergency-mandate`, `treasury economic-mandate`, `reserve mandate`,
`market conversion-mandate`, `claims mandate`, `disbursement grants-mandate` and `registrar-mandate`. Events:
`EventClaimSubmitted`, `EventEmergencySuspended`, and each module's `Event*MandateSet`.

#### C1 — Term expiring · notify governance

**Condition:** an active mandate's `expiry_time` within a lead time of now (suggest 30 days).

**Why:** a lapsed window disables the committee with no event, and reappointment takes a deposit period and a vote. A
lapsed grants window also stops registration, and a lapsed Reserve window leaves positions unattested (R1).

**Response:** [governance runbook §3](../governance/GOVERNANCE_RUNBOOK.md#3-committees).

#### C2 — Committee claim submitted · notify governance

**Condition:** `EventClaimSubmitted` for a claim whose record shows committee origin.

**Why:** the cancellation period is governance's veto over the payout, a week at launch, and a vote needs about four
days of it. Review starts at submission or the veto is theoretical.

**Response:** [governance runbook §9](../governance/GOVERNANCE_RUNBOOK.md#9-claims).

#### C3 — Emergency suspension · page governance

**Condition:** `EventEmergencySuspended`.

**Why:** the asset's holders cannot redeem, and until a settlement plan opens its supply makes valuation incomplete (A4).
The committee's act only contains; every decision after it is governance's.

**Response:** [governance runbook §5](../governance/GOVERNANCE_RUNBOOK.md#5-asset-registry).

#### C4 — Allowance nearly used · notify governance

**Condition:** a term's remaining allowance (claims limit, grants compensation allowance, Reserve deployment allowance)
under a share of the whole (suggest a tenth).

**Why:** the committee's next act is refused, which during an incident is when it matters.

**Response:** [governance runbook §3](../governance/GOVERNANCE_RUNBOOK.md#3-committees).

## 7. Disbursement

**Signals.** `disbursement/Grants` (`arkd query disbursement grants`) filters by `--registrar-term` and
`--committee-term`; `disbursement/Balance` reports the open member tranche, and `EventTrancheOpened` what each opening
added. `EventGrantEntryRecorded` carries every grant's creation, payment, suspension, and cancellation with its `actor`,
and `EventPaymentDeferred` every refused payment.

The membership service exports the series the Disbursement rules read, since D1 is a join against its admission
records and D3 is the number it watches to raise the next funding request: `ark_membership_registrations_unreconciled`,
a gauge by `registrar_term` of registrations on chain for more than a day with no matching admission record;
`ark_membership_member_tranche_remaining_ratio`, the open member tranche over the last opening's `amount`; and
`ark_membership_comparison_last_success_seconds`, the Unix time the comparison last completed. The names are the
contract: `TestAlertRulesReadPinnedSeries` reads them from this paragraph.

#### D1 — Registrations unreconciled · page the grants committee

**Condition:** a grant created by the registrar that the membership service's admission records do not match within a
day.

**Why:** a stolen registrar key can commit the whole open tranche, and the two-week hold before any member pays is
the only window to suspend fakes for nothing. This alert is a join against off-chain records: the chain cannot know
who was admitted.

**Response:** [committee runbook §8](../governance/COMMITTEE_RUNBOOK.md#8-grants).

#### D2 — Payment deferred · warn

**Condition:** `EventPaymentDeferred`.

**Why:** the payee refused the payment, so the chain holds it and retries a day later. Until the payee changes, the grant
keeps deferring.

**Response:** [committee runbook §8](../governance/COMMITTEE_RUNBOOK.md#8-grants), destination recovery.

#### D3 — Member tranche nearly committed · notify governance

**Condition:** the open member tranche under a share of the last opening's `amount` (suggest a tenth).

**Why:** registration stops when it runs out, and the next quarter takes a proposal and a vote.

**Response:** [committee runbook §8](../governance/COMMITTEE_RUNBOOK.md#8-grants), a member tranche.

#### D4 — Comparison stalled · page the grants committee

**Condition:** `time() - ark_membership_comparison_last_success_seconds` past two days.

**Why:** D1 is silent when the comparison does not run, and silence must not read as reconciled (§2.3). Two missed
days leave most of the hold to suspend in.

**Response:** [committee runbook §8](../governance/COMMITTEE_RUNBOOK.md#8-grants): compare by hand against
`grants --registrar-term` until the service's comparison runs again.

## 8. Source inventory

Every query and typed event the alerts above read, with the fields they read. It is the input contract for the
indexer that collects them, a poller for the queries and the event stream for the rest, which Prometheus then scrapes.
The metric names it emits belong to it; this table pins only the chain's side, and §7 the membership service's.
`TestProtocolSourceInventoryResolves`
resolves every source and field against the registered protobuf descriptors, so renaming or removing anything listed
fails the build here rather than blanking a dashboard elsewhere. Field paths descend with `.`; `[]` marks a repeated
field.

| Source | Kind | Fields read | Cadence | Alerts |
| --- | --- | --- | --- | --- |
| `ark.treasury.v1.Query/ExposureStatus` | query | `refresh_pending`, `exposure_state.multiplier`, `exposure_state.liability_ratio`, `exposure_state.flow_pressure`, `exposure_state.volatility_variance`, `annualised_volatility` | half a refresh period, 300 blocks (§2.7) | A2 |
| `ark.treasury.v1.Query/FundStatus` | query | `net_liability`, `redemption_buffer_balance`, `strategic_reserve_target`, `stale_member_supply[]`, `untrusted_suspended_supply[]` | 300 blocks | A2, A4, A6 |
| `ark.reserve.v1.Query/BurnableSurplus` | query | `burnable` | 300 blocks | A5 |
| `ark.reserve.v1.Query/RecognisedCapital` | query | `recognised`, `assets[].denom`, `assets[].stale_quantity`, `assets[].max_attestation_age`, `assets[].max_rate_age` | minutes | A5, R1, R2, R3 |
| `ark.reserve.v1.Query/OpenPositions` | query | `positions[].position_id`, `positions[].quantity`, `positions[].attested_time` | hourly | R1 |
| `ark.oracle.v1.Query/StoredRates` | query | `rates[].denom`, `rates[].block_time` | a minute | O1, O2, R3 |
| `ark.oracle.v1.Query/Params` | query | `params.max_exchange_rate_age` | hourly | O1, O2 |
| `ark.oracle.v1.Query/ReferenceDenom` | query | `reference_denom` | hourly | O1 |
| `ark.market.v1.Query/Pool` | query | `base_pool`, `ark_pool_delta` | minutes | M1, M2 |
| `ark.security.v1.Query/SecurityMandate` | query | `active`, `mandate.envelope.expiry_time` | hourly | C1 |
| `ark.asset.v1.Query/EmergencyMandate` | query | `active`, `mandate.envelope.expiry_time` | hourly | C1 |
| `ark.treasury.v1.Query/EconomicMandate` | query | `active`, `mandate.envelope.expiry_time` | hourly | C1 |
| `ark.reserve.v1.Query/Mandate` | query | `active`, `mandate.envelope.expiry_time`, `mandate.deployment_allowance`, `allowance_remaining` | hourly | C1, C4 |
| `ark.market.v1.Query/ConversionMandate` | query | `active`, `mandate.envelope.expiry_time` | hourly | C1 |
| `ark.claims.v1.Query/ClaimsMandate` | query | `active`, `mandate.envelope.expiry_time`, `mandate.committee_claim_limit`, `allowance_remaining` | hourly | C1, C4 |
| `ark.disbursement.v1.Query/GrantsMandate` | query | `active`, `mandate.envelope.expiry_time`, `mandate.compensation_allowance`, `compensation_remaining` | hourly | C1, C4 |
| `ark.disbursement.v1.Query/RegistrarMandate` | query | `active`, `mandate.envelope.expiry_time` | hourly | C1 |
| `ark.claims.v1.Query/Claim` | query | `claim.origin`, `claim.closing_time` | on each claim submission | C2 |
| `ark.disbursement.v1.Query/Balance` | query | `member_tranche` | hourly | D3 |
| `ark.treasury.v1.EventExposureRefreshed` | event | `previous_multiplier`, `multiplier`, `uncapped_multiplier`, `liability_ratio`, `annualised_volatility`, `flow_ratio` | every block | A1, A3 |
| `ark.treasury.v1.EventLiabilityIncomplete` | event | `stale_member_supply[]`, `untrusted_suspended_supply[]` | every block | A4 |
| `ark.treasury.v1.EventExpansionAllocated` | event | `redemption_buffer_credit`, `strategic_reserve_credit`, `insurance_credit`, `overflow_burn` | every block | dashboard (§2.6) |
| `ark.treasury.v1.EventRedemptionBufferDrawn` | event | `payment` | every block | dashboard (§2.6) |
| `ark.oracle.v1.EventOracleJail` | event | `validator`, `eligible_blocks`, `attended_blocks` | every block | O3 |
| `ark.oracle.v1.EventFeedTransitionScheduled` | event | `denom`, `direction`, `activation_vote_height` | every block | O4 |
| `ark.asset.v1.EventEmergencySuspended` | event | `denom`, `term` | every block | C3 |
| `ark.claims.v1.EventClaimSubmitted` | event | `claim_id` | every block | C2 |
| `ark.disbursement.v1.EventGrantEntryRecorded` | event | `grant_id`, `action`, `actor` | every block | D1 |
| `ark.disbursement.v1.EventPaymentDeferred` | event | `grant_id`, `payee`, `due_time` | every block | D2 |
| `ark.disbursement.v1.EventTrancheOpened` | event | `amount`, `member_tranche` | every block | D3 |

- **Events** are typed: each block's results carry them with the source's full name as the event type, so a generic
  ABCI event indexer suffices.
- **Off-chain join:** D1 also needs the membership service's admission records beside the registrar's grants.
- **Retention:** at least 30 days, for the "N consecutive" and "held" conditions and for setting thresholds.
- **Thresholds:** every "suggest" above is a placeholder, set with the operators, committees, and governance that
  receive the alert.

The process side of oracle attendance and price-feed liveness, what this node signs and what its sidecar serves, is
[process monitoring §3](PROCESS_MONITORING.md#3-what-to-watch), read from the scrape endpoints rather than from state.
