# Economic committee runbook

For Treasury and Reserve committees sizing bounded economic actions, and governance coordinating a conversion-mandate
reference change. This guide owns action selection and preflight; [protocol monitoring](PROTOCOL_MONITORING.md) owns
alert conditions and diagnosis. Security incident delivery lives in the [emergency runbook](EMERGENCY_SUBMISSION_RUNBOOK.md).

**Why this exists.** Three of the committee's bounds — what it may burn, what it may move into the Redemption
Buffer, what it may move into Insurance — are not fixed by its mandate. They are computed at execution against
the exposure multiplier, a protocol-computed figure the committee does not control and cannot see in its own
mandate. A proposal that was correctly sized when it was drafted can fail on submission with nothing in the
Reserve having changed. That is the mechanism working as designed, and this document is how a committee learns
it somewhere calm rather than from a rejected transaction.

Monitoring is `docs/PROTOCOL_MONITORING.md`; §5 below answers its alerts by ID. The governance message table is
`docs/GOVERNANCE_OPERATIONS.md` §5, and the mechanism itself is [local design](../x/treasury/README.md#exposure-sampling-and-refresh).

Before any action, query the live appointment and its term/window, the relevant policy, balances, and bounds. After
inclusion, verify transaction success and the resulting policy or custody state. A simulation is a preflight against
one state, so a later refusal requires fresh reads before resizing or signing again.

## 1. The levers

The committee writes `EconomicPolicy` through `MsgCommitteeUpdatePolicy`, clamped field by field by the
`minimum_policy` and `maximum_policy` of its mandate. Eight fields, in three groups:

| Group | Fields | What it sets |
| --- | --- | --- |
| Block rewards | `validator_block_reward_target`, `oracle_block_reward_target` | what the protocol pays out |
| Fund ratios | `redemption_buffer_target_ratio`, `strategic_reserve_target_ratio`, `insurance_target_ratio` | how much capital it holds against liability |
| Exposure weights | `liability_ratio_weight`, `volatility_weight`, `flow_weight` | how much extra a unit of measured risk demands |

D79 counts these as six levers by taking the three weights as one; the mandate clamps them as eight
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

**Check `ExposureStatus` first.** `BurnableSurplus` is evaluated when the transaction executes, against the
multiplier *then* — not against the one that held when the proposal was written. A surplus sized last week can
be zero today with no change to the Reserve's own holdings.

**There is no query that reports the bound.** `BurnableSurplus` is a keeper function with no RPC. To
reconstruct it:

```text
surplus   = reserve/RecognisedCapital − treasury/FundStatus.strategic_reserve_target
spendable = reserve/Balance − ReserveMandate.minimum_noah_balance
burnable  = min(surplus, spendable), floored at zero
```

Use `strategic_reserve_target`, not `strategic_reserve_net_target`: the requirement reads the gross target.

**Both terms bind, and the second is easy to forget.** A burn can fail because the mandate's NOAH floor leaves
nothing spendable even while the surplus is comfortably positive. Alert A5 watches only the first term, so this
failure arrives unannounced.

**A refused burn is not necessarily an error.** If `RequiredReserveCapital` errors rather than returning a
number, the liability valuation is incomplete and the refusal is deliberate: a fund that cannot size its
requirement must not dispose of capital. Check for alert A4 before treating it as a bug.

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

Conditions and diagnostics are in `docs/PROTOCOL_MONITORING.md` §2.4. What follows is only the decision.

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

## Reference change and conversion mandate

When changing the protocol reference, put `MsgSetReferenceDenom` before `MsgSetConversionMandate` in the same
governance proposal. Appointment validates the corridor against the live pool unit, so reversing the order fails;
a combined proposal cannot leave only half the pair applied. Allow margin for execution-time rates when sizing the
new depth corridor. The corridor constrains future candidates, so an appointment need not report that it fails to
bracket the live rebased depth; review that explicitly.

The old corridor is not silently reinterpreted in new units. Treasury's corridor has no denomination and requires no
corresponding appointment; its reference-valued state rebases atomically. Query fresh incoming pricing and either
fresh outgoing pricing or the explicitly governed outgoing-rate override under
[Oracle's reference contract](../x/oracle/README.md#13-protocol-reference-denom).
