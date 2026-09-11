# Economic design

This reference explains Ark's current economic model: ownership, custody, conversion flows, liability, capital targets,
fees, and reward funding. It is intended for protocol contributors and readers evaluating how funds and authority move.
[Decision history](ECONOMIC_DECISIONS.md) owns D/P records; [genesis](../governance/GENESIS.md) owns launch settings;
[governance operations](../governance/GOVERNANCE_OPERATIONS.md) owns the procedures for changing them.

## Contents

- [1. The design in one page](#1-the-design-in-one-page)
- [2. Reading this design](#2-reading-this-design)
- [3. Terminology](#3-terminology)
- [4. Ownership boundaries](#4-ownership-boundaries)
- [5. Accounts and custody](#5-accounts-and-custody)
- [6. Conversion flows](#6-conversion-flows)
- [7. Capital: liability, targets, recognition](#7-capital-liability-targets-recognition)
- [8. Transfer tax](#8-transfer-tax)
- [9. Validator and Oracle funding](#9-validator-and-oracle-funding)
- [10. Authority and reporting](#10-authority-and-reporting)
- [11. Transfer surfaces: IBC and Wasm](#11-transfer-surfaces-ibc-and-wasm)
- [12. Related references](#12-related-references)

## 1. The design in one page

Ark has no routine NOAH issuance. There is no `x/mint`, no staking-reward inflation, no seigniorage, and no fund that
mints to reach a target (D1). Market is the only module holding `Minter`, and it mints and burns only inside
stablecoin conversion settlement (D2). Every other flow moves NOAH that already exists.

Three funds stand behind the stablecoins, each a module account holding NOAH and nothing else at launch:

- **The Redemption Buffer** is conversion inventory. Each redemption draws the Buffer's coverage share of its quoted
  NOAH from inventory and mints the rest (D4), so the Buffer recycles NOAH retained from earlier expansions instead of
  letting every redemption dilute.
- **The strategic Reserve** is governed emergency capacity. It is never debited by a redemption; governance may commit
  NOAH from it to the Buffer, and a committee may deploy it under a bounded mandate (D16, D27, D56).
- **Insurance** pays covered losses through recorded claims under a governance-owned mandate (D15, D30).

The funds are filled by expansion. When NOAH converts to a stablecoin, Market escrows the gross offer and Treasury
routes it once per block down a waterfall: Buffer gap, then Reserve gap, then Insurance gap, then the overflow is
burned (D5, D33). Spread and dust travel with the principal (D6). Each fund's target is a governed ratio of the
stablecoin liability, scaled by a bounded stress multiplier (D72), and targets only decide where new principal goes:
they never trigger a mint, a trade, or a withdrawal (D7).

Security and the price feed are funded from a separate circuit. Gas fees flow through Distribution as usual. A fixed
transfer tax on every user-facing stablecoin transfer (D18) accumulates for one funding window, then meets the Oracle
reward target first and any validator gap second, with a genesis-seeded, non-minting subsidy pool covering what
remains (D9 to D12). No controller adapts the tax rate, the targets, or the Buffer share to prices or revenue.

Authority is split by role. Governance holds every unbounded power. One threshold-multisig committee per module holds
a bounded, height-scoped mandate: the economic-policy committee moves the reversible levers between governance-set
bounds; the Claims committee submits claims within a term allowance; the Reserve committee deploys and keeps the
books; Market's conversion committee moves spread and Tobin policy inside a corridor. Every role has its own address
and its own messages, and none holds general Bank authority (D31, D36).

Market's virtual pool is denomination-bearing and launches in `axdr`; one governance message can re-denominate it live
to a future basket without changing which stablecoins may be converted (D22, D23).

### 1.1 What the feedback loop is, and is not

The design keeps a bounded endogenous response to conversion flow and removes every amplifier Terra carried:

- Expansion can contract NOAH supply, because principal above the three targets burns. Redemption can expand it, by
  the share the Buffer's coverage does not fund. The coverage draw recycles retained NOAH without a first-come
  depletion cliff.
- The response is block-granular (D33). Every expansion in a block allocates under one liability valuation and every
  redemption draws at one coverage ratio, settled once from Market's EndBlocker. Nothing about a quote, payout, or
  spread changes with it; what it removes is intra-block path dependence, so there is no within-block ordering to
  compete for.
- The Reserve is never an automatic Market source. Governance commits NOAH to the Buffer by proposal; Market cannot
  request, size, or trigger it, and no redeemer is paid from the Reserve directly.
- Virtual-pool pricing penalises sustained one-way flow and remembers imbalance; time-based recovery limits how fast
  cheap capacity returns.
- No adaptive reward target, tax-rate controller, price-reactive Buffer share, or automatic Reserve trigger exists. The
  reward waterfall allocates only tax already collected and subsidy already issued, inside hard balance bounds.
- The exposure multiplier (D72) is a bounded target controller and leaves every claim above intact. It moves one
  number, the liability basis the three targets are sized on, which decides how much expansion principal is retained
  rather than burned and how much capital a committee may move or destroy. It mints nothing, triggers no trade, and
  changes no quote, spread, payout, or draw, since the draw's basis stays raw by D73. It is floored at one, so its
  inert state is exactly the unscaled sizing, capped by governance, and rate-limited per update.

A Reserve-to-Buffer commitment is a reclassification of NOAH that already exists, not a controller: the Reserve falls
by the amount, the Buffer rises by it, and supply, liabilities, quotes, spreads, and pool state do not move. The lower
Reserve balance reopens its passive gap, so later expansion principal can rebuild it under the ordinary waterfall.
That refill cannot cause expansion or mint NOAH, but repeated commitments delay Insurance funding and overflow burn,
which each proposal states.

The Buffer and the virtual pool therefore buffer, dampen, and rate-limit Terra-style supply feedback. They do not
remove it. Buffer-funded redemption releases dormant NOAH into circulation, while residual minting raises both
circulating and total supply. The Buffer reduces dilution; it is not collateral, backing, or solvency capital, and the
protocol never presents it as one (D19). Eliminating the feedback outright would mean retaining enough NOAH to fund
every redemption and refusing any uncovered output, which is a redemption gate and a strong first-mover incentive. Ark
keeps unconditional settlement with a continuous proportional minting response instead.

## 2. Reading this design

Read terminology, ownership, and custody before the conversion and funding flows. The sections below describe the
current economic contract. [Economic decisions](ECONOMIC_DECISIONS.md) preserves the D/P records and their amendments;
[genesis](../governance/GENESIS.md) owns the launch values. Local implementation and rationale live in the
[subsystem READMEs](../../app/README.md#module-entry-points).

## 3. Terminology

### 3.1 Routine issuance

NOAH minted independently of a user conversion: staking inflation, block rewards, Oracle rewards, seigniorage, a
fund topping itself up to target, deficit funding. None exists on Ark.

### 3.2 Conversion issuance

NOAH or a stablecoin minted as the output of an atomic Market conversion while the offered asset is burned. This is the
only issuance path, and only Market has it.

### 3.3 Expansion principal

For a NOAH-to-stablecoin conversion, the NOAH value of the final integer stablecoin output after spread, converted at
the quote's own rates and rounded down. Market derives it in the conversion, because the conversion already holds
those rates to charge the spread, and refuses a conversion whose principal exceeds the gross offer: the stablecoin
minted is never worth more than the NOAH offered (D33). Since D6 the waterfall runs on the gross offer rather than on
this figure, so the principal is the eligibility bound, not the allocation input.

### 3.4 Spread and dust

The gross NOAH offered less the expansion principal: the spread the quote charged plus integer dust. It enters the
waterfall with the principal, fund targets first and overflow burned (D6). With the funds at target it burns exactly as
an unconditional burn would have; with a fund short it is capital arriving as the requirement rises. Redemption spread
is NOAH never issued and stays unissued.

### 3.5 Subsidy pool

The `treasury_subsidy_pool` account's NOAH. Seeded at genesis, it has no mint permission and covers only the aggregate
gap between organic validator and Oracle funding and the accumulated per-block targets at each completed funding
window. Anyone may extend it by sending `anoah`; a deposit changes neither total supply nor the targets, only how long
coverage lasts. There is no refill, target balance, refund, withdrawal, or conversion path (D9, D25).

### 3.6 Redemption Buffer

The `treasury_redemption_buffer` account's NOAH: inventory retained from earlier expansions. Each redemption receives
the Buffer's pre-trade coverage share of its quoted output, measured against the claimable liability, the supply that
can currently redeem. Supply that cannot claim, a member whose feed is stale or suspended supply without an activated
plan, is excluded from that denominator and disclosed, so a suspension elsewhere never switches the Buffer off for the
healthy exits it exists to dampen (D4). It is conversion inventory, not collateral, solvency capital, or a first-come
pool. Anyone may deposit `anoah`; a deposit raises inventory under the same formula and may reduce later minting, but
changes no quote, spread, minimum receive, or eligibility and creates no claim on the fund.

### 3.7 Strategic Reserve

The `strategic_reserve` account, owned by `x/reserve`. It is never debited by a redemption and never pays a redeemer,
a reward, or a claim (D16). Its outbound paths are the governed commitments to the Buffer and to Insurance, the
committee's deployments and bookkeeping under a bounded mandate, and burns split by what they can destroy (D27, D56,
D61). The account admits NOAH and asset-registry members by membership alone (D65, D70): Ark paper is protocol
liability wherever it sits and the Reserve is the one account whose committee can retire it. External symbols are
refused at the restriction, permanently; an external holding is attested at its destination and never sits in the
account. Derecognised transfer tax routed here at settlement is inert custody: it earns no credit and waits for a
governed burn.

### 3.8 Insurance

The `claims_insurance` account, owned by `x/claims`, used only for covered-loss payments through recorded claims. It
is never a redemption source and receives and pays `anoah` alone. The Claims committee is an ordinary threshold
multisig appointed for one termed, half-open height window with a fixed gross claim allowance; it signs typed
submissions and cancellations and holds no custody, module permission, or generic send. Governance owns the mandate,
submits without consuming the committee's allowance, and may cancel any pending claim during the shared cancellation
period, which is an `x/claims` parameter (D30, D38, D54).

### 3.9 Market pool denomination

Market's `BasePool` is a denomination-bearing `sdk.DecCoin` and `ArkPoolDelta` is a signed decimal in that unit; the
delta never exists without its unit being observable. The pool unit is the protocol reference denomination, which
`x/oracle` owns and which launches as `axdr`. It is not Treasury's liability numeraire, which is NOAH throughout
(D24); a stablecoin's convertibility does not depend on it; and Treasury's tax-cap reference shares the unit but not
the amount (D21, D22).

### 3.10 Live pool-unit transition

Re-pointing the reference is one Oracle message, `MsgSetReferenceDenom`, which runs Treasury's and Market's rebase in
the same transaction or fails whole. Market converts the stored `BasePool` into the new unit at one fresh Oracle
snapshot and rescales `ArkPoolDelta` by the same factor, so `delta / BasePool.Amount` is preserved and no unit change
resets imbalance or reprices without flow (D23). Market's own policy messages move the amount, the spread, and the
recovery period, and refuse a denomination change, so the two units cannot drift apart. A depth change rescales the
delta the same way. There is one virtual pool, no transition object, and no parallel state; the transition changes no
stablecoin's eligibility and `axdr` stays convertible in both directions.

## 4. Ownership boundaries

| Component | Owns | Never owns |
| --- | --- | --- |
| Market | Quotes, spread, final integer output, the denomination-bearing virtual pool, conversion escrow, mint and burn, atomic settlement, the conversion mandate | Tax policy, fund targets, allocation decisions |
| Treasury | Tax policy and the one calculator, liability valuation, the three fund targets and the exposure multiplier, the expansion waterfall and coverage draw, reward funding and the subsidy pool, the base-fee controller, the economic-policy mandate; custody of the subsidy pool, the Buffer, and the tax collector | Quotes, pool state, gross conversion custody, mint or burn, claim adjudication, any fund's `recognised_capital` |
| Claims | The Claims mandate, the claim record, the Insurance reservation, `claims_insurance` custody, Insurance `recognised_capital` | Tax, targets, the waterfall, liability valuation, the Reserve |
| Reserve | `strategic_reserve` custody, the Reserve mandate, the quantity journal and positions, the recognition policy, Reserve `recognised_capital`, the governed commitments out of the Reserve | Tax, targets, the waterfall, liability valuation, claims |
| Oracle | Consensus rates, the feed registry, the reference denomination, attendance and participation scores, Oracle reward allocation | Fiscal allocation, minting |
| Asset | The registry and lifecycle of every non-NOAH Bank asset, settlement plans, the emergency-suspension mandate | Pricing, custody, allocation |
| Distribution | Validator and delegator fee accounting and payouts | Tax classification, any fund |
| App ante and policy router | Fee declaration and settlement, priority, the transfer-tax charge for signed transactions and for contract, ICA, and GMP dispatches | Target-based allocation, a second tax formula, persistent fiscal state |
| Governance | Every unbounded power: Params of every module, every mandate, claims, Reserve commitments and burns, the recognition policy, feeds, the reference denomination | Routine operation, automatic controllers |
| Committees | One bounded, expiring power each, on a distinct address | Anything another role holds; general Bank authority |

Two rules keep the boundaries one-way. Market depends on Treasury and Treasury receives no Market keeper: Market's
EndBlocker hands Treasury the block's conversion totals and receives a burn amount back, and that return value is how
Market finishes settlement (D33). Treasury exposes no operation on Market state, so it cannot initiate or intermediate
a pool change. Treasury sizes every fund's requirement and each fund's module reports what it recognises through one
number-or-refuse interface, `RecognisedCapital`; the Reserve reads Treasury's requirement and shortfalls back only to
bound its own burns and commitments (D54, D55). No module holds debit authority over another's account.

## 5. Accounts and custody

The module accounts, their permissions, and their inbound rules are tabulated in `docs/governance/GENESIS.md` §3. The rules that
produce that table:

- Only Market holds `Minter`, and it also holds `Burner` for settlement. `strategic_reserve` holds `Burner`, never
  `Minter`, because governance and the committee may destroy custody but nothing on the chain may create it there
  (D2, D61).
- The blocked-address list is replaced, not augmented, so every blocked account is named and the four custody accounts
  are deliberately left reachable by ordinary sends.
- Each custody owner provides a recipient-aware Bank send restriction over its own accounts: Treasury over the subsidy
  pool and the Buffer, `x/claims` over Insurance, `x/reserve` over the Reserve. Bank collects them in a named order,
  and a module providing one must appear in that order or the app fails to construct. Owning the restriction beside
  the account is why the Reserve's rule could diverge from the other three without touching them.
- The subsidy pool, the Buffer, and Insurance admit only a positive, `anoah`-only credit; a mixed or non-NOAH credit
  fails atomically after the whole restriction chain, whatever surface it came through (D25, D26). The Reserve admits
  NOAH and registry members by membership and refuses everything else (D65, D70); that membership rule is what admits
  the derecognised transfer tax settlement routes there, which the earlier collector exemption existed for.
- Deposits ride ordinary Bank rails. They need no module message, receipt, or ledger, are accepted above target, and
  grant no refund, ownership, withdrawal, coverage, priority, governance, or deployment right. A taxable stablecoin
  deposit is taxed like the same transfer to any other account; the recipient creates no exemption.
- Bank balances are the source of truth for every fund. No module stores a fund balance, a target, or a deposit
  record; Claims stores encumbrance and authorisation, and the Reserve stores quantities, never values.

## 6. Conversion flows

Every conversion settles in two moments. The transaction does what depends only on its own quote: escrow, mint, burn,
payout. The block's EndBlocker does what depends on liability or fund state: one liability valuation, one waterfall
over the block's expansion, one coverage draw over the block's redemptions (D33). Market owns custody, mint, and burn
throughout; Treasury owns valuation and allocation; and the burn Treasury returns is how Market finishes. A settlement
error fails the block, which is the deliberate trade: the hazard it replaces mis-allocated silently.

### 6.1 NOAH to stablecoin

In the transaction, Market quotes the output at the block's Oracle rates with spread, enforces the trader's minimum
receive, applies the virtual-pool transition, moves the gross NOAH offer into its own account, derives the expansion
principal (§3.3) and refuses the conversion if it exceeds the offer, mints and pays the stablecoin output, and adds the
gross offer and the NOAH value of the output to the block's transient conversion totals. No Treasury call is made.

At EndBlock, Market hands Treasury the totals. Treasury values the liability partition once (§7.1), sizes the three
targets on the net basis (§7.2), takes each fund's gap against what it recognises (§7.3), and runs the waterfall over
the block's gross total `G`:

```text
buffer_credit    = min(G, redemption_buffer_gap)
reserve_credit   = min(G - buffer_credit, strategic_reserve_gap)
insurance_credit = min(G - buffer_credit - reserve_credit, insurance_gap)
overflow_burn    = G - buffer_credit - reserve_credit - insurance_credit

G = buffer_credit + reserve_credit + insurance_credit + overflow_burn
expansion_principal <= G
Delta NOAH supply   = -overflow_burn
Delta stable supply = +sum(stable_output)
```

Treasury moves each positive credit from Market's account to the fund and returns `overflow_burn`; Market burns it
from what remains of its escrow (D5, D6). When the valuation is incomplete, the whole gross total parks in the Reserve
and nothing burns: an incomplete aggregate can land either side of the truth, and parking is revisable where a burn is
not. `EventExpansionAllocated` carries the denomination, the three credits, and the overflow; `EventLiabilityIncomplete`
carries the claimable liability the block could value when it parked.

Market never revalues the output, recomputes a target, or sees the credit split; it burns what it is told. The gross
offer never passes through a Treasury account, and Treasury never mints or burns.

### 6.2 Stablecoin to NOAH

In the transaction, Market quotes the NOAH output with spread, applies the pool transition, moves the stablecoin offer
into its account, burns the whole offer, mints the whole quoted output to the redeemer, and adds the output and the
redeemed liability to the totals, each valued at the rate its own path quoted: the fresh Oracle set for a swap, the
plan's committed rate for a settlement conversion.

At EndBlock, after the waterfall, the Buffer's share of the block's summed output moves into Market and is burned
there. The end state is what a per-conversion draw would have produced, reached by mint-then-burn instead of minting
only the residual: the redeemer holds the quoted output, the Buffer is down by what it paid, and net new supply is the
difference. Let `B` be the Buffer after the block's expansion credits, `L` the claimable liability before the block's
burns, reconstructed as the freshly scanned net liability plus the block's redeemed value, and `Q` the summed output:

```text
buffer_paid   = min(Q, floor(Q * B / L))
residual_mint = Q - buffer_paid
```

The product is formed before the division. Forming `B / L` first rounds an intermediate against its own bound of one
and `Q` then amplifies the error past `B`; multiplying first leaves one rounding on a whole-unit quantity, so
`buffer_paid <= B` and `buffer_paid <= Q` are theorems rather than checks. Two properties of the basis are decisions:
it is net, because no claim can arrive from paper the Reserve holds (D67), and it is raw, never scaled by the exposure
multiplier, because rationing the run's opening phase guards against an exhaustion proportional coverage cannot
produce while multiplying coverage front-loads finite inventory on a bet about run depth (D73).

The draw always runs. Unclaimable supply, a member whose feed is stale or suspended supply without an activated plan,
leaves the denominator and is disclosed, so a suspension elsewhere never retires the Buffer during the contagion exits
it exists to dampen (D4). The cost is that frozen supply exerts no drag on coverage, so the Buffer spends
proportionally faster during a freeze, bounded by the frozen fraction.

Within a fixed claimable set, coverage cannot fall. With `c = B / L` and every redemption satisfying `0 < Q <= R <= L`
for its redeemed liability `R`, the Buffer after the draw against the liability after the burn is `c(R - Q) >= 0`
above proportional, and flooring the payment only retains more. At a boundary where the claimable set changes the
ratio steps while the balance stays continuous: a suspension steps coverage up, a plan activation or a returning feed
steps it down, and neither changes any quote or output, so a step creates no first-mover entitlement. An empty Buffer
mints everything; a Buffer at or above the liability pays the whole output; retiring the last stablecoin leaves any
excess in the Buffer rather than gifting it to the last redeemer.

There is no residual-mint limiter. The capacity analysis found no recipient-output, fixed-price cycle, or split-path
amplification under coverage-based funding, and a visible quota would create exactly the first-mover pressure the
draw avoids (P2). `EventRedemptionBufferDrawn` carries the denomination and the payment.

### 6.3 Stablecoin to stablecoin

One fresh offer and ask snapshot; the offer converts directly to the ask denomination. The path loads neither
`BasePool` nor `ArkPoolDelta`, burns the whole offer, mints the integer ask after the larger of the two Tobin taxes and
dust, and touches no fund and no NOAH. Its result is therefore independent of which unit labels the virtual pool, and
`axdr` and any later basket are valid in both directions.

### 6.4 Public ratios and the no-cliff rule

Every balance and supply is public, so anyone can compute the three funds' ratios; the protocol makes their meanings
explicit instead of hiding them (D19). The Buffer ratio measures how much existing NOAH is recycled per unit of
liability, not backing. The Reserve ratio measures what the Reserve still holds against its target; NOAH committed to
the Buffer counts there and nowhere else. The Insurance ratio measures unencumbered balance against covered-loss
capacity, with the raw balance and the reservation reported apart. No ratio crossing zero, target, or any threshold
changes a quote, spread, minimum receive, or eligibility; queries report the three balances separately and publish no
combined health figure. The goal is to remove the first-come exhaustion rule and make each signal correspond to one
honest mandate.

### 6.5 Commitments out of the Reserve

Governance may commit Reserve NOAH to the Buffer with `ark.reserve.v1.MsgFundBuffer`, carrying the amount and a
minimum remaining balance, and to Insurance with `MsgFundInsurance` (D27). Source, destination, and denomination are
fixed in code; the handler reads only the live Reserve balance, never a target, price, or Market state, and refuses if
the transfer would leave less than the stated minimum. That minimum is a per-proposal stale-state guard, not a
protocol floor: governance may commit everything by stating zero, and the check is against the remaining balance
rather than an expected pre-transfer figure so a harmless permissionless deposit cannot fail the proposal. The action
is not capped by the target gap, since targets are routing thresholds rather than custody caps.

The committee has the same two transfers, `MsgCommitteeFundBuffer` and `MsgCommitteeFundInsurance`, bounded by the
receiving fund's shortfall on the gross basis and by the mandate's NOAH floor (§7.5). Governance executes its proposal
messages in EndBlock after Market has settled the block, so a commitment lands between settlements without a lock or
queue.

For an amount `a`: the Reserve falls by `a`, the receiving fund rises by `a`, and total supply, liability, quotes,
spread, pool state, and every other fund are unchanged. Commitment is one-way. There is no Buffer-to-Reserve path,
because a public removal path would make committed inventory reversible and create pressure to redeem ahead of it, and
no transfer-history collection, because the passed proposal is the authorisation record and Bank's event is the audit
(D16). A commitment succeeds under an incomplete valuation; the enlarged Buffer is then spent by the ordinary draw.

### 6.6 Insurance claims

Governance stores one Claims mandate: the shared envelope (chain-derived term, exact committee, half-open activation
and expiry heights) and a fixed gross claim allowance for the term (D30, D38). An empty committee is the disabled
mandate; replacing or disabling advances the term, resets the allowance used, and never touches an existing claim or
the reservation. Appointment requires an active span no shorter than the cancellation period, an `x/claims` parameter.

A claim is submitted, encumbered, then paid by the chain:

- **Submission.** The committee submits a positive `anoah` claim with its expected term during the active window; the
  claim must fit the remaining allowance, consumes it permanently, stores the term, and must close before the mandate
  expires. Governance submits at any height with no term and consumes no allowance. Both derive the closing height
  from the cancellation period, name a recipient Bank can pay, and must be covered by the Insurance balance less what
  is already reserved. The keeper assigns a monotonic claim ID, reserves the amount without moving coins, and emits
  `EventClaimSubmitted`.
- **Cancellation.** Before the closing height, governance may cancel any pending claim without reading the mandate, and
  the current committee may cancel a non-governance claim with its exact term. Cancellation releases the reservation,
  records which authority vetoed, and restores no allowance, so approve-and-cancel cycles cannot redirect expansion
  funding (D32).
- **Settlement.** At the closing height cancellation closes for everyone and the `x/claims` EndBlocker pays the
  immutable recipient and amount. Payment needs no signer because nothing about it is discretionary, and the recipient,
  who has just suffered the covered loss, is the party least able to send a transaction; an unsettled claim would
  encumber Insurance for as long as it stood. The sweep is uncapped, since one settling block answers one submitting
  block at comparable cost. A claim the chain cannot pay ends `failed` and releases its reservation, because nothing
  about a due claim changes with height and a retry would repeat an identical computation; `EventClaimFailed` carries
  no reason, since the cause is chain-generated text and a consensus-visible field cannot carry it.

The reservation is an encumbrance, not a movement: submission lowers the unencumbered balance and opens only the
passive target gap, execution lowers balance and reservation together and creates no second gap shock, and the
reservation never exceeds the balance. There is no guardian, pause, category, per-claim cap, or clawback of a paid
claim (D32).

The record is one height and two enums. `closing_height` is scheduled while pending and actual once not, because
settlement happens in the EndBlock of the block a claim comes due and a cancelled claim never reaches its scheduled
height; the live handler, not stored-record validation, enforces the window. `origin` and `cancelled_by` share the
`ClaimAuthority` enum, so a committee withdrawing its own claim is a meaningful comparison; the cancelling address is
recoverable from the appointment history and the governance authority is a constant. `submitter` stays an address
beside `mandate_term`, because submission moves value and the pair pins the accountable party without a lookup. No
free-text reason is stored: a required reason nobody verifies adds a field without a fact, and where one exists it is
in the governance proposal.

## 7. Capital: liability, targets, recognition

### 7.1 The liability partition

Treasury values the stablecoin liability directly in NOAH (D24), once per block that converts and on every read of
`FundStatus`, never from a cache (D39). The registry decides what counts and how (`x/asset/README.md`,
[Treasury valuation](../../x/treasury/README.md#liability-and-capital-reads)):

| Bucket | Supply | Valued at |
| --- | --- | --- |
| `priced_liability` | ACTIVE and ISSUANCE_HALTED members with a fresh feed | the Oracle rate |
| `settlement_liability` | SUSPENDED members under an open settlement plan | the plan's committed rate |
| `stale_priced_liability` | members whose feed is stale or absent | the last known rate, kept apart because it failed the freshness gate |
| excluded, disclosed | suspended supply with no plan (`untrusted_suspended_supply`); written-off supply (`written_off_exposure`) | nothing: a write-off extinguishes the obligation, and untrusted supply cannot be valued |

Gross liability is the three valued buckets. Self-held liability is the Reserve's balance of each counted member, valued
through the same branch that counted it, so the two sides of the subtraction never price on different bases and
`net = gross - self_held` is non-negative by construction (D66). `FundStatus` reports gross, self-held, and net side by
side.

Completeness is read off the disclosure lists, not a flag: a non-empty `stale_member_supply` or
`untrusted_suspended_supply` means a recognised exposure went unvalued, and every target then reports zero, while a
non-empty `written_off_exposure` does not. Incompleteness means exactly one thing, an exposure no honest rate could
value; arithmetic that leaves the representable domain is fatal instead, because it is state the protocol does not
support and would otherwise retire the Buffer behind a signal that fires for benign reasons.

Completeness is defined by outstanding liabilities, never by a policy denomination. A missing `axdr` rate is irrelevant
while `axdr` supply is zero, a stale reference rate affects only cap derivation, and a stale pool-unit rate may block a
Market quote without touching this valuation.

### 7.2 Targets and the exposure multiplier

Each fund's target is its governed ratio times the liability basis times the multiplier `m`, rounded up, because a
requirement ceils (D72, and the arithmetic rules in `CLAUDE.md`):

```text
target_f = ceil(ratio_f * m * basis)
```

Two bases, assigned by one principle (D67). A flow serves claims that can arrive, and none arrives from paper the
Reserve holds, so the expansion waterfall's gaps size on net. A bound serves discretion, and the discretion-holder can
re-issue that paper, so the bounds on committee acts size on gross: the Reserve's required capital, hence its burnable
surplus, and the Buffer and Insurance shortfalls that bound committee commitments. `FundStatus` reports both families,
`*_target` on gross and `*_net_target` on net, beside the multiplier.

The multiplier says the same nominal liability is a larger exposure under stress. It combines three indicators the
chain already maintains, each weighted by a committee lever in `EconomicPolicy`: the liability ratio, net liability
over circulating NOAH, which is the dilution term and collapses to a quotient because liability is already in NOAH;
the annualised realised volatility of the reference rate, an EWMA of squared per-block returns; and flow pressure, an
EMA of per-block net redemption value taken from the conversion totals. Samples fold in every block's settlement; the
multiplier recomputes on a governed period, moves at most a governed step per recompute, and is clamped to `[1, cap]`.
The decays, cap, step, and period are governance `Params`, because they define the measuring instrument and the rails
on how far and how fast the response may move; the weights are the committee's, because a weight is the same kind of
stance as the ratio it modifies and reversible within hours (D37, D72). Every input is a vote-median rate, a Bank
supply, or flow that cost spread to produce, so no spot price enters. Zero weights pin `m` at one. The multiplier's own
liability-ratio input reads raw liability, since a controller fed its own output would compound, and the redemption
draw reads raw liability by D73.

### 7.3 Recognition

Treasury computes what each fund needs; each fund's module computes what it has, and returns one number through
`RecognisedCapital` (D54, D55):

```text
required_capital   = target_f
recognised_capital = what the fund's owner certifies
capital_gap        = max(required_capital - recognised_capital, 0)
```

The Buffer counts its `anoah` balance at par; Treasury reads it directly, since the Buffer has no operator. Insurance
counts its balance less the reservation held against pending claims, because an approved claim cannot cover a second
loss. The Reserve counts its `anoah` at par plus a haircut credit for each attested position:

```text
credit_a = min(haircut_a * attested_quantity_a * rate_a, cap_ratio_a * recognised_capital)
```

where `rate_a` is the derived feed's rate within the entry's own staleness window and zero otherwise. The cap is a
share of recognised capital itself, the self-reference solved per block in closed form: a flat amount decays into a
binding or a vacuous constraint as the balance sheet moves, and a share of the requirement would let one corrupted
attestation against an empty fund manufacture credit up to a share of a large target, whereas against the certificate
itself credit levers only the provable NOAH base and a lone bad input can never push the total past `honest ÷ (1 −
ratio)` (D58, D64). Ratios sum strictly below one per policy, checked at the write and at genesis.

Everything about recognition degrades to zero, never to a stale number. A dark or over-age feed zeroes exactly one
asset's credit and tightens every neighbour's ceiling; there is no governance-supplied fallback price; valuation is
never stored (D57, D71). Ark-issued paper earns nothing wherever it sits: it is a liability until burned, and the
external-symbol shape of eligibility entries makes a member entry unrepresentable rather than merely refused (D28,
D62, D69). An external holding is named `<feed>-<tag>` and priced on the feed its prefix derives, so several custodians
share one series with their own haircut, cap, and window and no aliasing exists (D70). Custody is attested: the asset
sits at a mandate destination and the committee books its quantity in an append-only journal that keeps errors beside
their corrections; the account itself never holds an external token (D57, D59, D70). The `RecognisedCapital` query
decomposes every row so each zero credit shows its reason, and the Reserve registers as an Oracle feed-removal guard so
a feed that backs a credited entry or an open position cannot be removed before governance delists it (D63).

When the Reserve spends 100 NOAH on an asset recognised at 70 after haircut, a 30 NOAH gap reopens. Zero credit would
reopen all 100 and invite an acquisition-and-refill loop that captures expansion principal; full credit would close a
gap that is not closed. The rule is part of the anti-feedback policy, not only reporting.

### 7.4 Passive-target rules

- Targets decide only where new expansion principal goes (D7). They are not custody caps: deposits are valid above
  target, and a falling target releases, burns, or moves nothing.
- The Buffer spends below target through the draw, which the target never triggers. The Reserve's only debits are the
  governed commitments, the committee's mandate, and burns; no target, price, or Market request sizes them. Insurance
  is encumbered by submission and spent by settlement; the target authorises neither.
- Expansion fills the Buffer, then the Reserve, then Insurance, and burns only what remains. A Reserve-to-Buffer
  commitment changes two balances and reopens the Reserve's gap for the same waterfall to fill later.
- An incomplete valuation parks the block's whole gross expansion in the Reserve and fails no priceable conversion;
  failure to value an output itself fails the conversion before any transfer.
- `FundStatus` always answers and never presents a partial target as complete: incomplete valuation zeroes every target
  and lists what it excluded. Refusing to answer during exactly the stress that makes valuation incomplete would blind
  operators when they most need the report.

### 7.5 Bounds on committee acts

The same targets bound what a committee may move or destroy, always in the conservative direction. The Reserve
committee may burn NOAH surplus up to `min(recognised_capital - required_capital, balance - mandate_floor)`, so the
power exhausts itself at the target line and leaves the committee timing rather than size; that bound requires a
complete valuation, since an incomplete aggregate can understate the requirement and overstate the surplus (D61). The
committee may commit to the Buffer or Insurance up to the receiving fund's shortfall on the gross basis and above the
mandate's floor; those bounds size on whatever the block could value, because their error runs only in the direction
where the money stays protocol capital. The exposure multiplier tightens all of them: a higher requirement shrinks the
burnable surplus and widens the shortfalls, and `m` is protocol-computed state no committee can move (D72).

What each ratio literally sets, and what to choose it from, is tabulated in `docs/governance/GENESIS.md` §12. Under NOAH-only
custody every fund's exposure has liability as its only base, which is why the ratios are the exposure model and no
per-fund model exists; its revisit trigger is in [future changes](../direction/FUTURE_CHANGES.md#4-capital-and-protocol-follow-ups).

## 8. Transfer tax

### 8.1 Settings

- `Params.transfer_tax_rate`, governance-owned, in `[0, 1]`. Zero disables the tax without deleting anything (D44). It
  is governance's rather than the committee's because it is part of the fee every wallet signs: a raise refuses every
  in-flight transfer until clients re-query, and the voting period is the notice that needs (D80).
- `Params.reference_denom` and `Params.reference_tax_cap`: the reference unit, which is the Oracle's protocol reference
  and launches as `axdr`, and a cap amount in it. Zero means uncapped; a positive amount derives a positive integer cap
  for every taxable denomination through the conversion-factor table (§8.4). Changing the amount moves no fund, pool,
  quote, or coverage figure (D21, D37).
- No tax-rate controller, revenue target, change-rate limit, or rolling indicator exists (D79).

The rate is held at or below the conversion spread floor a conversion committee can reach, because NOAH is untaxed and
`MsgSwap` is exempt, so a swap to NOAH, a NOAH send, and a swap back delivers the same value for spread instead of tax
whenever the rate exceeds the floor, at every principal. Nothing enforces this in code, since Market depends on
Treasury and neither module's stateless validation sees the other's state; it is a governance-time constraint on the
rate and on every conversion appointment (D81).

### 8.2 Taxable denominations and operations

Every registered stablecoin with a derived cap is taxable; NOAH never is (D44). A registered denomination whose cap
is missing fails closed, an explicit zero cap is uncapped, and a denomination outside the registry is untaxed. What is
taxed is the transfer, so a suspended or written-off denomination stays taxable while it moves.

One calculator prices every user-facing transfer surface, and internal Bank movements are never taxed (D18): a global
Bank hook would tax settlement, draws, claims, reward funding, and commitments unless it recreated an exemption system.

| Input | Tax principal |
| --- | --- |
| `bank.MsgSend` | the stablecoins sent |
| `bank.MsgMultiSend` | each input independently; outputs never |
| `market.MsgSwapSend` | the stablecoin offer |
| `market.MsgSwap` | exempt; a self-returning conversion is governed by spread |
| `authz.MsgExec` | every nested message, recursively, to a bounded depth |
| vesting account creation | the stablecoin funding |
| IBC `MsgTransfer` and v2 `MsgSendPacket` | the outbound stablecoin; a transfer-port payload carries principal, other ports carry none (D46) |
| Wasm instantiate and execute | attached stablecoin funds |
| contract, ICA, and GMP dispatches | each Bank send, IBC send, execute funds, and instantiate funds at dispatch (§8.6) |

Escrow, packet acknowledgement, timeout, refund, forwarding hops, Market settlement, fund movements, claims, and reward
distribution are protocol continuation, not new inputs, and receive no tax (D42, D46, D47).

### 8.3 Calculation

```text
uncapped[input][denom] = floor(principal[input][denom] * transfer_tax_rate)
tax[input][denom]      = uncapped                          if cap[denom] == 0
                       = min(uncapped, cap[denom])         otherwise
total[denom]           = checked sum over every taxable input
```

Each input receives its own cap; two sends from one source, two `MultiSend` inputs, and a signed input beside an
execution-generated one are never combined (D17). Malformed or undecodable nested messages fail closed. Totals use
checked addition and return an out-of-range error rather than panic. The ante's checker, the post decorator's charge,
the policy router, and `Query/ComputeTax` call one implementation.

### 8.4 Caps and the reference denomination

Each block Treasury derives a conversion factor for every member from the Oracle rate set, expressing one reference
unit in that denomination, and both derived figures follow from it: the per-denomination tax cap is the reference cap
times the factor, and the per-denomination gas price is the base gas price times the factor. A factor persists through
a feed outage, so a stale feed defers a cap rather than failing a block; a member arriving without a rate seeds at
one. A positive reference cap that would truncate to zero is refused, so it can never become the uncapped sentinel
(D44). `EventConversionFactorsRefreshed` carries each pass's table.

`Params.reference_denom` equals the Oracle's reference at every height, and every guard sits at a write, never at a
read: `InitGenesis` refuses a mismatch, `MsgUpdateParams` rejects any denomination change so governance moves the
amount alone, and the Oracle's `MsgSetReferenceDenom` is the one denomination-moving path, running Treasury's cap
rebase and Market's pool rebase before storing the new reference and failing whole if either fails
(`EventReferenceTaxCapRebased`). Nothing re-checks the pair at runtime, because a derive-time guard fired only when a
rebuild was owed and made the derivation depend on state it never used. The obligation is on any future writer of
`Params`: an upgrade that touches either side repeats the genesis check and tests the pair rather than each side alone.

### 8.5 The fee: declaration, settlement, charge

The signed fee declares the tax (D78). A transaction's fee is gas plus the exact tax its messages owe, and Ark's own
`FeeDecorator` in `app/ante/fee.go` holds the declaration to the computed tax before anything is deducted, so a signer
sees the whole charge in the fee they sign and is never taxed past it. The fee field carries it because it is the one
slot every wallet builds and every sign mode renders, amino included. A wallet that omits the tax is refused with an
error naming it, the deliberate cost of the declaration; `arkd` prices every transaction command through
`Query/ComputeTax` and `Query/GasPrices`, and a front end does the same.

The fee is a ceiling and the tip is NOAH (D80). Each stablecoin leg is charged at most what it owes, the exact tax plus
the base fee if it is the first leg in denomination order whose slack covers it, and the rest never leaves the payer,
so a wallet pads a stable leg for rate drift at no cost. The NOAH leg is charged whole: base fee if no stable leg paid
it, remainder tip, and only the tip ranks for priority. NOAH is the one denomination the tax is never owed in, which is
what lets the chain tell a tip from a padded tax.

The tax is charged after the messages, on success only (D82). The ante deducts the gas fee and tip and hands the
priced tax on through the context; `TransferTaxDecorator` in `app/ante/transfer_tax.go` runs on the messages' branch,
charges that figure to `transfer_tax_collector`, and draws a granter's allowance for it. A transaction whose messages
fail pays gas and no tax; a payer the messages leave short of the tax fails at the charge with gas kept and principal
unmoved; a payer who acquires the taxed denomination from an earlier message in the same transaction is taxed and
succeeds. Admission, recheck, and proposal verification check the signed tax declaration and upfront gas fee
but skip tax collection and tax allowance draws. Finalisation and simulation check affordability after messages;
a tax failure reverts those messages and keeps the gas fee. Nothing is escrowed or returned. Feegrant draws the gas fee at the ante and the tax at the charge, so
an allowance is never charged for a tax a failed transaction did not pay.

Gas is priced by Treasury's base-fee controller, not by node-local minimum gas prices ([Treasury fee controller](../../x/treasury/README.md#conversion-factors-and-dynamic-fees)).
The base gas price, held in the reference unit and floored at `min_base_gas_price`, moves each block toward a target
block utilisation by at most `base_fee_adjustment_rate`, and each accepted fee denomination prices through its
conversion factor. The ante chain itself is assembled in `app/ante/ante.go` rather than by the SDK's constructor,
because the Wasm decorators must sit immediately after context setup and the SDK admits no insertion point; an SDK
release that adds a decorator is mirrored by hand, and a test pins the order. Under simulation the gate's reads are
made and not enforced, so an estimate tracks execution to within about a percent.

### 8.6 Execution-generated tax

A transfer a contract, an interchain account, or a GMP-derived account creates while it runs is absent from the signed
message list, so the ante cannot see it. Ownership by call path gives exactly-once assessment without a transfer ID or
context marker (D41): the ante owns signed top-level inputs, and the execution policy router in
`app/execution_policy_router.go` owns everything dispatched. At dispatch the router prices the message with the same
calculator, charges the dispatching account in addition to the complete principal, sends the tax straight to
`transfer_tax_collector`, and runs the dispatch, all inside the same cached execution, so a synchronous or caught
failure rolls both back and a recipient's amount is never reduced to fund tax (D42, D48). The outer fee payer and any
feegrant granter never sponsor it. The router also enforces the message policies the ante enforces for signed
transactions, the MultiSend fan-out guard and the vote stake floor, so the two seams agree.

A contract estimates tax through `Query/ComputeTax`, reached through the query accept list (§11.3), by pricing the
proto form of the message it will dispatch; the estimate reserves nothing and never replaces recomputation at dispatch
(D43, D74).

## 9. Validator and Oracle funding

### 9.1 Two lanes and one window

Gas fees reach `fee_collector` and pass to Distribution every block. Transfer tax accumulates in
`transfer_tax_collector` until Treasury settles the funding window. Validator and Oracle funding have separate
per-block NOAH-value targets in `EconomicPolicy`, but Treasury adds the applicable target once per observed block and
compares only the window's aggregates, so a quiet block does not spend subsidy that a busy block in the same window
would have covered (D10). Expansion principal funds the three capital funds, never rewards.

`reward_funding_window` is a governance parameter defaulting to one chain week and bounded at `2^32` observations so
the accrual cannot overflow (D34). It is a separate clock from the Oracle's reward window. When an empty window records
its first observation it initialises the countdown from the parameter; a parameter change never rewrites a positive
countdown and takes effect at the next empty window. Beginning with the first block, Treasury EndBlock observes the block just executed.

### 9.2 Accrual

Treasury persists one `RewardFundingState`: the countdown, the accumulated validator and Oracle targets, the accumulated
NOAH value of eligible fees. For each completed block it adds the then-current targets with
checked addition and values the non-empty `fee_collector` balance before Distribution empties it, at that block's
accepted Oracle rates, without retaining or moving the coins. NOAH has identity value; registered stablecoins receive
target credit; a denomination the registry cannot price counts as zero for that observation while the priced
remainder still accrues. An accumulated figure that leaves the representable domain fails the EndBlock, because a
window valued from a partial sum would misallocate real coins rather than degrade safely, which is why this lane has
no conservative branch.

### 9.3 Settlement

With the window's accumulated validator target `V`, Oracle target `O`, eligible fee value `G`, the NOAH value `T` of
the collector's priced tax, and the subsidy pool balance `B`:

```text
validator_pre_tax_gap = max(0, V - G)
protected_oracle_tax  = min(T, O)
desired_validator_tax = min(T - protected_oracle_tax, validator_pre_tax_gap)
```

One validator ratio, `desired_validator_tax / T`, is applied to every tax denomination independently, rounding the
validator amount down; the Oracle receives the exact remainder of every coin, and no coin is converted (D11). The
actual post-rounding allocations are revalued from the same snapshot:

```text
validator_shortfall = max(0, V - (G + value(validator_tax_coins)))
oracle_shortfall    = max(0, O - value(oracle_tax_coins))
total_shortfall     = validator_shortfall + oracle_shortfall

if B >= total_shortfall: pay both in full
else:
  oracle_paid    = floor(B * oracle_shortfall / total_shortfall)
  validator_paid = B - oracle_paid
```

Scarce subsidy is shared by shortfall with the integer remainder to validators (D12). The transfers run in Treasury's
EndBlock: validator tax and subsidy into `fee_collector` for Distribution at the next block's start, Oracle tax and subsidy
straight to the Oracle account, so an Oracle top-up never passes through Distribution. Gas at or above `V` sends all
tax to the Oracle; tax at or below `O` does the same; only tax above the protected Oracle floor may replace subsidy on
a validator gap, and every residual returns to the Oracle. Depletion of the subsidy pool never mints, borrows from a
fund, or stops block production; a deposit extends coverage without touching `V` or `O`.

Before valuing the pot, Treasury partitions the collector's unpriced coins: tax in a written-off or retired
denomination routes to the Reserve as inert custody (`EventUnpricedTransferTaxRouted`), and tax in a denomination
still awaiting a feed stays in the collector for a later window, so no unvalued coin reaches an allocation. A
successful settlement resets the state to canonical zero atomically with its transfers; `EventBlockRewardsToppedUp`
carries the targets, organic funding, and payments.

### 9.4 Distribution and the Oracle

Distribution's `community_tax` is zero in every generated genesis because the distribution module basic is overridden
in application code, so nothing skims validator income (D13). The community-pool ledger stays inside Distribution and
receives only rounding residue; Treasury never funds it, `x/protocolpool` is not wired, and governance may sweep it
(D14). The Oracle distributes every positive denomination its account holds across validator reward weights,
truncating per validator per denomination and leaving dust in place; its reward and distribution windows remain the
smoothing horizon for scores.

### 9.5 Lifecycle order

Conversion settlement precedes governance lifecycle changes and later fund movements. Reward funding values each
block's earned fees before Distribution consumes them at the next block's start; Oracle funding bypasses Distribution.
The [application lifecycle](../../app/README.md#block-lifecycle) owns hook ordering, and
[Treasury](../../x/treasury/README.md#reward-funding-and-genesis) owns funding-window mechanics.

## 10. Authority and reporting

### 10.1 Treasury params and policy

`Params` holds what governance owns: the reference denomination and cap amount; the funding window; the transfer tax
rate; the exposure instrument, `volatility_decay`, `flow_decay`, `multiplier_cap`, `multiplier_max_step`, and
`exposure_refresh_period_blocks`; and the base-fee controller, `base_fee_target_utilisation`,
`base_fee_adjustment_rate`, and `min_base_gas_price`. Each is domain-capped in its own validation with orders of
magnitude of headroom, so a governance value feeding halt-class arithmetic is refused at the write rather than
discovered in a block: the window at `2^32`, the refresh period at a year of blocks, the multiplier cap at a million,
the base gas price at `10^18`, the adjustment rate at one.

`EconomicPolicy` holds the committee's levers: the two block reward targets, the three fund target ratios, and the
three exposure weights. The boundary between the two messages is stance against machinery: a lever stating how much of
something the protocol wants, reversible and safe to clamp between a mandate's bounds, is the committee's; the
instrument that computes it, its cadence, and its rails are governance's (D37). Ratios and weights lie in `[0, 1]`;
the three target ratios are independent stocks whose sum may exceed one, since the waterfall rather than their sum
rations scarce principal.

`EconomicMandate` is the shared envelope, term, committee, and half-open window, plus a minimum and maximum policy. Its
empty-committee form is the disabled state.

### 10.2 Role separation

One principle sorts every message: roles with disjoint powers or different execution semantics get separate messages,
and one message serves several roles only when they are the same action under the same rules. No committee passes
that test against governance, since each differs in staleness guard, allowance metering, or cancellable set, so every
role has its own message and every committee power is enumerable from the proto service. Each handler is one
authorisation assertion followed by a shared keeper core, so effect logic cannot drift between roles. Governance
messages carry no expected term, because no governance authorisation depends on a mandate; a committee message naming
the wrong signer is rejected before any term reasoning.

| Owner | Governance authority | Delegated committee authority |
| --- | --- | --- |
| Treasury | Parameters, appointment, and unrestricted valid economic policy | Policy inside the appointed corridor |
| Claims | Parameters, appointment, submission, and cancellation | Term-limited submission and origin-limited cancellation |
| Reserve | Recognition, appointment, corrections, commitments, and discretionary burns | Bounded deployment, bookkeeping, commitments, and burns |
| Market | Parameters, appointment, conversion policy, and Tobin policy | Conversion corridor and Tobin band |
| Oracle | Parameters, feeds, and reference unit | None |
| Asset | Registry, lifecycle, settlement terms, and appointment | One suspension per asset per term |
| Security | Appointment and standard-module authority | Own upgrade planning/cancellation and client recovery |

Market's `MsgSwap` and `MsgSwapSend` are the trader's, and `MsgSettle` converts suspended supply under a plan. Three
absences are decisions: no fund has a deposit message, since Bank rails and the recipient restriction suffice; no
message settles a claim, since the chain does it; and no message withdraws from the Reserve to an arbitrary recipient,
since every Reserve debit is a fixed commitment, a bounded deployment, or a burn.

Appointment, staleness, and module-specific bounds are documented by the [module READMEs](../../app/README.md#module-entry-points)
and the [shared mandate package](../../pkg/mandate/doc.go). The claim and Reserve flows are specified in §6.6 and §7.3;
their financial bounds remain in §7.5.

### 10.3 Reporting

`FundStatus` reports gross, self-held, and net liability, disclosures, both target families, and the multiplier; it
never combines distinct funds into a single backing figure (D19). Insurance capital is balance less reservation.
Funding accounting remains readable without current Oracle prices. Module query schemas and their READMEs define the
exact API; [protocol monitoring](../operations/PROTOCOL_MONITORING.md) defines the operational interpretation of those surfaces.

Deposits, commitments, and burns emit no module event: Bank's canonical transfer and burn events already record
sender, recipient, and coins, and the signed message is the authorisation. Every Treasury liability figure in a query
or event is an `anoah` value constructed from the whole decimal aggregate, never truncated for display.

## 11. Transfer surfaces: IBC and Wasm

### 11.1 The execution-tax contract

Every transfer adapter on the chain implements one contract (D40 to D42):

- The fee payer or feegrant granter pays every ante-visible tax as part of the declared fee.
- The dispatching account, a contract, an interchain account, or a GMP-derived account, pays every execution-generated
  tax in addition to the complete requested principal; an insufficient balance fails that dispatch without reducing
  the recipient's amount.
- The ante owns signed top-level inputs, including IBC `MsgTransfer` and Wasm attached funds; the policy router owns
  only what execution generates. This structural ownership is exactly-once assessment, with no persistent transfer ID,
  context marker, or global Bank hook.
- Execution-generated tax goes straight to `transfer_tax_collector` inside the same cached execution as its transfer,
  so synchronous, caught, and uncaught failures roll both back.
- Neither feegrant nor the outer fee payer sponsors execution-generated tax.
- A successfully created IBC packet keeps its tax through acknowledgement, timeout, refund, and return; those, and
  escrow, forwarding hops, Market settlement, fund movements, claims, and reward distribution, are never new inputs.

### 11.2 The hub

Ark supports both ICS-20 routes, packet forwarding, callbacks, ICA, GMP, and a dormant Wasm light client.
The [application README](../../app/README.md#ibc-and-wasm-integration) owns the exact stack, routing, and excluded integrations.

The hub ships shut behind one switch. The launch allowed-client list is empty, so no client, connection, channel, v2
counterparty, or packet of any kind can exist until governance admits `07-tendermint`; the ICS-20 flags and both ICA
sides are off as well, so that vote opens client creation and nothing more (D45 as amended). One switch was chosen
because two surfaces have no flag of their own: GMP, whose derived accounts are ordinary accounts that pay execution
tax on a contract's terms, and a contract's own IBC channels on either stack. Opening the hub is a governance sequence
recorded in `docs/governance/GOVERNANCE_OPERATIONS.md` §2: admit the client type, let relayers open routes, set rate limits per denomination
and route, then flip the transfer flags.

A packet-forwarded hop is continuation of the original transfer and takes no second tax; the raw v2 `MsgSendPacket` is
the same outbound leg as `MsgTransfer` and prices identically, since the transfer application authenticates the
payload sender and escrows through the same path, and a transfer payload the calculator cannot decode fails the
transaction rather than passing untaxed (D46). Every IBC credit to a fund passes through the configured Bank keeper, so
the recipient restrictions cannot be bypassed.

### 11.3 The contract runtime

The runtime ships open: anyone may upload and instantiate from height one, while the empty IBC client allowlist
prevents contract channels. Contracts use the execution-tax contract above; generated messages must name the calling
contract as signer. The query accept list exposes a deliberately bounded deterministic API whose admitted response
shapes are consensus inputs. Widening it requires a coordinated binary upgrade, not a parameter vote.

[Application integration](../../app/README.md#ibc-and-wasm-integration) owns runtime wiring, query registration, callbacks,
and address handling; [governance operations](../governance/GOVERNANCE_OPERATIONS.md#4-the-contract-runtime) owns permissions.

Under `Nobody`, governance could still upload, since Wasmd hands the authority its own permission policy; the choice
to open the runtime removes a lock on everyone else, not on governance.

## 12. Related references

- [Economic decisions](ECONOMIC_DECISIONS.md): preserved rationale, rejected choices, and amendments.
- [Genesis](../governance/GENESIS.md): launch settings, open values, and validation.
- [Governance operations](../governance/GOVERNANCE_OPERATIONS.md): appointments, calibration, and external-asset onboarding.
- [Future changes](../direction/FUTURE_CHANGES.md): deferred engineering and its revisit triggers.
- [Application module map](../../app/README.md#module-entry-points): state, API schemas, and subsystem development.
- [Ante/post handling](../../app/ante/README.md) and [IBC/Wasm integration](../../app/README.md#ibc-and-wasm-integration): execution enforcement.
