# Protocol monitoring

For network operators and economic committees: collect protocol-state signals, calibrate alerts, and diagnose changes.
Coverage currently centres on exposure and capital. This guide specifies monitoring requirements;
[future changes](FUTURE_CHANGES.md#protocol-monitoring-tooling) tracks delivery of the required tooling.

This note says what to watch in Ark's own state: which query answers a question, which event carries it, what
should wake someone, and what should not.

It does not cover node or process health. `docs/PROCESS_MONITORING.md` owns that seam — the `[prometheus]` scrape
endpoint, otel.yaml, and CometBFT's own registry are how an operator watches the machine. This document is about
what the chain believes.

Responses live in `docs/ECONOMIC_COMMITTEE_RUNBOOK.md`, keyed by the alert IDs used here. The split: an alert states a
condition, why it matters, and the first thing to look at; the runbook states what a committee or governance
then decides. Writing the decision into both is how the two drift apart.

## Contents

- [1. The rule: observability is off-chain](#1-the-rule-observability-is-off-chain)
- [2. Exposure multiplier](#2-exposure-multiplier)
- [3. Other subsystems](#3-other-subsystems)

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

Covers D72 and D73, implemented in `x/treasury` ([local design](../x/treasury/README.md#exposure-sampling-and-refresh)).

The multiplier is a controller nobody watches by default. It ships inert — every weight zero, `m = 1` — so
nothing it does is visible until a weight is voted positive, and after that its whole effect is a number that
quietly changes how much expansion principal is retained and how much capital a committee may move. There is no
user-facing symptom when it is working, and none when it is stuck either.

### 2.1 Two phases, and the first one is not alerting

**Phase A — inert (now, until activation).** The weights are zero, so `m` is pinned at one and the composite
carries no information. But the *indicators* are live from the first block: the volatility and flow series fold
every block inside `SettleConversions`, and `ExposureStatus` reports both alongside the liability ratio at each
update. This is the calibration window, and it is the whole reason the design ships disabled.

The work in Phase A is to learn the calm-state distribution of the three indicators on this chain — not to alert
on them. Collect at least a few weeks of `ExposureStatus`, then choose weights so that a calm reading composes
to roughly 1.0–1.1 and a genuinely stressed one approaches the cap. Weights chosen from priors rather than from
this data are guesses; [P1](ECONOMIC_DECISIONS.md#p1) treats them as a launch calibration decision for exactly
this reason, and `docs/GOVERNANCE_OPERATIONS.md` §5 gates the activation message on this window having produced numbers.

Panel 2 of the dashboard (§2.6) is the instrument. Building it is not preparation for calibration; it is the
calibration.

One alert is worth having even in Phase A: **A2, refresh stalled**. A multiplier that cannot recompute is broken
whether or not anyone is reading it, and Phase A is precisely when that would go unnoticed.

**Phase B — activated.** The full alert set applies.

### 2.2 Signal inventory

| Signal | Source | Cadence |
| --- | --- | --- |
| `multiplier` | `ExposureStatus.exposure_state.multiplier`; `EventExposureRefreshed.multiplier` | per refresh |
| `uncapped_multiplier` | `EventExposureRefreshed.uncapped_multiplier` | per refresh |
| `liability_ratio` | `ExposureStatus`; `EventExposureRefreshed` | per refresh |
| `annualised_volatility` | `ExposureStatus.annualised_volatility` (derived); `EventExposureRefreshed` | per refresh / on demand |
| `flow_pressure` | `ExposureStatus.exposure_state.flow_pressure` (absolute NOAH); `EventExposureRefreshed.flow_ratio` (a share of net liability) | per block / per refresh |
| `refresh_pending` | `ExposureStatus.refresh_pending` | on demand |
| `volatility_variance` | `ExposureStatus.exposure_state.volatility_variance` | on demand |
| net and nominal liability, all six targets, fund balances | `treasury/FundStatus` | on demand |
| valuation incompleteness | `EventLiabilityIncomplete` (and the two supply lists it carries) | per settling block, when degraded |
| retained vs burned | `EventExpansionAllocated` | per settling block with expansions |
| buffer payout | `EventRedemptionBufferDrawn` | per settling block with redemptions |
| Reserve recognised capital | `reserve/RecognisedCapital` | on demand |
| Reserve capital requirement | `treasury/FundStatus.strategic_reserve_target` | on demand |

Note the last two. `RequiredReserveCapital` is a keeper function reading the **gross** Reserve target, so
`strategic_reserve_target` is the field that matches it — not `strategic_reserve_net_target`.

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

**First look:** `EventExposureRefreshed` carries all three indicators, so read which one is driving `uncapped`.
The decision that follows is governance's — `ECONOMIC_COMMITTEE_RUNBOOK.md` §5.

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

**First look:** the event carries `stale_member_supply` and `untrusted_suspended_supply`, which say precisely
which members and why. A stale feed and a suspension-without-plan need different fixes. Expect a committee
follow-up if the window was long; `ECONOMIC_COMMITTEE_RUNBOOK.md` §5.

#### A5 — Reserve burnable surplus collapsed · notify the committee

**Condition:** `reserve/RecognisedCapital − FundStatus.strategic_reserve_target` crosses from positive to
non-positive, when driven by a rise in `multiplier` rather than a fall in recognised capital.

**Why:** the committee's burn authority just went to zero without the committee doing anything, because the
requirement moved underneath it. This is the mechanism working correctly — the worst moment to destroy reserve
capital is when the risk model says it is needed — but it will look like a bug to whoever's proposal fails.

**Caveat, and it is a real gap.** This condition models only half of `BurnableSurplus`, which is
`min(recognised − required, balance − mandate.minimum_noah_balance)`. When the mandate's NOAH floor is the
binding term, a burn fails with A5 silent. Watching the floor margin beside this condition closes it; see the
open item in §2.8, because there is no query that reports the composed bound.

**Response:** none required. This alert exists so the committee learns it from a dashboard rather than from a
rejected transaction.

#### A6 — Buffer coverage falling · warn

**Condition:** `FundStatus.redemption_buffer_balance ÷ FundStatus.net_liability` below a governance-chosen floor.

**Why:** this is the outcome measure — what a redeemer actually receives — and it is *not* an exposure signal.
It falls when NOAH weakens, because the denominator is NOAH-valued, which is the reflexivity the multiplier
exists to prepare for. Watching it beside `multiplier` is how you tell whether the preparation is keeping up: a
rising multiplier with flat coverage means retention is not arriving, usually because there are no expansions to
retain from.

**Response:** the designed lever is a committee Reserve-to-Buffer transfer; `ECONOMIC_COMMITTEE_RUNBOOK.md` §3.

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
  is why A2 stays reliable during an A4 incident.
- **Indexer.** `EventExposureRefreshed`, `EventLiabilityIncomplete`, `EventExpansionAllocated`,
  `EventRedemptionBufferDrawn`. All four are typed events, so a generic ABCI event indexer suffices.
- **Retention.** At least 30 refresh periods of update history, for the "N consecutive" conditions and for A5's
  attribution.

The collector must preserve these semantics regardless of its implementation. Delivery status lives in
[future changes](FUTURE_CHANGES.md#protocol-monitoring-tooling).

### 2.8 Calibration and operational ownership

1. **Thresholds.** `N` for A1 and A3, and the coverage floor for A6, are placeholders. Set them from Phase A
   data, in the same pass that chooses the weights.
2. **Where the dashboard lives**, and who receives the A1, A2 and A4 pages. An ops decision, not a protocol one.
3. **A5's attribution** — separating "multiplier rose" from "recognised capital fell" — needs both series
   retained. If that proves awkward, downgrade A5 to a plain surplus-crossed-zero notification; the committee
   still learns what it needs to.
4. **A5 is an indicator, not the execution bound.** It tracks recognised capital against the Reserve target.
   The [economic committee runbook](ECONOMIC_COMMITTEE_RUNBOOK.md#2-before-drafting-a-burn) also applies the mandate's
   NOAH floor when sizing a transaction. Retain that distinction; a positive capital surplus alone does not authorise
   a burn. A proposed bound query is tracked with [future tooling](FUTURE_CHANGES.md#protocol-monitoring-tooling).

## 3. Other subsystems

The process side of oracle attendance and price-feed liveness, what this node signs and what its sidecar serves,
is `docs/PROCESS_MONITORING.md` §8, read from the scrape endpoints rather than from state. The protocol side of both,
and market conversion health, still have on-chain state and typed events that would fit the shape of §2; they get
sections here when someone needs them, rather than a document each.
