# Committee runbook

For the members of an appointed committee, and the governance steps their work interleaves with: what each committee
may do, how to size and preflight it, and what to check afterwards. [Governance runbook](GOVERNANCE_RUNBOOK.md)
owns appointment and governance's own decisions; [protocol monitoring](../operations/PROTOCOL_MONITORING.md) owns alert
conditions, which the sections below answer by ID; each module's README owns the mechanism behind its committee's power.

## Contents

- [1. Every committee](#1-every-committee)
- [2. Security](#2-security)
- [3. Asset emergency](#3-asset-emergency)
- [4. Economic policy](#4-economic-policy)
- [5. Reserve](#5-reserve)
- [6. Conversion](#6-conversion)
- [7. Claims](#7-claims)
- [8. Grants](#8-grants)

## 1. Every committee

| Committee | Mandate query | Committee commands | Mechanism |
| --- | --- | --- | --- |
| Security | `arkd query security security-mandate` | `committee-plan-upgrade`, `committee-cancel-upgrade` | [x/security](../../x/security/README.md#powers-and-rationale) |
| Asset emergency | `arkd query asset emergency-mandate` | `emergency-suspend-asset` | [x/asset §9](../../x/asset/README.md#9-the-emergency-mandate) |
| Economic policy | `arkd query treasury economic-mandate` | `committee-update-policy` | [x/treasury](../../x/treasury/README.md#exposure-sampling-and-refresh) |
| Reserve | `arkd query reserve mandate` | `deploy`, `record-update`, `attribute-return`, `burn-surplus`, `burn-paper`, `fund-redemption-buffer`, `fund-insurance`, and the position corrections | [x/reserve](../../x/reserve/README.md#mandate-custody-and-journal) |
| Conversion | `arkd query market conversion-mandate` | `committee-update-policy`, `committee-set-tobin-tax` | [x/market](../../x/market/README.md#conversion-mandate) |
| Claims | `arkd query claims mandate` | `committee-submit-claim`, `committee-cancel-claim` | [x/claims](../../x/claims/README.md#claims-and-reservation-lifecycle) |
| Grants | `arkd query disbursement grants-mandate` | `committee-registrar`, `committee-suspend`, `committee-reinstate`, `committee-compensate` | [x/disbursement](../../x/disbursement/README.md#committee-awards) |

Every committee message carries `expected_term`, the term the mandate query shows; replacement or reappointment
advances it, so nothing prepared under an earlier appointment lands. Before acting, query the mandate for its term,
window, and usage, then the state the act depends on: balances, bounds, the record it names. A message that carries a
whole policy replaces it, so copy the current object and change only the fields the act is about. A simulation is a
preflight against one state, so a refusal after it calls for fresh reads before resizing or signing again. After
inclusion, confirm the module's event and query the state the act changed.

### Submitting a transaction

Collect threshold signatures off-chain: a public voting contract would disclose the target during deliberation, which
matters most for the Security and Asset emergency committees. A signed act is public once broadcast, so keep the time
from decision to broadcast short.

1. Preflight the unsigned transaction with `arkd tx simulate <unsigned-tx.json> --from <committee-key> --gas auto`;
   for a multisig, `--dry-run` uses a single-key placeholder and is not a reliable preflight. Check the account
   sequence and the current mandate term, and price gas from `arkd query treasury gas-price anoah`: local
   `minimum-gas-prices` does not affect admission.
2. Sign offline, `arkd tx sign` per member, and assemble with `arkd tx multi-sign`. Changing gas or fees means
   rebuilding and signing again.
3. Broadcast with `arkd tx broadcast <signed-tx.json> --broadcast-mode sync` and require `code: 0`; any other code
   means the transaction is not queued.
4. Confirm with `arkd query tx <hash>` and the module's event. Until it lands, resubmit the same signed bytes while
   they remain valid: CometBFT's cache forgets rejected transactions, and an asynchronous response is not proof of
   admission.

Committee messages ride the committee lane, which reserves a share of pool storage and block space ([mempool
policy](../../app/mempool/README.md#admission-service-and-sdk-behaviour)), so an incident's transaction reaches a block
under congestion. A contract root's transactions ride the normal lane instead
([governance runbook §3](GOVERNANCE_RUNBOOK.md#3-committees)).

## 2. Security

The committee schedules or cancels its own chain upgrade, so a fix ships in hours rather than after a vote. It holds no
funds and no freeze, and it can never touch an upgrade governance scheduled.

1. Confirm the slot. `arkd query upgrade plan` must be empty or hold the committee's own plan, which
   `arkd query security committee-plan` reports with `matches_pending`. A governance plan is out of reach.
2. Schedule with `arkd tx security committee-plan-upgrade NAME HEIGHT`. `NAME` is the plan the new binary registers
   ([app/upgrade](../../app/upgrade/README.md)); the height leaves operators time to build or verify the binary and
   install it under the [upgrade binary policy](../operations/NODE_OPERATIONS.md#upgrade-binary-policy). There is no
   minimum lead time, and scheduling approves no binary: publish the release through the agreed release process at
   the same time.
3. To move the height, schedule again; a schedule identical to the pending plan is refused. To withdraw, send
   `arkd tx security committee-cancel-upgrade`, which reaches only the committee's own plan.

Until the upgrade executes, `committee-plan` shows `matches_pending: true`. At the height every node halts until it
runs the named binary.

## 3. Asset emergency

The committee suspends a failing registry asset: issuance, conversion, and ordinary redemption stop at once, while
balances and transfers carry on. Until it executes the asset redeems at its full reference rate, so the committee's
test is mustering its threshold in minutes at any hour. Suspension moves no value and governance reverses it by
recovery; the committee has no other power.

1. Check `arkd query asset emergency-mandate` for an active window, the term, and the denominations already suspended
   this term: each asset admits one committee suspension per term, and re-suspending one governance recovered takes
   governance. `arkd query asset asset DENOM` must show `ACTIVE` or `ISSUANCE_HALTED`. No version is needed; the term
   is the staleness guard.
2. Send `arkd tx asset emergency-suspend-asset DENOM TERM`.
3. Confirm the asset reads `SUSPENDED`, then hand it to governance: a settlement plan for its holders, or recovery once
   the feed is stable ([lifecycle operations](../../x/asset/README.md#8-lifecycle-operations)).

## 4. Economic policy

The committee writes `EconomicPolicy` with `arkd tx treasury committee-update-policy`. The message carries the whole
policy, so copy `arkd query treasury economic-policy` first. Each field must lie inside the mandate's
`minimum_policy` and `maximum_policy`, inclusive, or the whole update is refused; nothing is clamped. Eight fields, in
three groups:

| Group | Fields | What it sets |
| --- | --- | --- |
| Block rewards | `validator_block_reward_target`, `oracle_block_reward_target` | what the protocol pays out |
| Fund ratios | `redemption_buffer_target_ratio`, `strategic_reserve_target_ratio`, `insurance_target_ratio` | how much capital it holds against liability |
| Exposure weights | `liability_ratio_weight`, `volatility_weight`, `flow_weight` | how much extra a unit of measured risk demands |

The mandate bounds all eight independently. A weight's minimum may sit above zero, which keeps the committee from
switching the exposure model off; a minimum of zero grants it that power for the appointment. Weights stay at zero
until the calibration window has produced thresholds ([genesis §14](GENESIS.md#committee-appointments)).

**The transfer tax is not a committee lever.** `transfer_tax_rate` lives in `Params` and moves only by
governance vote: it is part of the fee every wallet signs, so a raise refuses every in-flight
transfer and must sit behind a voting period.

Governance keeps the machinery behind the weights too — `volatility_decay`, `volatility_sample_seconds`, `flow_decay`,
`multiplier_cap`, `multiplier_max_step`, `exposure_refresh_period_blocks`. A holder who could set the step could
reach the cap in one update; a series folded under one memory is not recoverable by restoring the old decay. A
committee at its maximum moves the multiplier by at most one step per period toward a governance-set cap, which is
exactly what organic stress already produces.

The ratios and weights reach the Reserve committee: raising them raises the targets, which shrinks its burn limit and
widens the Buffer and Insurance shortfalls (§5). Read `arkd query treasury exposure-status` and `fund-status` before
and after.

## 5. Reserve

The committee deploys Reserve NOAH to approved destinations and attests what they hold, burns registry paper and NOAH
surplus, and moves NOAH into the Redemption Buffer and Insurance. Query `arkd query reserve mandate`, `balance`,
`recognised-capital`, and `burnable-surplus` before each act.

**Why the limits move.** Three of the committee's limits — what it may burn, what it may move into the Buffer, what it
may move into Insurance — are not fixed by its mandate. They are computed at execution against the exposure multiplier,
which Treasury computes and the committee does not control
([mechanism](../../x/treasury/README.md#exposure-sampling-and-refresh)). A proposal that was correctly sized when it was
drafted can fail on submission with nothing in the Reserve having changed. That is the mechanism working as designed.

### Burns

**Check `ExposureStatus` first.** The burn bound is evaluated when the transaction executes, against the
multiplier *then* — not against the one that held when the proposal was written. A surplus sized last week can
be zero today with no change to the Reserve's own holdings.

**Read the bound with `reserve/BurnableSurplus`** (`arkd query reserve burnable-surplus`). It computes the
handler's own bound: the largest burn after which recognised capital, recomputed at the balance the burn leaves,
still covers the requirement, and never more than the balance above the mandate's NOAH floor. Clipped stakes and the
floor are both included, so the figure is exact at the queried height. The handler recomputes it at execution, so
leave margin for the multiplier and prices to move between the query and inclusion, then send `burn-surplus`.

**A refused burn is not necessarily an error.** If `RequiredReserveCapital` errors rather than returning a
number, the liability valuation is incomplete and the refusal is deliberate: a fund that cannot size its
requirement must not dispose of capital. `BurnableSurplus` fails in the same condition rather than reporting zero.
Check for alert A4 before treating either as a bug.

`burn-paper` retires registry paper in custody that carries no credit. It retires liability, cannot lower recognised
capital, and needs no surplus.

### Transfers into the Buffer and Insurance

These get **easier** under stress, not harder. Both are bounded by a shortfall against a target, and both
targets widen with the same multiplier that shrank the burn bound. This is the intended fast lever during a run.
Send `fund-redemption-buffer` or `fund-insurance`.

Two things still bind:

- The mandate's `minimum_noah_balance` applies to transfers exactly as it does to burns.
- The Insurance shortfall sizes against Insurance's *recognised* capital, not its raw balance, so an approved
  pending claim has already encumbered part of it and the gap is wider than the balance suggests.

Unlike the burn bound, these shortfalls size on whatever the block could value, however incomplete — because the
coverage draw spends the Buffer against that same aggregate.

### Deployment and attestation

Deployment (`deploy`) honours the static NOAH floor, the term `deployment_allowance`, and the `destinations` list, and
is deliberately **not** surplus-bounded. The multiplier does not gate converting NOAH into external assets:
deployment keeps haircut recognition credit and is the fund's mandate, so bounding it by the requirement would
penalise the fund for doing the thing it exists to do.

**Re-attest every position inside its window.** Each eligibility entry states a `max_attestation_age`. A position
whose whole holding the committee has not stated within it earns no credit, which reopens the Reserve's gap and pulls
expansion principal back in. Before the window closes, obtain the custodian's statement and send `record-update`
with the holding it shows, the same quantity if nothing changed, citing the statement as `reference`, which is
required. A return attribution re-attests the remainder too; funding a position does not.
`reserve/RecognisedCapital` reports lapsed holdings as `stale_quantity`, and each position's `attested_time` plus the
entry's window is when its credit lapses.

### Alerts

Conditions and diagnostics are in [protocol monitoring §2.4](../operations/PROTOCOL_MONITORING.md#24-alerts).

**A5 — burnable surplus collapsed.** No action. The alert exists so the committee learns its authority moved
before it drafts against the old number.

**A6 — buffer coverage falling.** The designed lever is a Reserve-to-Buffer transfer, whose cap is
`RedemptionBufferShortfall` — itself widened by the same multiplier. Read it beside `multiplier`: a rising
multiplier with flat coverage means retention is not arriving, usually because there are no expansions to retain
from, and no amount of waiting will change that.

## 6. Conversion

The committee moves Market's depth, recovery period, and spread floor inside its corridor with
`arkd tx market committee-update-policy`, which carries the whole `ConversionPolicy`, so copy
`arkd query market conversion-policy` first. It sets a denomination's Tobin tax between the default and the mandate's
cap with `committee-set-tobin-tax`. Governance moves policy beyond the corridor with `MsgUpdatePolicy`. A reference
change closes the corridor until governance reappoints the committee in the same proposal
([governance runbook §3](GOVERNANCE_RUNBOOK.md#3-committees)).

### Sizing depth

Market's depth is the throttle on how fast NOAH mints and burns, and nothing raises it automatically. The committee
and governance size it by one rule, in the pool unit, valuing every currency at the oracle rate:

```text
depth      = max(100 × daily net one-way flow, 50 × largest routine swap)
daily flow = liquid NOAH × NOAH price × share converting in a month ÷ 30
```

The factors hold at a 2% floor and a one-day recovery; in general they are `2 / floor × recovery period in days` and
`1 / floor`. [Market's quote contract](../../x/market/README.md#local-quote-and-admission-contracts) derives them.

**When to run it.** With a live NOAH price before conversion opens
([opening conversion](GOVERNANCE_RUNBOOK.md#opening-conversion)), when a grant tranche adds liquid NOAH, and
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

## 7. Claims

The committee pays covered losses from Insurance. A claim waits out `claim_cancellation_period`, a week of block time
at launch, and EndBlock pays it at its closing time with no further signature. Governance submits and cancels claims
too, with no mandate.

1. Check `arkd query claims mandate` for the term, the `committee_claim_limit` in the reference unit, and what the term
   has used, and `arkd query claims balance` for Insurance's custody and reservations. A committee claim needs a fresh
   oracle rate for its denomination and the reference, so through an oracle outage only governance submits.
2. Submit with `arkd tx claims committee-submit-claim`, naming the recipient, the amount in NOAH or an active registry
   asset, and a reference to the off-chain case record, at most 2,048 bytes. A stablecoin claim funds itself at
   submission, from custody first and then by buying with Insurance NOAH; size its `max_offer` from
   `arkd query market buy AMOUNTDENOM anoah` for the whole amount. The limit is checked at face value before the buy
   and charged what the claim drew after.
3. `arkd query claims claim ID` shows the closing time and the charge. Until the closing time,
   `arkd tx claims committee-cancel-claim ID` cancels it; after it, the claim pays. A recipient the chain cannot pay
   fails the claim and releases its reservation; the remedy is a fresh claim naming a payable recipient.

A claim's reservation spans appointments, so replacing the committee frees nothing a pending claim was promised.

## 8. Grants

The [disbursement plan](DISBURSEMENT_PLAN.md) uses the native [Disbursement module](../../x/disbursement/README.md),
which holds the member and contributor accounts from genesis. Inspect `arkd query disbursement params` and
`arkd query disbursement balance anoah`, which shows each account's pool and the member tranche, before proposing. `GOV`
below is the governance authority; a tranche opens the member pool alone, since the contributor pool has none. No
contract upload, instantiation, or sudo is involved. Amounts are denomination base units and timestamps are block time.

**A member tranche** is a `MsgOpenTranche` on the member account beside a `MsgUpdateParams` carrying the step's member
amount; it moves no coins, and members already registered keep the terms they joined under. Appoint the grants committee
with `/ark.disbursement.v1.MsgSetGrantsMandate`, naming the exact committee account, a half-open window of block time in
Unix seconds, and its award power: a compensation allowance in the reference unit; nothing pays before one period after
an award, a fortnight at launch in the example; an appointment that delegates no awards leaves the allowance zero.
Ownership is awarded by proposal only. The appointment is its own message, so operational params never travel with it;
every appointment advances the term the committee's transactions must carry, shown by
`query disbursement grants-mandate`. Launch artifacts set a 20,000 NOAH member amount and leave the mandate disabled;
the ownership cap is a param, 15,000,000 NOAH at launch, and founding identities are not.

```json
{
  "@type": "/ark.disbursement.v1.MsgSetGrantsMandate",
  "authority": "GOV",
  "committee": "COMMITTEE",
  "activation_time": "ACTIVATION",
  "expiry_time": "EXPIRY",
  "compensation_allowance": "250000000000000000000000"
}
```

```json
{
  "title": "Member tranche, step 2, first quarter",
  "summary": "Fifteen million NOAH for one thousand member grants of fifteen thousand, the first quarter of step 2's member budget.",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [
    {
      "@type": "/ark.disbursement.v1.MsgUpdateParams",
      "authority": "GOV",
      "params": {
        "member_amount": "15000000000000000000000",
        "member_schedule": {
          "first_length": "1209600", "first_weight": "13",
          "period_length": "2419200", "period_weight": "9", "periods": 13
        },
        "compensation_cap": "1000000000000000000000000",
        "compensation_window": "31449600",
        "compensation_period": "1209600",
        "ownership_cap": "15000000000000000000000000",
        "ownership_schedule": {
          "first_length": "31449600", "first_weight": "26",
          "period_length": "1209600", "period_weight": "1", "periods": 78
        }
      }
    },
    {
      "@type": "/ark.disbursement.v1.MsgOpenTranche",
      "authority": "GOV",
      "amount": "15000000000000000000000000"
    }
  ]
}
```

`MsgUpdateParams` replaces the whole params object, so copy the current values from `arkd query disbursement params`
and change only `member_amount`; the compensation and ownership terms stay unless one of them is the proposal. Step 1
opens at the launch artefact's amount, so its proposals need only the tranche; each later step's first tranche carries
both messages and its later ones the tranche alone, and the plan's schedule runs 20,000, 15,000, 10,000, 7,500, 5,000.
A step's first proposal is due as the open tranche nears exhaustion. Its summary states the previous step's
registrations, suspensions, and cancellations with any admissions change they prompted, and holds the new amount's
tenth under the price of a bought identity at the oracle's NOAH rate ([plan §4](DISBURSEMENT_PLAN.md#steps)).
Open each step's member budget in quarters: 7.5M at step 1, 375 members, then 15M, 22.5M, 22.5M, and 49.8M, so
twenty tranche votes cover the distribution. The open tranche is all a stolen registrar key can commit, and the two-week
hold is the time to suspend it, so a tranche is the exposure governance accepts; split one rather than grow it.

**The registrar.** The grants committee names the Disbursement registrar under its own term with
`MsgCommitteeRegistrar`, for a window of block time inside its own. The registrar is an operational key, hardware-backed
in the registrar signer, that registers batches as admissions clear; the committee may name itself. The
open tranche is its only bound, and nothing it registers pays before the two-week hold ends: compare the chain's
registrations against the platform's admission records daily, and the moment the key is suspect rotate it by
reappointment, which advances the registrar term without a vote, and suspend what it registered before the hold ends.
Every committee appointment clears the registrar, so a replaced or reappointed committee names its registrar again in
its first transaction; governance reaches the registrar only through the committee.

The committee appoints the registrar with
`arkd tx disbursement committee-registrar --expected-term TERM --registrar REGISTRAR --activation-time ACTIVATION --expiry-time EXPIRY --from COMMITTEE`;
`query disbursement registrar-mandate` shows the appointment, its term, and whether it is active. The registrar then
signs
`arkd tx disbursement register-members --expected-term REGISTRAR_TERM --addresses ADDRESS --addresses ADDRESS --reference REF --from REGISTRAR`,
where the reference names the batch's admission record and carries no personal data; the member roll does not pass
through a proposal. Registration reserves the whole grant and pays nothing; the block sends the tenth when the two-week
hold ends. Look up the permanent grant ID with `arkd query disbursement member ADDRESS`. The chain pays each later
period at the end of the block its boundary falls in, to the recorded payee; nothing is signed to receive it, and
`grant ID` shows `due_time`, the next look.

**A faked member** can be paused with `arkd tx disbursement committee-suspend --expected-term TERM --addresses ADDRESS --from COMMITTEE`.
Governance then cancels by permanent grant ID:

```json
{
  "@type": "/ark.disbursement.v1.MsgCancelGrants",
  "authority": "GOV",
  "grant_ids": ["1", "2"]
}
```

Cancellation retains earned unpaid principal at the effective suspension time, or at execution time if there is no
effective suspension. It returns unearned member funds to the open member tranche for the next member. It attempts no member payment;
retained debt remains payable even after cancellation. `MsgCommitteeReinstate` from the committee or `MsgReinstateMembers`
from governance resumes a suspended grant and permits catch-up; neither can reopen a cancelled grant. A compromised
committee can reinstate members before cancellation, so follow the disbursement plan's prompt cancellation process.

**Registrar recovery** is one committee transaction: `committee-registrar` naming the new key, or an empty one to
disable, advances the registrar term, so nothing prepared under the old key lands afterwards. The committee then
suspends the fakes that `arkd query disbursement grants --registrar-term OLD_REGISTRAR_TERM` lists, before their hold
ends, and governance cancels them by ID as above; cancelled inside the hold, a fake returns its whole grant to the
tranche.

**Committee recovery** takes two proposals. The first carries `MsgSetGrantsMandate` appointing the new committee,
which clears the registrar the old key appointed in the same write, `MsgCancelGrants` for
the old term's awards, a hundred IDs a message, and `MsgReinstateMembers` for any members the old
key wrongly suspended, a hundred addresses a message. `arkd query disbursement grants --committee-term OLD_TERM` lists the
awards and `--registrar-term OLD_REGISTRAR_TERM` the registrations, each `OLD_TERM` being the term its query showed before the vote; legitimate ones can stay off the list. The
appointment advances the term, so nothing prepared under the old key lands afterwards. Once it executes, list the old
term again and cancel, in a second, expedited proposal, any award the key made while the first vote was open: nothing
pays within a period of an award, at least two weeks, and the two votes take about three days at launch. Neither proposal
reverses payments or touches the new term, and suspensions stand until reinstated. Governance cancels known sybils among the
listed registrations in the first proposal. An empty committee disables the mandate without changing existing grants, and a lapsed window does
the same until the next appointment; disabling or replacing the committee clears its registrar, and the new committee
names one again before registration resumes.

**A contributor award** is a `MsgGrantOwnership` or `MsgCompensate` drawing on the contributor pool, which has no
tranche; the step's contributor budget is the plan's guideline, not a limit; an award past it states the overrun in the
summary. The award carries no schedule: ownership takes the shape in params, a one-year cliff then biweekly for three
years, and `start_time` places the clock, no earlier than execution. The proposal summary states the work, its period,
and why this amount, against the founder's 15M grant and the grants already made; `reference` identifies the approved
work or agreement on the permanent grant itself. The pool must cover the whole amount before it starts.

```json
{
  "title": "Contributor ownership award",
  "summary": "<the work, its period, and why this amount>",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [
    {
      "@type": "/ark.disbursement.v1.MsgGrantOwnership",
      "authority": "GOV",
      "beneficiary": "BENEFICIARY",
      "amount": "1000000000000000000000000",
      "start_time": "START_UNIX_SECONDS",
      "reference": "<approved contributor agreement>"
    }
  ]
}
```

The clock starts at `start_time`, for ownership no earlier than execution. No payment occurs until a period has
elapsed, and ownership still checks the cap. Founding status is read from genesis seat records; an award has no `seat_holder`
input. The beneficiary initially controls the contributor payee, and later awards preserve the existing controller.
Use `arkd tx disbursement set-controller --beneficiary ADDRESS --controller NEW_CONTROLLER --from CURRENT_CONTROLLER` to
rotate it. Governance can also submit `MsgSetController`, with `sender: GOV`, for recovery.

**Compensation** is `MsgCompensate`, with the funded denomination and `periods`: that many fortnights in equal shares,
one for a bounty, together at most ten years. Its clock may start up to a quarter back, so a stream aligns to the day a
hire started and pays the elapsed fortnights at the first look. Its denomination is NOAH or an active asset in the Asset
registry; `max_offer` bounds the NOAH converted to fund a stablecoin award. Stablecoin compensation is fully funded in
that denomination, untaxed, and outside NOAH ownership caps. It does not promise a conversion rate. A denomination's
later suspension prevents new awards without erasing already funded entitlement. One record's compensation is at most
`compensation_cap` in the reference unit a `compensation_window`, by proposal or committee alike, valued at the fresh
oracle rates when awarded, a stream counting at its yearly rate while it runs and cancelled awards for a window after
them, so an award fails while the oracle has no rate for its denomination or the reference unit.

**Committee awards** pay pay streams and bounties without a proposal; ownership is a proposal only. Within its
appointment the committee signs `MsgCommitteeCompensate` with the beneficiary, amount, `start_time`, `periods`, and a work
reference; nothing pays before one period after the award, whatever the start time, and a stablecoin award draws on custody first and converts
NOAH from the contributor pool for the rest, within the award's `max_offer`, like a governance award. The
compensation allowance caps the term's total at reference value, cancelled awards included.
`query disbursement grants-mandate` shows what the live term has used and what remains, and `query disbursement grants
--committee-term TERM` lists its awards.

**Stablecoin funding** happens in the award. A stablecoin award draws uncommitted stablecoin in the contributor
account first and converts NOAH from the contributor pool for the rest, through Market like any trader's swap,
inside the message that creates the grant. `max_offer` is the most NOAH the award may spend; the module sizes the
smallest offer that funds it, and an award whose cap buys less than it needs fails whole. Size the cap from
`arkd query market buy AMOUNTausd anoah`, Market's reverse quote for the whole pay; the award converts less if custody
covers part. A proposal executes after its vote, so its cap is the worst price governance accepts then.

```json
{
  "@type": "/ark.disbursement.v1.MsgCompensate",
  "authority": "GOV",
  "beneficiary": "BENEFICIARY",
  "amount": {"denom": "ausd", "amount": "200000000000000000000000"},
  "start_time": "START_UNIX_SECONDS",
  "periods": 52,
  "reference": "<approved work>",
  "max_offer": "260000000000000000000000"
}
```

Market settles the conversion at EndBlock like any trader's, and the grant journal's `converted` entry under the grant records
the output and the NOAH offer it cost; Market's own event carries the fee. Stablecoin a cancelled award leaves in custody funds the next award before
anything converts.

**Cancellation and destination recovery.** `MsgCancelGrants` applies to any grant kind. Contributor cancellation
returns unearned NOAH to the contributor pool, leaves unearned stablecoins in custody for the next award, and
retains accrued debt; an ownership cancel also returns the unearned part to the record's room under the cap.
A cancellation sends nothing itself, so a blocked destination cannot veto it; the block pays the retained debt
afterwards and defers a destination that refuses, which `EventPaymentDeferred` reports. A contributor controller, or the original
registered member key for a member grant, signs
`arkd tx disbursement set-payee --grant-id ID --payee ADDRESS --from CONTROLLER_OR_MEMBER`. The beneficiary identity and all
historical payments remain unchanged. The committee has no destination-recovery authority.

**Audit after execution.** Use `arkd query disbursement` with `grant ID`, `beneficiary ADDRESS`, `balance DENOM`, and
`grant-journal --grant-id ID`. The grant journal holds each grant's creation, conversion, payments and their payees, cancellation,
suspensions, and payee changes; `beneficiary-journal ADDRESS` lists a beneficiary's controller changes; governance actions
are recorded in their proposals. All list queries use key pagination; maximum page size is 100. Read all pages when reconciling.
`MsgReturnUnallocated` returns from the contributor account to the community pool without touching any grant's
reserved principal: NOAH from its pool, any other denomination from custody outside grants. The member
account has no exit.
