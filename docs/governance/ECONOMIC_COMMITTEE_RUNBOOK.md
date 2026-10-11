# Economic committee runbook

For Treasury, Reserve, and Market committees sizing bounded economic actions, and governance coordinating a
conversion-mandate reference change. This guide owns action selection and preflight;
[protocol monitoring](../operations/PROTOCOL_MONITORING.md) owns
alert conditions and diagnosis. Security incident delivery lives in the [emergency runbook](EMERGENCY_SUBMISSION_RUNBOOK.md).

**Why this exists.** Three of the committee's bounds — what it may burn, what it may move into the Redemption
Buffer, what it may move into Insurance — are not fixed by its mandate. They are computed at execution against
the exposure multiplier, a protocol-computed figure the committee does not control and cannot see in its own
mandate. A proposal that was correctly sized when it was drafted can fail on submission with nothing in the
Reserve having changed. That is the mechanism working as designed, and this document is how a committee learns
it somewhere calm rather than from a rejected transaction.

Monitoring is `docs/operations/PROTOCOL_MONITORING.md`; §5 below answers its alerts by ID. The governance message table is
`docs/governance/GOVERNANCE_OPERATIONS.md` §3, and the mechanism itself is [local design](../../x/treasury/README.md#exposure-sampling-and-refresh).

Before any action, query the live appointment and its term/window, the relevant policy, balances, and bounds. After
inclusion, verify transaction success and the resulting policy or custody state. A simulation is a preflight against
one state, so a later refusal requires fresh reads before resizing or signing again.

## 1. The levers

The committee writes `EconomicPolicy` through `MsgCommitteeUpdatePolicy`. Each field must lie inside the
`minimum_policy` and `maximum_policy` of its mandate, inclusive, or the whole update is refused; nothing is
clamped. Eight fields, in three groups:

| Group | Fields | What it sets |
| --- | --- | --- |
| Block rewards | `validator_block_reward_target`, `oracle_block_reward_target` | what the protocol pays out |
| Fund ratios | `redemption_buffer_target_ratio`, `strategic_reserve_target_ratio`, `insurance_target_ratio` | how much capital it holds against liability |
| Exposure weights | `liability_ratio_weight`, `volatility_weight`, `flow_weight` | how much extra a unit of measured risk demands |

D79 counts these as six levers by taking the three weights as one; the mandate bounds them as eight
independent fields, which is the number that matters when drafting a policy update.

**The transfer tax is not a committee lever.** `transfer_tax_rate` lives in `Params` and moves only by
governance vote (D78, D80): it is part of the fee every wallet signs, so a raise refuses every in-flight
transfer and must sit behind a voting period.

Governance keeps the machinery behind the weights too — `volatility_decay`, `flow_decay`, `multiplier_cap`,
`multiplier_max_step`, `exposure_refresh_period_blocks`. A holder who could set the step could reach the cap in
one update; a series folded under one memory is not recoverable by restoring the old decay. A committee at its
maximum moves the multiplier by at most one step per period toward a governance-set cap, which is exactly what
organic stress already produces.

## 2. Before drafting a burn

**Check `ExposureStatus` first.** The burn bound is evaluated when the transaction executes, against the
multiplier *then* — not against the one that held when the proposal was written. A surplus sized last week can
be zero today with no change to the Reserve's own holdings.

**Read the bound with `reserve/BurnableSurplus`** (`arkd query reserve burnable-surplus`). It runs the handler's
own solve: the largest burn after which recognised capital, re-solved at the balance the burn leaves, still covers
the requirement, and never more than the balance above the mandate's NOAH floor. Clipped stakes and the floor are
both included, so the figure is exact at the queried height. The handler solves again at execution, so leave
margin for the multiplier and prices to move between the query and inclusion.

**A refused burn is not necessarily an error.** If `RequiredReserveCapital` errors rather than returning a
number, the liability valuation is incomplete and the refusal is deliberate: a fund that cannot size its
requirement must not dispose of capital. `BurnableSurplus` fails in the same condition rather than reporting zero.
Check for alert A4 before treating either as a bug.

## 3. Transfers into the Buffer and Insurance

These get **easier** under stress, not harder. Both are bounded by a shortfall against a target, and both
targets widen with the same multiplier that shrank the burn bound. This is the intended fast lever during a run.

Two things still bind:

- The mandate's `minimum_noah_balance` applies to transfers exactly as it does to burns.
- The Insurance shortfall sizes against Insurance's *recognised* capital, not its raw balance, so an approved
  pending claim has already encumbered part of it and the gap is wider than the balance suggests.

Unlike the burn bound, these shortfalls size on whatever the block could value, however incomplete — because the
coverage draw spends the Buffer against that same aggregate.

## 4. Deployment is unaffected

Deployment honours the static NOAH floor, the term `deployment_allowance`, and the `destinations` list, and is
deliberately **not** surplus-bounded. The multiplier does not gate converting NOAH into external assets:
deployment keeps haircut recognition credit and is the fund's mandate, so bounding it by the requirement would
penalise the fund for doing the thing it exists to do.

## 5. Answering the alerts

Conditions and diagnostics are in `docs/operations/PROTOCOL_MONITORING.md` §2.4. What follows is only the decision.

**A1 — multiplier pinned at the cap.** Decide deliberately, by governance vote: raise `multiplier_cap`, or
accept that the model is saturated and act on the underlying exposure directly. Do not raise the cap
reflexively. A saturated model during a real crisis may be telling the truth, and raising the ceiling to make
the alarm stop is how a risk model gets talked out of its own finding.

**A4 — sustained incomplete valuation.** The two supply lists on the event say whether the cause is a stale feed
or a suspension without a settlement plan; they need different fixes. Expect a Reserve-to-Buffer proposal as
follow-up if the window was long, because retention accumulated in the Reserve rather than filling targets
throughout — the whole block's expansion principal parks there while valuation is incomplete.

**A5 — burnable surplus collapsed.** No action. The alert exists so the committee learns its authority moved
before it drafts against the old number.

**A6 — buffer coverage falling.** The designed lever is a Reserve-to-Buffer transfer, whose cap is
`RedemptionBufferShortfall` — itself widened by the same multiplier. Read it beside `multiplier`: a rising
multiplier with flat coverage means retention is not arriving, usually because there are no expansions to retain
from, and no amount of waiting will change that.

**A2 and A3** are engineering conditions with no committee action. A2 is a stalled refresh, A3 a rate limiter
binding; both are diagnosed against `FundStatus` and the oracle, not decided by vote.

## Conversion depth

Market's depth is the throttle on how fast NOAH mints and burns, and nothing raises it automatically (D95). The
Market committee moves it with `MsgCommitteeUpdatePolicy` inside its corridor; governance moves it beyond with
`MsgUpdatePolicy`. Both size it by one rule (D104), in the pool unit, valuing every currency at the oracle rate:

```text
depth      = max(100 × daily net one-way flow, 50 × largest routine swap)
daily flow = liquid NOAH × NOAH price × share converting in a month ÷ 30
```

The factors hold at a 2% floor and a one-day recovery; in general they are `2 / floor × recovery period in days` and
`1 / floor`. [Market's quote contract](../../x/market/README.md#local-quote-and-admission-contracts) derives them.

**When to run it.** With a live NOAH price before conversion opens
([opening conversion](GOVERNANCE_OPERATIONS.md#6-opening-conversion)), when a grant tranche adds liquid NOAH, and
when `Query/Pool` shows `ark_pool_delta` held more negative than a hundredth of `base_pool`, which is expansion flow
paying above the floor.

**Before raising.**

- A positive `ark_pool_delta` is redemption pressure. Do not raise into it: a deeper pool retires currency into NOAH
  faster, the May 2022 Terra proposal the whitepaper records (§2.3).
- A mint at a wrong price stays profitable up to roughly the mispricing times the depth. If that is more than the
  venues pricing NOAH justify, raise by less; flow above the sizing still converts, at the spreads below.
- Capacity is added by depth, never by shortening `pool_recovery_period`.

| Daily one-way flow over depth | Marginal spread |
| --- | --- |
| 1% | 2%, the floor |
| 2.5% | about 5% |
| 5% | about 10% |

A depth change rescales the delta in proportion, so a raise keeps the imbalance it found. After inclusion, verify
with `Query/Pool` and `EventPoolUpdated`.

## Reference change and conversion mandate

When changing the protocol reference, put `MsgSetReferenceDenom` before `MsgSetConversionMandate` in the same
governance proposal. Appointment validates the corridor against the live pool unit, so reversing the order fails;
a combined proposal cannot leave only half the pair applied. Allow margin for execution-time rates when sizing the
new depth corridor. The corridor constrains future candidates, so an appointment need not report that it fails to
bracket the live rebased depth; review that explicitly.

The old corridor is not silently reinterpreted in new units. Treasury's corridor has no denomination and requires no
corresponding appointment; its reference-valued state rebases atomically. Disbursement's compensation figures, the cap,
every award's value and charge, and the live grants term's usage and allowance, rebase atomically too, so the grants
committee needs no reappointment, and so do the Claims committee's limit, every committee claim's charge, and the live
term's usage. Query fresh incoming pricing and either
fresh outgoing pricing or the explicitly governed outgoing-rate override under
[Oracle's reference contract](../../x/oracle/README.md#13-protocol-reference-denom).
