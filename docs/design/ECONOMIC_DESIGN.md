# Economic design

This reference states Ark's economic contract between modules: ownership, custody, conversion flows, liability,
capital targets, fees, and reward funding. It is intended for protocol contributors and readers evaluating how funds
and authority move. [Economic decisions](ECONOMIC_DECISIONS.md) holds each rule's reason under its D/P identifier; the
[subsystem READMEs](../../app/README.md#module-entry-points) own each module's internals; [genesis](../governance/GENESIS.md)
owns launch settings; [governance operations](../governance/GOVERNANCE_OPERATIONS.md) owns the procedures for changing
them.

## Contents

- [1. The design in one page](#1-the-design-in-one-page)
- [2. Ownership and custody](#2-ownership-and-custody)
- [3. Conversion](#3-conversion)
- [4. Capital](#4-capital)
- [5. Transfer tax and fees](#5-transfer-tax-and-fees)
- [6. Validator and Oracle funding](#6-validator-and-oracle-funding)
- [7. Authority](#7-authority)
- [8. Related references](#8-related-references)

## 1. The design in one page

Ark has no routine NOAH issuance. There is no `x/mint`, no staking-reward inflation, no seigniorage, and no fund that
mints to reach a target (D1). Market is the only module holding `Minter`, and it mints and burns only inside
stablecoin conversion settlement (D2). Every other flow moves NOAH that already exists.

Three funds stand behind the stablecoins, each a module account holding NOAH and nothing else at launch:

- **The Redemption Buffer** is conversion inventory. Each redemption draws the Buffer's coverage share of its quoted
  NOAH from inventory and mints the rest (D4), so the Buffer recycles NOAH retained from earlier expansions instead of
  letting every redemption dilute.
- **The Strategic Reserve** is governed emergency capacity. It never pays a redeemer; governance may commit NOAH from
  it to the Buffer, and a committee may deploy it under a bounded mandate (D16, D27, D56).
- **Insurance** pays covered losses through recorded claims under a governance-owned mandate (D15, D30).

The funds are filled by expansion. When NOAH converts to a stablecoin, Market escrows the gross offer and Treasury
routes it once per block down a waterfall: Buffer gap, then Reserve gap, then Insurance gap, then the overflow is
burned (D5, D33). Each fund's target is a governed ratio of the stablecoin liability, scaled by a bounded stress
multiplier (D72), and targets only decide where new principal goes: they never trigger a mint, a trade, or a
withdrawal (D7).

Security and the price feed are funded from a separate circuit. Gas fees flow through Distribution as usual. A fixed
transfer tax on every user-facing stablecoin transfer (D18) accumulates for one funding window, then meets the Oracle
reward target first and any validator gap second, with a genesis-seeded, non-minting subsidy pool covering what
remains (D9 to D12). No controller adapts the tax rate, the targets, or the Buffer share to prices or revenue.

Authority is split by role. Governance holds every unbounded power. Each module's committee holds a bounded,
time-scoped mandate on its own address with its own messages, and none holds general Bank authority (D31, D36); §7.2
tabulates them.

Market's virtual pool is denomination-bearing and launches in `axdr`; one governance message can re-denominate it live
to Orbis, the planned basket currency ([Future changes §3](../direction/FUTURE_CHANGES.md#3-basket-index-assets)),
without changing which stablecoins may be converted (D22, D23).

### 1.1 What the feedback loop is, and is not

The design keeps a bounded endogenous response to conversion flow and removes every amplifier Terra carried:

- Expansion can contract NOAH supply, because principal above the three targets burns. Redemption can expand it, by
  the share the Buffer's coverage does not fund. The coverage draw recycles retained NOAH without a first-come
  depletion cliff.
- The response is block-granular (D33). Every expansion in a block allocates under one liability valuation and every
  redemption draws at one coverage ratio, so there is no within-block ordering to compete for. No quote, payout, or
  spread changes with it.
- The Reserve is never an automatic Market source. Governance commits NOAH to the Buffer by proposal; Market cannot
  request, size, or trigger it.
- Virtual-pool pricing penalises sustained one-way flow and remembers imbalance; time-based recovery limits how fast
  cheap capacity returns.
- No adaptive reward target, tax-rate controller, price-reactive Buffer share, or automatic Reserve trigger exists. The
  reward waterfall allocates only tax already collected and subsidy already issued.
- The exposure multiplier (D72) is the one controller, and a bounded one. It moves only the liability basis the three
  targets are sized on: how much expansion principal is retained rather than burned, and how much capital a committee
  may move or destroy. It mints nothing, triggers no trade, and changes no quote, spread, payout, or draw (D73). It is
  floored at one, capped by governance, and rate-limited per update.

The Buffer and the virtual pool therefore dampen and rate-limit Terra-style supply feedback; they do not remove it.
Buffer-funded redemption releases dormant NOAH into circulation, and residual minting raises supply. The Buffer reduces
dilution; it is not collateral, backing, or solvency capital, and the protocol never presents it as one (D19).
Removing the feedback outright would mean refusing any uncovered output, a redemption gate with a strong first-mover
incentive; Ark keeps unconditional settlement instead.

## 2. Ownership and custody

### 2.1 Ownership boundaries

| Component | Owns | Never owns |
| --- | --- | --- |
| Market | Quotes, spread, final integer output, the denomination-bearing virtual pool, conversion escrow, mint and burn, atomic settlement, the conversion mandate | Tax policy, fund targets, allocation decisions |
| Treasury | Tax policy and the one calculator, liability valuation, the three fund targets and the exposure multiplier, the expansion waterfall and coverage draw, reward funding and the subsidy pool, the base-fee controller, the economic-policy mandate; custody of the subsidy pool, the Buffer, and the tax collector | Quotes, pool state, gross conversion custody, mint or burn, claim adjudication, any fund's `recognised_capital` |
| Claims | The Claims mandate, the claim record, the Insurance reservation, `claims_insurance` custody, Insurance `recognised_capital` | Tax, targets, the waterfall, liability valuation, the Reserve |
| Disbursement | The member and contributor accounts, fully funded member and contributor schedules, the ownership cap, compensation, the conversions that fund stablecoin awards, executed as a Market trader, permanent beneficiary records and operation journal | Minting, quoting or settling conversions, delegation of unpaid principal, off-chain membership or work verification |
| Reserve | `strategic_reserve` custody, the Reserve mandate, the quantity journal and positions, the recognition policy, Reserve `recognised_capital`, the governed commitments out of the Reserve | Tax, targets, the waterfall, liability valuation, claims |
| Oracle | Consensus rates, the feed registry, the reference denomination, attendance and participation scores, Oracle reward allocation | Fiscal allocation, minting |
| Asset | The registry and lifecycle of every non-NOAH Bank asset, settlement plans, the emergency-suspension mandate | Pricing, custody, allocation |
| Distribution | Validator and delegator fee accounting and payouts | Tax classification, any fund |
| App ante and policy router | Fee declaration and settlement, priority, the transfer-tax charge for signed transactions and for contract dispatches | Target-based allocation, a second tax formula, persistent fiscal state |
| Governance | Every unbounded power: Params of every module, every mandate, claims, Reserve commitments and burns, the recognition policy, feeds, the reference denomination | Routine operation, automatic controllers |
| Committees | One bounded, expiring power each, on a distinct address | Anything another role holds; general Bank authority |

Two rules keep the boundaries one-way. Market depends on Treasury and Treasury receives no Market keeper: Market's
EndBlocker hands Treasury the block's conversion totals and receives a burn amount back (D33), so Treasury cannot
initiate or intermediate a pool change. Treasury sizes every fund's requirement and each fund's module reports what it
recognises through one number-or-refuse interface, `RecognisedCapital`; the Reserve reads Treasury's requirement back
only to bound its own burns and commitments (D54, D55). No module holds debit authority over another's account.

### 2.2 Custody

[Genesis §3](../governance/GENESIS.md#3-accounts-supply-and-validator-seats) tabulates every module account's
permissions and inbound rule. What may debit the four custody accounts:

| Account | Owner | Debited only by |
| --- | --- | --- |
| `treasury_redemption_buffer` | Treasury | the coverage draw (§3.2) |
| `strategic_reserve` | Reserve | commitments to the Buffer and Insurance (§3.5), the committee's bounded deployments, and burns (D27, D56, D61) |
| `claims_insurance` | Claims | claim settlement, and the NOAH a stablecoin claim converts when it is submitted (§4.6) |
| `treasury_subsidy_pool` | Treasury | window settlement and `MsgReturnSubsidy` (§6.3) |

- Only Market holds `Minter`, and it also holds `Burner` for settlement. `strategic_reserve` holds `Burner`, never
  `Minter`: governance and the committee may destroy custody there, but nothing may create it (D2, D61).
- The blocked-address list is replaced, not augmented, so every blocked account is named and the four custody accounts
  stay reachable by ordinary sends.
- Each custody owner installs a recipient-aware Bank send restriction over its own accounts. The Buffer and the subsidy
  pool admit only a positive `anoah` credit, and anything else fails atomically whatever surface it came through (D25,
  D26). The Reserve and Insurance admit NOAH and registry members by membership, with no sender exemption (D65, D70,
  D140): that is what admits derecognised transfer tax into the Reserve, where it is inert custody awaiting a governed
  burn, and the paper a stablecoin claim buys into Insurance. External symbols are refused at the Reserve's
  restriction; an external holding is attested at its destination and never sits in the account.
- Deposits ride ordinary Bank rails. They need no module message, receipt, or ledger, are accepted above target, and
  grant no refund, ownership, withdrawal, coverage, priority, governance, or deployment right. A taxable stablecoin
  deposit is taxed like any other transfer.
- Bank balances are the source of truth for every fund. No module stores a fund balance, a target, or a deposit
  record; Claims stores encumbrance and authorisation, and the Reserve stores quantities, never values. Deposits,
  commitments, and burns emit no module event, since Bank's transfer and burn events already record them and the
  signed message is the authorisation.

## 3. Conversion

Every conversion settles in two moments. The transaction does what depends only on its own quote: escrow, mint, burn,
payout. Market's EndBlocker does what depends on liability or fund state: one liability valuation, one waterfall over
the block's expansion, one coverage draw over the block's redemptions (D33). Market owns custody, mint, and burn
throughout; Treasury owns valuation and allocation, and the burn it returns is how Market finishes. A settlement error
fails the block, which is the deliberate trade: per-transaction allocation against a mid-block snapshot would
mis-allocate silently.

### 3.1 NOAH to stablecoin

In the transaction, Market quotes the output at the block's Oracle rates with spread, enforces the trader's minimum
receive, applies the virtual-pool transition, escrows the gross NOAH offer, mints and pays the stablecoin output, and
adds the gross offer and the expansion principal to the block's totals. The expansion principal is the NOAH value of
the final integer output at the quote's own rates, rounded down; Market refuses a conversion whose principal exceeds
the gross offer, so the stablecoin minted is never worth more than the NOAH offered (D33).

At EndBlock, Treasury values the liability once (§4.1), sizes the targets on the net basis (§4.2), takes each fund's
gap against what it recognises (§4.3), and runs the waterfall over the block's gross total `G`:

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

The waterfall runs on the gross offer, so spread and dust travel with the principal: with the funds at target they
burn exactly as an unconditional burn would, and with a fund short they are capital arriving as the requirement rises
(D6). Treasury moves each positive credit from Market's account to its fund and returns `overflow_burn`, which Market
burns from its escrow (D5). When the valuation is incomplete, the whole gross total parks in the Reserve and nothing
burns: an incomplete aggregate can land either side of the truth, and parking is revisable where a burn is not.

### 3.2 Stablecoin to NOAH

In the transaction, Market quotes the NOAH output with spread, applies the pool transition, burns the whole stablecoin
offer, mints the whole quoted output to the redeemer, and adds the output and the redeemed liability to the totals.
Redemption spread is NOAH never issued. `MsgSettle` redeems a suspended asset the same way at its plan's committed
rate, with no spread and no pool transition.

At EndBlock, after the waterfall, the Buffer's share of the block's summed output moves into Market and is burned. The
redeemer holds the quoted output, the Buffer is down by what it paid, and net new supply is the difference. With `B`
the Buffer after the block's expansion credits, `L` the claimable liability before the block's burns, and `Q` the
summed output:

```text
buffer_paid   = min(Q, floor(Q * B / L))
residual_mint = Q - buffer_paid
```

`L` is net, because no claim can arrive from paper the Reserve holds (D67), and raw, never scaled by the exposure
multiplier (D73). It counts every obligation the chain can value (§4.1): a stale member at its last known rate and a
suspended asset at its plan rate stay in, so their share of the Buffer is held for them rather than paid to whoever
exits during the outage. Only supply the chain cannot value, a member never priced or suspended supply without a plan,
leaves the denominator and is disclosed, so a suspension elsewhere never switches the Buffer off for the exits it
exists to dampen (D4).

Within a fixed claimable set coverage cannot fall ([whitepaper A.4](../papers/WHITEPAPER.md#a4-conditional-coverage-property));
where the set changes, the ratio steps while the balance stays continuous, and no step changes a quote or output. An
empty Buffer mints everything; a Buffer at or above the liability pays the whole output; retiring the last stablecoin
leaves any excess in the Buffer. There is no residual-mint limiter: a visible quota would create the first-mover
pressure the draw avoids (P2).

### 3.3 Stablecoin to stablecoin

One fresh offer and ask snapshot; the offer converts directly to the ask denomination after the larger of the two
Tobin taxes. The path reads no virtual-pool state, burns the whole offer, mints the integer ask, and touches no fund
and no NOAH, so its result is independent of the pool unit.

### 3.4 The pool unit

Market's `BasePool` is a denomination-bearing `sdk.DecCoin` and `ArkPoolDelta` a signed decimal in that unit. The unit
is the protocol reference denomination, which `x/oracle` owns and which launches as `axdr`, a feed with no asset that is
never converted. It is not Treasury's liability numeraire, which is NOAH throughout (D24), and no stablecoin's
convertibility depends on it (D22).

Re-pointing the reference is one Oracle message, `MsgSetReferenceDenom`, which runs every wired consumer's rebase,
Market's pool, Treasury's tax cap, and Disbursement's and Claims' reference-unit figures, in the same transaction or
fails whole (D123). Market converts `BasePool` at one fresh Oracle snapshot and rescales `ArkPoolDelta` by the same
factor, so no unit change resets imbalance or reprices without flow (D23). Market's own policy messages refuse a unit
change, so the two cannot drift; there is one virtual pool and no transition object or parallel state.

### 3.5 Commitments out of the Reserve

Governance may commit Reserve NOAH to the Buffer with `MsgFundBuffer` and to Insurance with `MsgFundInsurance` (D27).
Source, destination, and denomination are fixed in code, and the handler reads only the live Reserve balance, never a
target, price, or Market state. Each carries a minimum remaining balance, a per-proposal stale-state guard rather than
a protocol floor: stating zero commits everything, and checking what remains rather than an expected starting balance
means a permissionless deposit cannot fail the proposal. The amount is not capped by the target gap, since targets
route principal and do not cap custody.

The committee's `MsgCommitteeFundBuffer` and `MsgCommitteeFundInsurance` are bounded by the receiving fund's gross
shortfall and the mandate's NOAH floor (§4.5). Governance executes proposals before Market settles the block, so a
commitment lands ahead of that block's settlement.

A commitment of `a` lowers the Reserve by `a` and raises the receiving fund by `a`; supply, liability, quotes, spread,
pool state, and every other fund are unchanged. The Reserve's reopened gap is refilled later by the ordinary
waterfall, which delays Insurance funding and overflow burn, and each proposal states that cost. Commitment is
one-way: there is no Buffer-to-Reserve path (D16). It succeeds under an incomplete valuation.

## 4. Capital

### 4.1 The liability partition

Treasury values the stablecoin liability directly in NOAH (D24), once per block that converts and on every read of
`FundStatus`, never from a cache (D33). The registry decides what counts and how
([Asset](../../x/asset/README.md), [Treasury valuation](../../x/treasury/README.md#liability-and-capital-reads)):

| Bucket | Supply | Valued at |
| --- | --- | --- |
| `priced_liability` | ACTIVE and ISSUANCE_HALTED members with a fresh feed | the Oracle rate |
| `settlement_liability` | SUSPENDED members under an open settlement plan | the plan's committed rate |
| `stale_priced_liability` | members whose feed is stale or absent | the last known rate, kept apart because it failed the freshness gate |
| excluded, disclosed | suspended supply with no plan (`untrusted_suspended_supply`); written-off supply (`written_off_exposure`) | nothing: a write-off extinguishes the obligation, and untrusted supply cannot be valued |

Gross liability is the three valued buckets. Self-held liability is the Reserve's balance of each counted member,
valued through the same branch that counted it, so `net = gross - self_held` is non-negative by construction (D66).

Completeness is read off the disclosure lists, not a flag: a non-empty `stale_member_supply` or
`untrusted_suspended_supply` means a recognised exposure went unvalued, and every target then reports zero, while a
non-empty `written_off_exposure` does not. It depends on outstanding liabilities, never on a policy denomination: a
missing `axdr` rate is irrelevant while `axdr` supply is zero. Arithmetic that leaves the representable domain is not
incompleteness; it fails the block.

### 4.2 Targets and the exposure multiplier

Each fund's target is its governed ratio times the liability basis times the multiplier `m`, rounded up because a
requirement ceils (D72):

```text
target_f = ceil(ratio_f * m * basis)
```

Two bases, assigned by one principle (D67). A flow serves claims that can arrive, and none arrives from paper the
Reserve holds, so the waterfall's gaps size on net. A bound serves discretion, and the discretion-holder can re-issue
that paper, so the bounds on committee acts size on gross. `FundStatus` reports both families, `*_target` on gross and
`*_net_target` on net, beside the multiplier.

The multiplier says the same nominal liability is a larger exposure under stress. It combines three indicators the
chain already maintains, each weighted by a committee lever: the liability ratio, net liability over circulating NOAH;
the annualised realised volatility of the reference rate, measured in block time (D133); and an EMA of net redemption
flow. Every input is a vote-median rate, a Bank supply, or flow that cost spread to produce, so no spot price enters.
It recomputes on a governed period, moves at most a governed step, and is clamped to `[1, cap]`; zero weights pin it
at one. Its own liability-ratio input reads raw liability, since a controller fed its own output would compound.
[Treasury](../../x/treasury/README.md#exposure-sampling-and-refresh) owns the instrument.

### 4.3 Recognition

Treasury computes what each fund needs; each fund's module certifies what it has through `RecognisedCapital` (D54,
D55):

```text
required_capital   = target_f
recognised_capital = what the fund's owner certifies
capital_gap        = max(required_capital - recognised_capital, 0)
```

The Buffer counts its `anoah` at par; Treasury reads it directly, since the Buffer has no operator. Insurance counts its
`anoah` less the NOAH reserved against pending claims, because an approved claim cannot cover a second loss; the
registry paper it holds for stablecoin claims counts for nothing (D28). The Reserve counts its `anoah` at par plus a
haircut credit for each listed asset's open, unimpaired attested quantity:

```text
credit_a = min(haircut_a * attested_quantity_a * rate_a, max_credit_per_noah_a * noah_balance)
```

where `rate_a` is the derived feed's rate within the entry's own staleness window and zero otherwise. The cap reads only
the NOAH balance, which Bank proves, so every credit is independent: a misreported holding or a wrong price adds at most
its own `max_credit_per_noah × noah_balance` and moves no other asset's credit. The policy's values sum to at most one,
so recognised capital never exceeds twice the NOAH the Reserve holds, whatever is attested (D58, D64).

Recognition degrades to zero, never to a stale number: a dark or over-age feed zeroes its own asset's credit, there is
no fallback price, and valuation is never stored (D57, D71). Ark-issued paper earns
nothing wherever it sits, since it is a liability until burned (D28, D62, D69). Custody is attested: the asset sits at
a mandate destination and the committee books its quantity in an append-only journal
([Reserve](../../x/reserve/README.md#mandate-custody-and-journal)).

When the Reserve spends 100 NOAH on an asset recognised at 70 after haircut, a 30 NOAH gap reopens, plus each
capped asset's ratio of the 100. Zero credit would
reopen all 100 and invite an acquisition-and-refill loop that captures expansion principal; full credit would close a
gap that is not closed.

### 4.4 Passive targets and public ratios

- Targets decide only where new expansion principal goes (D7). They are not custody caps: deposits are valid above
  target, and a falling target releases, burns, or moves nothing. No target triggers a debit (§2.2).
- `FundStatus` always answers and never presents a partial target as complete: incomplete valuation zeroes every target
  and lists what it excluded. Refusing to answer during exactly that stress would blind operators. Every liability
  figure it reports is an `anoah` value built from the whole decimal aggregate, never truncated for display.
- Every balance and supply is public, so the protocol states what each ratio means rather than hiding it (D19). The
  Buffer ratio measures recycled NOAH per unit of liability, not backing; the Reserve ratio what it still holds against
  its target; the Insurance ratio unencumbered balance against covered-loss capacity. No ratio crossing any threshold
  changes a quote, spread, minimum receive, or eligibility, and no query publishes a combined health figure.

### 4.5 Bounds on committee acts

The same targets bound what a committee may move or destroy, always in the conservative direction, and `m`, which no
committee can move, tightens all of them (D72):

- The Reserve committee may burn NOAH surplus up to the largest amount whose post-burn recognition still covers the
  requirement, and never below the mandate floor. Clipped credits lever the balance, so the bound is read at the
  balance a burn leaves; it exhausts itself at the target line, leaving the committee timing rather than size. It
  requires a complete valuation, since an incomplete aggregate can understate the requirement (D61).
- The committee may commit to the Buffer or Insurance up to the receiving fund's gross shortfall and above the
  mandate's floor. These bounds use whatever the block could value, because their error only keeps money as protocol
  capital.

What each ratio sets, and how to choose it, is in [genesis §4](../governance/GENESIS.md#4-treasury). Under NOAH-only
custody every fund's exposure has liability as its only base, which is why the ratios are the exposure model and no
per-fund model exists (D8).

### 4.6 Insurance claims

Governance stores one Claims mandate: the shared envelope and a fixed gross claim limit for the term in the reference
unit (D30, D38, D140). Its window and the `x/claims` cancellation period are block time (D133). Replacing or disabling
the mandate advances the term and resets the allowance used, and never touches an existing claim or the reservation.

- **Submission.** A claim pays a positive amount of NOAH or an active registry asset, and must be covered by what
  Insurance holds in that denomination beyond what is already reserved. A stablecoin claim custody cannot cover buys
  the rest through Market with uncommitted Insurance NOAH, inside the submitting message and within the claim's
  `max_offer`. A committee claim permanently charges the limit with what it drew, custody and converted NOAH, in the
  reference unit; a governance claim charges nothing. Submission reserves the amount and sets the closing time from
  the cancellation period.
- **Cancellation.** Before the closing time, governance may cancel any pending claim, and the current committee,
  signing with its exact term, any claim governance did not submit. Cancellation releases the reservation and restores no allowance, so
  approve-and-cancel cycles cannot redirect expansion funding (D32). It never reaches Market: bought paper stays for
  the next claim in its denomination, and the spread stays spent, which is why the limit charges it.
- **Settlement.** At the closing time the `x/claims` EndBlocker pays the immutable recipient and amount, with no
  signer. A claim the chain cannot pay ends `failed` and releases its reservation.

The reservation is an encumbrance, not a movement: submission lowers the unencumbered balance and opens only the
passive target gap, settlement lowers balance and reservation together, and the reservation never exceeds the balance
in its denomination. A stablecoin claim's conversion spends Insurance NOAH at submission, opening the same passive gap
(D28, D140). There is no guardian, pause, category, per-claim cap, or clawback of a paid claim (D32). The
[Claims README](../../x/claims/README.md) owns the record, settlement bounds, and failure handling.

## 5. Transfer tax and fees

### 5.1 Settings

- `Params.transfer_tax_rate`, governance-owned, in `[0, 1]`. Zero disables the tax without deleting anything (D44). It
  is governance's because it is part of the fee every wallet signs: a raise refuses every in-flight transfer until
  clients re-query, and the voting period is the notice that needs (D80).
- `Params.reference_denom` and `Params.reference_tax_cap`: the reference unit, which always equals the Oracle's, and a
  cap amount in it (D21, D37). Zero means uncapped. Changing the amount moves no fund, pool, quote, or coverage figure.
- No tax-rate controller, revenue target, change-rate limit, or rolling indicator exists (D79).

The rate is held at or below the lowest conversion spread floor a conversion committee can reach: NOAH is untaxed and
`MsgSell` is exempt, so above that floor a swap to NOAH, a NOAH send, and a swap back would deliver the same value for
spread instead of tax. Nothing enforces this in code; it is a governance-time constraint on the rate and on every
conversion appointment (D81).

### 5.2 Taxable denominations and operations

Every registered stablecoin with a conversion factor is taxable; NOAH never is (D44). A member awaiting its first
factor is untaxed rather than refused, so missing pricing never blocks a transfer, and a denomination outside the
registry is untaxed. What is taxed is the transfer, so a suspended or written-off denomination stays taxable while it
moves.

One calculator prices every user-facing transfer surface, and internal Bank movements are never taxed (D18): a global
Bank hook would tax settlement, draws, claims, grants, reward funding, and commitments unless it recreated an
exemption system.

| Input | Tax principal |
| --- | --- |
| `bank.MsgSend` | the stablecoins sent |
| `bank.MsgMultiSend` | each output independently, as separate sends (D138) |
| `market.MsgSell` | exempt; a self-returning conversion is governed by spread |
| `market.MsgBuy` | exempt, as `MsgSell`; the offer is sized at execution within the message's maximum |
| `authz.MsgExec` | every nested message, recursively, to a bounded depth |
| vesting account creation | the stablecoin funding |
| Wasm instantiate and execute | attached stablecoin funds |
| contract dispatches | each Bank send, execute funds, and instantiate funds at dispatch (§5.5) |

Escrow, Market settlement, fund movements, claims, and reward distribution are never taxable inputs (D42). Native
Disbursement payments, stablecoin compensation included, are untaxed protocol disbursements (D87), and its conversions
of contributor NOAH are Market settlement (D88).

The tax sees the Bank ledger and nothing built on it: value moving inside a contract's own balances, an exchange, or a
custodian is taxed on the way in and out and never in between. The cap is the defence, bounding what a large mover
saves by leaving the rail; the share of each currency held by contract addresses is the signal that movers are leaving
(D138).

### 5.3 Calculation and caps

```text
uncapped[input][denom] = floor(principal[input][denom] * transfer_tax_rate)
tax[input][denom]      = uncapped                          if cap[denom] == 0
                       = min(uncapped, cap[denom])         otherwise
total[denom]           = checked sum over every taxable input
```

Each input receives its own cap; two sends from one source, two `MultiSend` outputs, and a signed input beside an
execution-generated one are never combined (D17). Malformed or undecodable nested messages fail closed, and totals use
checked addition. The ante's checker, the post decorator's charge, the policy router, and `Query/ComputeTax` call one
implementation.

At the end of each block Treasury derives a conversion factor for every member, one reference unit in that
denomination, so the next block's prices are fixed before it is proposed. The per-denomination tax cap is the reference
cap times the factor, and the per-denomination gas price is the base gas price times the factor; caps are derived on
every read, never stored. A factor persists through a feed outage, so a stale feed defers a cap rather than failing a
block. A positive cap that truncates to zero is clamped to one base unit, never the uncapped sentinel; a product too
large to represent reads as uncapped (D44). [Treasury](../../x/treasury/README.md#conversion-factors-and-dynamic-fees)
owns the factor table.

`Params.reference_denom` equals the Oracle's reference at every height, guarded at the writes: `InitGenesis` refuses a
mismatch, `MsgUpdateParams` refuses a denomination change, and `MsgSetReferenceDenom` is the one path that moves it,
rebasing the cap with every other consumer (§3.4).

### 5.4 The fee: declaration, settlement, charge

The signed fee declares the tax (D78). A transaction's fee is gas plus the exact tax its messages owe, and Ark's
`FeeDecorator` holds the declaration to the computed tax before anything is deducted, so a signer sees the whole charge
in the fee they sign and is never taxed past it. The fee field carries it because it is the one slot every wallet
builds and every sign mode renders, amino included. A wallet that omits the tax is refused with an error naming it;
`arkd` and front ends price every transaction through `Query/ComputeTax` and `Query/GasPrices`
([client fees](../clients/CLIENT_FEES.md)).

The fee is a ceiling and the tip is NOAH (D80). Each stablecoin leg is charged at most what it owes, the exact tax plus
the base fee if it is the first leg, in the order the payer encoded the fee, and the rest never leaves the payer, so a
wallet pads a stable leg for rate drift at no cost. The NOAH leg is charged whole: base fee if no stable leg paid it,
remainder tip, and only the tip ranks for priority. NOAH is the one denomination the tax is never owed in, which is
what lets the chain tell a tip from a padded tax.

The tax is charged after the messages, on success only (D82). The ante deducts the gas fee and tip and carries the
priced tax through the context; `TransferTaxDecorator` charges it to `transfer_tax_collector` after the messages and
draws a granter's allowance for it. A transaction whose messages fail pays gas and no tax; a payer the messages leave
short fails at the charge with gas kept and principal unmoved; a payer who acquires the taxed denomination from an
earlier message in the same transaction is taxed and succeeds. Admission, recheck, and proposal verification check the
declaration and upfront gas fee but never collect tax.
[Ante handling](../../app/ante/README.md#transfer-tax-commitment-and-rollback) owns the outcome table.

Gas is priced by Treasury's base-fee controller, not by node-local minimum gas prices. The base gas price, held in the
reference unit and floored at `min_base_gas_price`, moves each block toward a target utilisation by at most
`base_fee_adjustment_rate`, and each accepted fee denomination, NOAH or a member the Oracle still prices, pays through
its factor ([Treasury fee controller](../../x/treasury/README.md#conversion-factors-and-dynamic-fees)).

### 5.5 Execution-generated tax

A transfer a contract creates while it runs is absent from the signed message list, so the ante cannot see it.
Ownership by call path gives exactly-once assessment with no transfer ID, context marker, or global Bank hook (D40,
D41):

- The ante owns signed top-level inputs, Wasm attached funds included; the fee payer or feegrant granter pays that tax
  as part of the declared fee.
- The execution policy router in `app/execution_policy_router.go` owns everything dispatched. It prices each dispatch
  with the same calculator and charges the dispatching contract on top of the complete principal, sending the tax to
  `transfer_tax_collector` inside the same cached execution, so a failure rolls both back and a recipient's amount is
  never reduced to fund tax (D42). An insufficient balance fails that dispatch.
- Neither the outer fee payer nor feegrant sponsors execution-generated tax.

The router also enforces the policies the ante enforces for signed transactions, the MultiSend fan-out guard, the vote
stake floor, and the consensus-params block-data floor, so the two seams agree. A contract estimates tax through
`Query/ComputeTax` on the query accept list; the estimate reserves nothing and never replaces recomputation at dispatch
(D43, D74).

The contract runtime ships open: anyone may upload and instantiate from height one, and a generated message must name
the calling contract as signer. The accept list's response shapes are consensus inputs, so widening it is a
coordinated binary upgrade, not a parameter vote. There is no IBC (D118).
[Application integration](../../app/README.md#wasm-integration) owns runtime wiring and the accept list;
[governance operations](../governance/GOVERNANCE_OPERATIONS.md#2-the-contract-runtime) owns permissions.

## 6. Validator and Oracle funding

### 6.1 Window and accrual

Gas fees reach `fee_collector` and pass to Distribution every block. Transfer tax accumulates in
`transfer_tax_collector` until Treasury settles the funding window. Validator and Oracle funding have separate
per-block NOAH-value targets in `EconomicPolicy`, but Treasury adds the applicable targets once per observed block and
compares only the window's aggregates, so a quiet block does not spend subsidy that a busy block in the same window
would have covered (D10). Expansion principal funds the three capital funds, never rewards.

`reward_funding_window` is a governance parameter, one chain week by default and capped at `2^32` observations (D34),
and a separate clock from the Oracle's reward window. Each block Treasury adds the then-current targets and values the
non-empty `fee_collector` balance before Distribution empties it, at that block's Oracle rates; a denomination the
registry cannot price counts as zero for that observation. An accumulated figure that leaves the representable domain
fails the block, since a window valued from a partial sum would misallocate real coins
([Treasury](../../x/treasury/README.md#reward-funding-and-genesis)).

### 6.2 Settlement

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

Scarce subsidy is shared by shortfall with the integer remainder to validators (D12). Validator tax and subsidy go to
`fee_collector` for Distribution at the next block's start; Oracle tax and subsidy go straight to the Oracle account.
In effect, gas at or above `V` or tax at or below `O` sends all tax to the Oracle, and only tax above the protected
Oracle floor may replace subsidy on a validator gap.

Before valuing the pot, tax in a written-off or retired denomination routes to the Reserve as inert custody, and tax in
a denomination still awaiting a feed waits for a later window, so no unvalued coin reaches an allocation.

### 6.3 Subsidy and distribution

The `treasury_subsidy_pool` is seeded at genesis, has no mint permission, and covers only the window's aggregate
shortfall. Anyone may extend it by sending `anoah`, which lengthens coverage without changing supply or the targets;
there is no refill, target balance, refund, or conversion path (D9, D25). Depletion never mints, borrows from a fund, or
stops block production. Governance alone may return idle balance to the community pool with `MsgReturnSubsidy`, which
names an amount and a minimum remaining balance; there is no floor parameter, automatic sweep, or committee power over
it (D83).

Distribution's `community_tax` is zero in the launch and testnet genesis artefacts, so nothing skims validator income
(D13); `arkd init` still writes the SDK default. The community pool receives only rounding residue, Treasury never funds
it, `x/protocolpool` is not wired, and governance may sweep it (D14). The Oracle pays out its account over its own
reward windows ([Oracle](../../x/oracle/README.md#11-attendance-settlement)).

## 7. Authority

### 7.1 Treasury params and policy

`Params` holds what governance owns: the reference unit and cap amount, the funding window, the transfer tax rate, the
exposure instrument's decays, cap, step, and period, and the base-fee controller. `EconomicPolicy` holds the
committee's levers: the two block reward targets, the three fund target ratios, and the three exposure weights. The
boundary is stance against machinery: a lever stating how much of something the protocol wants, reversible and safe
between a mandate's bounds, is the committee's; the instrument that computes it, its cadence, and its rails are
governance's (D37). The target ratios lie in `[0, 1]` and are independent stocks whose sum may exceed one, since the
waterfall rations scarce principal. Every governance number feeding halt-class arithmetic is domain-capped in its own
validation, so it is refused at the write rather than discovered in a block.

`EconomicMandate` is the shared envelope, term, committee, and half-open window, plus a minimum and maximum policy. Its
empty-committee form is the disabled state.

### 7.2 Role separation

One principle sorts every message: roles with disjoint powers or different execution semantics get separate messages,
and one message serves several roles only when they are the same action under the same rules. Every committee differs
from governance in staleness guard, allowance metering, or cancellable set, so every role has its own message and
every committee power is enumerable from the proto service. Each handler is one authorisation assertion followed by a
shared keeper core, so effect logic cannot drift between roles.

| Owner | Governance authority | Delegated committee authority |
| --- | --- | --- |
| Treasury | Parameters, appointment, and unrestricted valid economic policy | Policy inside the appointed corridor |
| Claims | Parameters, appointment, submission, and cancellation | Term-limited submission and origin-limited cancellation |
| Disbursement | Operational parameters, committee appointment, member tranches, contributor awards, cancellation of grants, reinstatement, controller recovery, and returns of uncommitted contributor custody | Registrar appointment, suspension, and reinstatement, and compensation within a term allowance, under the committee's term and window; the registrar registers members within the open tranche |
| Reserve | Recognition, appointment, corrections, commitments, and discretionary burns | Bounded deployment, bookkeeping, commitments, and burns |
| Market | Parameters, appointment, conversion policy, and Tobin policy | Conversion corridor and Tobin band |
| Oracle | Parameters, feeds, and reference unit | None |
| Asset | Registry, lifecycle, settlement terms, and appointment | One suspension per asset per term |
| Security | Appointment and standard-module authority | Own upgrade planning and cancellation |

The grants committee awards compensation only; ownership is governance's alone (D113). Its registrar, a single key it
appoints for a window inside its own, registers members within the open tranche and nothing else (D120). The
[Disbursement README](../../x/disbursement/README.md) owns the rest.

Four absences are decisions: no Market message carries a recipient, since a payment in another denomination is a buy
and a Bank send in one transaction, taxed on the send; no fund has a deposit message, since Bank rails and the
recipient restriction suffice; no message settles a claim, since the chain does it; and no message withdraws from the
Reserve to an arbitrary recipient, since every Reserve debit is a fixed commitment, a bounded deployment, or a burn.

Appointment, staleness, and module-specific bounds are documented by the
[module READMEs](../../app/README.md#module-entry-points) and the [shared mandate package](../../pkg/mandate/doc.go).

## 8. Related references

- [Economic decisions](ECONOMIC_DECISIONS.md): each rule's reason, under stable D/P identifiers.
- [Whitepaper](../papers/WHITEPAPER.md): why the design is what it is, and its proofs.
- [Genesis](../governance/GENESIS.md): launch settings, open values, and validation.
- [Governance operations](../governance/GOVERNANCE_OPERATIONS.md): appointments, calibration, and external-asset onboarding.
- [Future changes](../direction/FUTURE_CHANGES.md): deferred engineering and its revisit triggers.
- [Application module map](../../app/README.md#module-entry-points): state, API schemas, and subsystem development.
- [Ante/post handling](../../app/ante/README.md), [client fees](../clients/CLIENT_FEES.md), and
  [Wasm integration](../../app/README.md#wasm-integration): execution enforcement.
