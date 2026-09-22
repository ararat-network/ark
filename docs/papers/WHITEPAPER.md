# Ark: Stability and Capital

Whitepaper · Draft for discussion · 11 September 2026

## Abstract

A monetary protocol must accommodate changing demand, finance the infrastructure that supports its currencies, and
decide how losses are recognised when its commitments cannot be sustained. Ark is a sovereign blockchain that issues a
family of fiat-tracking currencies through conversion with its native asset, NOAH. It combines elastic issuance with
three distinct funds: a Redemption Buffer that reduces the new NOAH required by redemptions, a strategic Reserve that
provides governed intervention capacity, and Insurance that pays approved claims. Expansion proceeds replenish these
funds before any excess is burned. Validator and oracle funding follows a separate budget, using fees, a transfer tax,
and a finite subsidy pool. Governance can delegate specified powers through bounded, expiring mandates. A currency
lifecycle provides for suspension, fixed-rate settlement, write-off, and retirement.

Ark addresses weaknesses exposed by Terra's pursuit of growth and the subsequent abandonment of several mechanisms that
its original stability argument relied upon. It retains the premise of elastic money while making resource allocation,
authority, and failure resolution explicit. This does not eliminate dependence on the market value of NOAH. The
contribution is a defined system for retaining resources, distributing the cost of contraction, and limiting further
exposure when a currency fails.

## Contents

1. [Introduction](#1-introduction)
2. [Terra: growth, commitments, and failure](#2-terra-growth-commitments-and-failure)
3. [The currency system](#3-the-currency-system)
4. [Price discovery](#4-price-discovery)
5. [Conversion and stability](#5-conversion-and-stability)
6. [Capital and loss absorption](#6-capital-and-loss-absorption)
7. [Financing security](#7-financing-security)
8. [Governance and authority](#8-governance-and-authority)
9. [Stress and failure](#9-stress-and-failure)
10. [Network and launch](#10-network-and-launch)
11. [Conclusion](#11-conclusion)

[Appendix A: Economic relationships](#appendix-a-economic-relationships) ·
[Appendix B: Launch snapshot](#appendix-b-launch-snapshot) · [References](#references)

## 1 Introduction

Money is useful because people can make commitments in it. A payment settles a purchase; a balance preserves the ability
to make another. For a currency designed to track an external unit, these uses require confidence that its market value
will remain sufficiently close to that unit across changing conditions. A mechanism that supports the price during
expansion answers only part of that requirement. It must also explain who absorbs contraction, what resources remain
available, and what holders can expect if stability cannot be restored.

Ark approaches these questions as connected parts of one monetary system. Its currencies track regional fiat units.
Users create them by supplying NOAH to the protocol and redeem them for NOAH through a price oracle and a governed
conversion mechanism. NOAH is also the asset used for staking and the unit in which the protocol measures its
obligations. Its holders therefore bear both the opportunity of growing currency demand and the cost of residual
issuance when demand contracts.

That arrangement requires more than an exchange formula. Expansion can be used to reduce native supply, distribute
income, or retain resources. These choices have different consequences for the next contraction. A fund can be available
for immediate redemption, reserved for discretionary intervention, or committed to specific losses; the same balance
cannot serve all three purposes at once. A governance process can authorise an emergency response, but the meaning of
that authority depends on what it may change and how quickly it can act.

Ark makes five commitments under its current rules. Expansion replenishes designated funds before burning excess NOAH.
Redemption uses the Buffer proportionally rather than exhausting it for the first holders to leave. Security funding has
its own finite budget. Delegated powers have explicit scope and expiry. A failed currency can be resolved through
recorded changes to its obligations without deleting its holders' balances.

These commitments concern protocol behaviour. They do not establish a guaranteed exchange price in external markets. The
Buffer holds NOAH, so it cannot independently secure NOAH's purchasing power. Residual redemption issuance can still
reinforce a decline in that purchasing power. Governance can make poor decisions, external prices can become unreliable,
and a technically functioning chain can support a currency that has lost demand. Those possibilities shape the design
rather than appearing only as qualifications to it.

This paper develops the economic argument for technically literate readers without assuming knowledge of the underlying
blockchain framework. It describes the current implementation and the launch policy settled on 11 September 2026. The
repository's subsystem documentation owns the detailed execution contracts; this paper explains how those contracts fit
together and what they can reasonably accomplish. Worked examples illustrate mechanics; launch allocations and settings
are identified separately and are not forecasts.

## 2 Terra: growth, commitments, and failure

### 2.1 The original proposition

Terra's 2019 whitepaper connected monetary stability to adoption. It proposed regional fiat pegs, validator-reported
prices, and conversion between stablecoins and Luna. Validators would absorb short-term contraction through Luna
dilution and receive more predictable compensation over time. Adjustable fees and the share of seigniorage burned were
intended to stabilise their returns; the remaining seigniorage would finance applications that increased use. The paper
explicitly considered recession and presented a severe contraction simulation. It also targeted growing unit mining
rewards and treated adoption as the foundation of durable monetary demand. [1][terra-paper]

The tension was within that proposition. Expanding use was expected to strengthen the asset that would absorb
contraction, while expansion proceeds were directed towards scarcity, rewards, or further adoption. Whether those uses
created enough durable demand to survive a reversal was an economic assumption. The conversion rule could issue Luna; it
could not guarantee a buyer for the Luna issued.

### 2.2 Growth at all costs

The description _growth at all costs_ refers here to a revealed policy priority: preserving expansion and the
attractiveness of Luna increasingly took precedence over the adjustable funding arrangements on which the original
stability argument depended. It is an interpretation of the decisions below, not a claim that the original authors never
considered risk or that every participant shared one motive.

In March 2021, the proposal to burn all seigniorage argued that the community and oracle reward pools were becoming
overfunded. It proposed burning accumulated community funds, directing future seigniorage to burning, and extending
distribution of existing oracle rewards. A stated benefit was a simpler economic narrative connecting stablecoin
creation directly to Luna destruction. Resources accumulated during expansion were treated as excess to distribute
through scarcity rather than inventory to retain for future redemptions. [2][burn-proposal]

Columbus-5 made the policy concrete. Its upgrade specification set the burn weight to one and redirected swap fees to
oracle rewards. This replaced a variable allocation with full seigniorage burning and a different reward source. The
historical distinction matters: the system entering its final expansion was no longer operating the adjustable burn
arrangement described in the whitepaper. [3][columbus-five]

The transfer-based stability fee followed. A January 2022 proposal sought to reduce the tax rate to zero, citing
implementation difficulties and its limited contribution relative to other revenue. Terra's own module documentation
subsequently recorded both the elimination of this tax and the burning of all seigniorage, explaining that the
Treasury's original levers no longer effectively operated. Reducing transaction friction could aid adoption, but it also
removed an independent source of validator income. [4][tax-proposal] [5][classic-treasury]

These changes did not create Luna's exposure to contraction; that exposure was already intrinsic to the conversion
model. They weakened the claim that the original reward-stabilisation design would compensate for it. A mechanism cannot
continue to supply its advertised protection after the inputs that make it work have been fixed or removed.

### 2.3 Capacity expanded with the liability

Conversion policy also adapted to accommodate a larger system. Jump Trading's January 2022 liquidity proposal
recommended a 50 million SDR base pool and a 36-block recovery period, estimating daily mint-and-burn capacity of $293
million. Its justification included the growth of UST and trading activity, alongside an explicit analysis of oracle
manipulation risk. The proposal was an attempt to improve liquidity, not evidence that its authors believed liquidity
was costless. [6][liquidity-proposal]

During the May 2022 depeg, another proposal sought a 100 million SDR pool and an 18-block recovery period, estimating
capacity near $1.2 billion per day. Its stated objective was to retire UST faster through conversion. The proposal
itself recognised the conflict between defending UST and protecting Luna. These figures were the proposers' capacity
estimates, not guaranteed amounts of market demand. [7][crisis-proposal]

Faster conversion can help when traders are willing to absorb the native asset and the obstacle is access to the
conversion mechanism. It cannot create that willingness. In a confidence crisis, expanding the channel also expands the
rate at which stablecoin exits can become native-asset selling pressure. Treating throughput as the remedy for every
loss of demand leaves the system without a clear point at which preserving one peg gives way to containing its cost.

### 2.4 Adoption and the treatment of failure

The quality of demand mattered as much as its scale. Liu, Makarov, and Schoar's reconstruction of the Terra ecosystem
documents Anchor's subsidised deposit returns and repeated replenishment of its yield reserve, including funding
obtained by converting Luna into UST. This connected a reason to hold the stablecoin to continued external support for
its yield. Growth in balances could therefore coexist with a recurring funding deficit. [8][anatomy-run]

There were attempts to address this. Anchor's March 2022 discussion proposed adjusting the deposit rate as its yield
reserve changed. That discussion is evidence that sustainability concerns were recognised before the collapse. It also
illustrates the difficulty of changing expectations once a high, stable return has become a product's central
attraction. [9][anchor-rate]

The eventual revival plan proposed a new chain without the algorithmic stablecoin, removing its monetary modules and
distributing new tokens among several affected groups. Whatever the merits of preserving the application ecosystem, that
was a new allocation negotiated after failure. It was not the execution of a settlement promise holders could have
evaluated before the run. [10][revival-plan]

### 2.5 Requirements for Ark

Ark draws a specific design programme from this history. Expansion must leave resources whose later use is defined.
Security funding must disclose both its recurring revenue and the subsidy it consumes. Conversion policy must recognise
the cost of sustained one-way flow. Emergency authority must be specified before it is needed. When a currency cannot
continue, its remaining obligations must have an explicit route to settlement or write-off.

Ark retains elastic conversion because it permits supply to respond directly to demand. It changes what surrounds that
conversion: principal is retained before it is burned, redemptions recycle inventory, and a failing currency's
open-ended exposure can be replaced by a fixed commitment. These are changes to resource use and decision rules. They do
not establish that the native asset will retain value under every shock.

The governance lesson applies to Ark itself. A published design is only as durable as the institutions maintaining it.
Governance can reduce funding, enlarge conversion capacity, or revise policy. The purpose of explicit accounts, bounded
delegation, and public decision records is to make the consequences visible when those choices are made.

## 3 The currency system

### 3.1 NOAH and the issued currencies

NOAH is Ark's native asset. It secures the chain through staking, can pay transaction fees, and is received or issued
when users convert between NOAH and Ark currencies. There is no scheduled staking inflation or routine issuance to
finance protocol expenses. After the initial allocation, new NOAH is created through currency conversion. Separate
authorised Reserve burns can also reduce existing supply.

The launch currency family comprises units tracking the Australian, Canadian, Singapore, and United States dollars; the
Chinese yuan; the euro; sterling; the Japanese yen; the South Korean won; and the Mexican peso. A holder owns
transferable units recorded on Ark. The protocol offers conversion under its current pricing and lifecycle rules; it
does not give the holder a deposit claim on the corresponding central bank or a right to withdraw fiat from Ark.

The word _liability_ in this paper means the protocol's recognised conversion obligation. It identifies supply that the
protocol values and may redeem under those rules. Registration of a currency creates the framework for that obligation;
supply arises when users convert into it. A registered currency with zero supply has no outstanding monetary liability.
[Ark: Asset][ark-asset]

### 3.2 One valuation convention

Every oracle rate is expressed as NOAH per unit of the denomination being priced. If one unit of currency A is worth
`r_A` NOAH, the value of `x` units is `x × r_A`. Before conversion charges, exchanging A for B gives:

```text
value in NOAH = x × r_A
output in B  = x × r_A / r_B
```

This convention makes obligations and funds comparable in one unit. It also makes native-asset exposure visible: when
NOAH falls against an unchanged fiat unit, the NOAH value of that unit's outstanding currency rises. A constant number
of currency units can therefore become a larger burden on a fixed NOAH balance.

Ark-native denominations use eighteen decimal places. Equations in the main text use display units for readability; the
chain settles whole base units. Appendix A states the distinction between economic formulas and fixed-precision
execution. [Ark: Oracle][ark-oracle]

### 3.3 Observations, currencies, and the reference unit

A price feed is an observation; an issued currency is an obligation. Ark registers them separately. Governance can
establish a feed, observe its operation, and subsequently register the currency it will price. A feed may also serve
reserve valuation without creating a currency. Registering a feed does not grant anyone authority to issue tokens.

The protocol reference initially uses the IMF's Special Drawing Right, identified by XDR. Ark observes this unit but
does not issue an XDR currency at launch. The reference denominates the virtual conversion pool, the transfer-tax cap,
and base gas pricing. NOAH remains the valuation unit for funds and obligations.

Governance can replace the reference through a coordinated re-expression of the values that use it. The pool's imbalance
changes units with its depth, preserving its economic meaning. A reference change requires a freshly priced successor
and, if the outgoing feed is unusable, an explicit outgoing rate supplied by governance. There is no automatic change of
unit in response to a price outage. A future basket reference is a development direction; the launch mechanism uses the
voted XDR feed. [Ark: Oracle][ark-oracle]

## 4 Price discovery

### 4.1 What the oracle observes

The oracle supplies the external rates needed to value currency units in NOAH. For a dollar-tracking currency, the
relevant rate is NOAH per dollar. It is not the secondary-market price of Ark's dollar token. This distinction is
necessary for arbitrage: a token trading below its intended unit must remain convertible by reference to that unit,
subject to spread and lifecycle rules, if conversion is to exert pressure towards the target.

It also establishes a limit. An apparently healthy dollar feed does not establish that Ark's dollar currency trades at
one dollar. The protocol requires separate observation of secondary markets and conversion flows to detect a currency's
distress. The oracle does not automatically identify every broken peg, and a valid price does not override a suspension.

### 4.2 From external markets to consensus

Each validator operates a pricefeed service that obtains observations from configured providers. It normalises pair
orientations, combines provider observations, and resolves routes into the chain's NOAH-per-unit convention. The
validator node reads the resulting snapshot locally and includes its report in a consensus vote extension.

External provider requests take place outside consensus execution. Validators agree on the signed reports carried
through consensus; they do not each query an exchange while executing a block. Every validating node checks the
authenticated reporting evidence and applies the same deterministic aggregation rules. Payload limits and target
membership checks bound the data accepted by this path. [Ark: Pricefeed][ark-pricefeed]

This separation permits provider diversity without making block execution depend on the availability or response order
of external services. It does not make those services trustworthy. If many validators depend on the same incorrect
market or upstream source, their reports can agree for the wrong reason.

### 4.3 Quorum and aggregation

A target must receive sufficient positive-report voting power before it can be priced. The current default requires more
than two thirds of total commit power. Validators with missing or invalid reports remain in the denominator; silence
cannot lower the threshold by removing itself from consideration.

Aggregation selects a temporary reference from the passing targets, preferring one with broad overlap among their
reporting validators. It takes a voting-power-weighted median for that reference. Other prices are derived from weighted
medians of cross rates reported by validators who supplied both the target and the reference. Each usable overlap must
itself satisfy quorum. A target without adequate evidence receives no new price.

This temporary aggregation reference is distinct from the governance-selected XDR protocol reference. The first
organises one set of reports; the second denominates persistent economic settings. Keeping their roles separate allows
aggregation to use the strongest available reporting overlap without changing the protocol's unit of account. [Ark:
Aggregation][ark-aggregation]

### 4.4 Incentives and unavailable prices

Validators earn oracle reward weight for reports within a governed band around the relevant median. Their weight
combines voting power with the number of targets rewarded. Reporting more usable targets can therefore earn more income.
Agreement with the median is an incentive for useful participation, not a proof of external truth.

Attendance is assessed separately. A validator participates when it supplies enough valid positive rates, and a block
counts towards attendance only when sufficient total power participates. Persistent absence while the oracle is
functioning can result in jailing. The oracle attendance mechanism does not slash stake; ordinary consensus penalties
remain separate. When the reporting fleet suffers a sufficiently broad outage, attendance grading stops rather than
treating collective data failure as individual misconduct.

Prices have an explicit freshness limit. Ordinary conversion cannot use a rate that fails it, even when the last value
remains in storage. Other consumers follow their own stated degraded-data rules: liability reporting retains last-known
evidence with disclosure, while external reserve recognition refuses stale credit. Freshness therefore constrains what
an observation may authorise, rather than erasing the history of the observation. [Ark: Oracle][ark-oracle]

## 5 Conversion and stability

### 5.1 The source of stabilising pressure

A currency trading above its reference value creates an opportunity to supply NOAH to Ark, receive newly issued
currency, and sell that currency externally. A currency trading below its reference value creates the reverse
opportunity: acquire it externally, redeem it for NOAH, and sell the NOAH. These trades expand or contract currency
supply in the direction that can reduce the deviation.

The opportunity exists only after accounting for conversion spread, fees, external liquidity, and the risk of prices
moving during execution. A two per cent conversion floor, for example, is material to the deviation needed for a trade
to be worthwhile. Ark's reference price is consequently not a promise that an external order book will quote an exact
peg at every moment. Traders provide the connection between the two markets, and their participation must remain
economically worthwhile.

No individual trader receives a general right to exhaust the protocol's funds. The conversion quote determines their
output; a separate settlement calculation determines how much existing NOAH the system uses to offset the issuance
associated with that output.

### 5.2 A conversion channel that remembers imbalance

Conversions involving NOAH use a virtual constant-product pool. Its two balances are accounting quantities expressed in
the protocol reference unit. They are not deposited liquidity and must not be included in the protocol's assets. The
governed base depth and a persistent imbalance determine the effective balances and the price impact of the next trade.

Sustained flow in one direction moves the pool away from balance and generally increases the cost of continuing that
flow. Opposing flow can relieve the imbalance. Each block also reduces it by a governed fraction, allowing conversion
capacity to recover over time. A minimum spread applies even when calculated price impact is small. Depth, recovery
period, and minimum spread form one conversion policy.

The launch policy sets the pool depth at 5,000,000 XDR, the minimum spread at 2%, and the recovery period at
14,400 blocks, approximately one day. Direct currency exchanges retain a 0.25% default Tobin charge. These settings
govern conversion cost; the fund seeds are separate real balances. [Ark: Launch genesis][ark-genesis]

A larger pool or faster recovery makes conversion less restrictive, but neither supplies real capital. A smaller pool or
slower recovery can moderate conversion pressure while also making arbitrage more expensive. Governance must evaluate
those costs together. The mechanism shapes the price of conversion; it does not impose a fixed daily issuance quota or
guarantee that dilution stays below a chosen daily amount.

Users may specify a minimum acceptable receive amount. If the executed quote falls below it, the conversion fails. Pool
state still changes with transaction order, so earlier trades can affect later quotes within the same block. The
block-level accounting described below removes a separate source of ordering dependence in fund allocation; it does not
remove ordinary transaction-ordering effects. [Ark: Market][ark-market]

### 5.3 Expansion retains principal

When a user supplies NOAH for an issued currency, the protocol receives the gross NOAH offer and mints the quoted
currency output. It retains the offer temporarily for end-of-block allocation. The currency received is worth no more
than the supplied NOAH under the conversion's valuation, with spread and integer rounding accounted for.

The block's combined expansion proceeds follow a fixed order. They first fill the Redemption Buffer's shortfall, then
the strategic Reserve's, then Insurance's. Any amount remaining after those gaps are filled is burned. The targets and
recognised balances determining the gaps are described in Section 6.

For example, suppose a block receives 1,000 NOAH through expansion and its resulting fund gaps are 200, 150, and 50
NOAH. The allocation credits those amounts to Buffer, Reserve, and Insurance respectively and burns 600 NOAH. The 400
retained NOAH remains part of total supply but moves into designated custody. It does not become validator income. This
example assumes a complete liability valuation and omits other activity in the block.

This order is an economic choice. The first use of expansion is to prepare for redemption; discretionary capacity and
approved-loss capacity follow. Insurance can receive little or nothing when earlier gaps remain large. The presence of
three funds therefore does not imply that all three accumulate resources at the same speed.

### 5.4 Redemption recycles inventory

A redemption burns the offered currency and gives the holder the quoted NOAH output. The Buffer reduces the net new
supply required by these redemptions in proportion to its balance relative to the relevant liability basis. For
aggregate output `Q`, Buffer balance `B`, and reconstructed pre-redemption liability `L`, the economic rule is:

```text
Buffer contribution = min(Q, floor(Q × B / L))
net redemption issuance = Q − Buffer contribution
```

The Buffer balance is measured after the block's expansion credits. The liability basis adds the value retired by the
block's redemptions back to the remaining recognised net liability. It includes ordinary and fixed-rate settlement
redemptions, each valued at its own applicable rate. The stress multiplier used for fund targets does not scale this
denominator.

Suppose the liability is 1,000 NOAH and the Buffer holds 200. A redemption extinguishing 100 NOAH of liability and
paying 98 NOAH after spread uses 19.6 NOAH of inventory and creates 78.4 NOAH net. The Buffer then holds 180.4 against
900 of liability, slightly more than the original twenty per cent ratio. The holder receives 98 regardless of the split.
Display decimals here represent whole amounts at the chain's finer base-unit precision.

In the proportional model, isolated redemptions at fixed valuation and membership preserve or improve coverage because
liability falls by at least as much as the output being funded. Appendix A gives the proof and its fixed-precision
qualification. An empty Buffer simply offsets none of the issuance. A Buffer at or above the liability offsets the whole
output, with any excess remaining in custody. There is no final fully-funded redeemer followed by a first completely
unfunded one.

The property is specific. Market price changes, additions to liability, reserve holdings, or lifecycle transitions can
change the ratio between blocks. Predictable proportional use of inventory cannot eliminate the incentive to sell NOAH
before its price falls or redeem a currency before suspension. [Ark: Conversion accounting][ark-economics]

### 5.5 Settlement timing and direct currency exchange

The implementation pays each redeemer by minting their full quoted NOAH during the transaction. At the end of the block,
it transfers the calculated Buffer contribution to Market and burns that amount. The final supply and balances match the
economic result of using inventory and minting only the residual. This timing lets transaction execution honour its
quote while aggregate accounting uses one valuation and one Buffer draw.

Expansion allocation and redemption funding are therefore evaluated together at block settlement. They do not consult
and consume a succession of per-transaction capital snapshots. An accounting failure prevents the block from completing;
successful settlement must reconcile actual custody with the recorded conversion totals.

Users can also exchange one eligible Ark currency directly for another. The protocol burns the offered units and mints
the destination units at their oracle cross rate, less the larger of the two applicable Tobin conversion charges. This
path uses neither NOAH nor the virtual pool and moves none of the three funds. It allows demand to migrate between
currency units without forcing the transition through the native asset. Both sides remain subject to lifecycle
eligibility and usable prices. [Ark: Market][ark-market]

## 6 Capital and loss absorption

### 6.1 Three funds with different purposes

Ark separates resources by the decisions required to spend them. The Redemption Buffer is NOAH inventory used by the
automatic proportional draw. The strategic Reserve holds resources that governance can commit to the Buffer or
Insurance, or authorise a committee to deploy within a mandate. Insurance holds NOAH available for approved claims, less
amounts already reserved for pending payments.

| Fund              | What it measures                                      | How resources leave                                     |
| ----------------- | ----------------------------------------------------- | ------------------------------------------------------- |
| Redemption Buffer | Existing NOAH available to offset redemption issuance | The proportional redemption draw                        |
| Strategic Reserve | Recognised resources available for governed purposes  | Fixed fund commitments, authorised deployment, or burns |
| Insurance         | NOAH available for additional approved losses         | Recorded claim settlement                               |

These accounts are separate because their resources are not interchangeable promises. A redeemer cannot demand that the
Reserve intervene. An Insurance claimant cannot spend the Buffer. A Reserve commitment moves resources from one purpose
to another and must reduce the source balance by the same amount.

The word _capital_ describes retained protocol resources and their recognised value. It does not establish that Ark is
collateralised. In particular, valuing NOAH at one NOAH is an accounting identity, not protection against its falling
value in fiat. The Buffer's ratio measures inventory reuse, not the probability that every currency will retain its peg.
Ark reports the funds separately rather than adding them into a single backing percentage.

### 6.2 Measuring the obligation

Fund requirements start with the currency liability. Supply in active or issuance-halted currencies is valued using
fresh oracle rates. Suspended supply with an activated settlement plan is valued at the plan's committed rate. Supply
whose ordinary feed is stale is recorded separately, with a last-known valuation retained where available. Untrusted
suspended supply without an active settlement commitment is disclosed without assigning an invented rate. Written-off
supply is separately disclosed while the asset remains written off, but no longer recognised as an obligation;
retirement preserves the resolution record.

The distinction between stale and extinguished liability is deliberate. A missing price does not forgive a debt-like
protocol obligation. Last-known evidence remains in the accounting where it exists, accompanied by an explicit statement
that the valuation is incomplete. Conversely, a governance write-off changes the obligation itself and must be
identified as that decision.

Gross recognised liability includes the amounts the chain can value through these branches. The Reserve may hold some of
the same issued currency. Those holdings are valued on the identical basis and subtracted to derive net liability for
flow calculations. Self-held currency receives no capital credit: subtracting an internal claim is different from
counting it as an asset that could support somebody else's claim.

Net liability determines ordinary expansion targets and the redemption draw's reconstructed basis. Gross liability
determines the target-based bounds on a committee's commitments and surplus burns. This prevents the delegated operator
from using its own currency holdings to reduce the requirement constraining its discretion. Both measures, the self-held
amount, and the excluded supplies remain visible. [Ark: Liability accounting][ark-liability]

### 6.3 Targets allocate future inflows

Each fund has a governed target ratio. A bounded exposure multiplier can scale the common liability basis, producing a
target of the form:

```text
fund target = ceil(target ratio × exposure multiplier × liability basis)
fund gap    = max(fund target − recognised fund resources, 0)
```

The launch target ratios are 30% for the Buffer, 15% for the Reserve, and 5% for Insurance. These are retention
requirements, not fixed redemption shares: actual Buffer coverage follows its live balance and the recognised
liability basis. The initial seeds can provide coverage above these targets, while revaluation or losses can leave
resources below them. [Ark: Launch genesis][ark-genesis]

The ratios specify desired stocks of resources. They are independent and need not sum to one; the waterfall allocates
scarce inflows in order. A larger target can retain more of the next expansion, but it cannot manufacture the expansion
or the resources required to fill it.

Targets are not custody ceilings. A fund can receive a deposit above target, and a falling target does not release
resources automatically. Crossing a target changes no holder's quote or conversion eligibility. The Buffer can continue
to be used while below target because redemption follows its own proportional rule. Insurance claims require actual
unreserved funds regardless of the target assigned to Insurance.

The exposure multiplier measures three conditions: net liability relative to circulating NOAH, realised volatility of
the protocol reference rate, and sustained net redemption flow. Governance sets the measuring periods, smoothing, cap,
and maximum movement per update. Economic policy sets the weights given to the indicators within any delegated corridor.
The result is floored at one and moves within bounded steps.

Its effect is to retain more future expansion under greater measured exposure and adjust the target-based bounds on
committee actions. It never orders a trade, creates NOAH, changes a redemption quote, or scales the Buffer payment
denominator. If no expansion arrives, a higher target produces no new resources. The launch configuration sets all three
weights to zero, leaving this response inactive until deliberately enabled and calibrated.

### 6.4 Recognising reserve resources

The Buffer recognises its NOAH balance at par. Insurance recognises its NOAH balance after pending reservations. The
Reserve can recognise NOAH plus a constrained contribution from eligible external holdings. External custody is attested
and recorded in a permanent position journal; sending NOAH to an approved destination is an on-chain fact, while what
the recipient holds afterwards requires separate evidence.

Governance defines eligible holdings, valuation haircuts, acceptable price age, and maximum contribution shares. The
credit given to an external holding is the smaller of its haircut-adjusted value and its allowed share of total
recognised Reserve capital. The combined share limits must sum to less than one. As a result, external attestations can
only extend a provable NOAH base by a bounded amount. An empty NOAH base cannot support a positive recognised total
solely through self-referential attestations.

For example, a Reserve holding 100 NOAH with one eligible position capped at twenty per cent of recognised capital can
recognise no more than 125 NOAH in total, even if an attestation claims a much larger external holding. The external
credit is at most 25. A haircut or a smaller attested position may reduce it further.

An impaired or ineligible position receives no credit, and a stale external price supplies no fallback credit. This
treatment differs from stale liabilities because the prudential direction differs: uncertain assets must not inflate
available resources, while an uncertain obligation must not silently disappear. Recognition is recomputed from current
evidence and policy. It remains an accounting allowance, not a guarantee that an external custodian can return funds
immediately or that an asset can be sold at its reported value. [Ark: Reserve][ark-reserve]

### 6.5 Committing resources and accepting losses

Governance can move Reserve NOAH into the Buffer or Insurance through transfers with fixed sources and destinations. It
specifies an amount and a minimum balance that must remain when the proposal executes. These transfers require existing
NOAH and do not mint currency or alter the virtual pool. There is no ordinary path to withdraw a Buffer commitment back
into the Reserve.

An authorised Reserve committee has narrower powers. Deployments consume a gross term allowance, respect a remaining
NOAH floor, and use approved destinations. Committee commitments to the other funds are limited by the receiving fund's
gross-basis shortfall and the Reserve floor. Committee NOAH burns require recognised surplus, a complete liability
valuation, and preservation of that floor. Governance can make broader decisions through its own messages, subject to
the rules those messages enforce.

A commitment reopens part of the Reserve's funding gap. Later expansion may refill that gap before reaching Insurance or
overflow burning. Intervention therefore has a future allocation cost even when it changes neither current supply nor
current liability. External investment losses similarly reduce recognised resources and can redirect subsequent
expansion towards rebuilding the Reserve.

When the overall liability valuation is incomplete, the ordinary expansion waterfall cannot reliably determine surplus.
Ark parks the whole block's gross expansion proceeds in the Reserve and burns none of them. Redemption funding continues
against the recognised net basis, including last-known liability where available. Suspended untrusted supply without an
active plan contributes no invented value to that denominator. These rules preserve resources without pretending that an
incomplete report is a complete measure of exposure. [Ark: Economic design][ark-economics]

## 7 Financing security

### 7.1 A separate operating budget

Validators and oracle participants perform work whether currency demand rises or falls. Ark funds that work through gas
fees, a transfer tax, and a finite subsidy pool. Expansion proceeds belong to the capital allocation circuit described
above. They do not automatically fund rewards, and the security-funding mechanism cannot draw from the Buffer, Reserve,
or Insurance.

The distinction allows participants to assess two different questions. Capital accounts show what resources support
conversion and intervention. The operating budget shows how long the network can fund its reward targets at a given
level of earned revenue. Growth in a capital account is not evidence that operating costs are covered, and a healthy
subsidy balance is not additional redemption inventory.

There is no protocol promise of a fixed staking yield. Two policy targets state desired aggregate funding per block: one
for validators and one for the oracle. Delegator returns also depend on stake, validator commission, participation, and
the value of the assets distributed. A target expressed in NOAH does not guarantee an equivalent amount of real-world
operating expenditure if NOAH's value changes.

At launch, each founding validator seat adds 0.0175 NOAH per block to the aggregate validator target and 0.0075 to
the oracle target: a 70/30 split of 0.025 NOAH. For `n` seats, the targets are therefore `0.0175n` and `0.0075n`.
Validators that join after launch add nothing by construction; raising the targets as the set grows is a governance
policy choice. This sets the network's funding budget; actual distributions still follow stake, commission, and
oracle performance. The first funding window closes after 100,800 block observations. [Ark: Launch genesis][ark-genesis]

### 7.2 Gas and the transfer tax

Gas pays for transaction execution. A base gas price is expressed in the protocol reference unit and adjusts with block
utilisation, within its bounds. It can rise under congestion and fall under spare capacity, subject to a minimum. This
controller responds to demand for block space rather than a desired validator yield. Supported fee denominations use
oracle-derived conversion factors; existing factors persist through a price outage while the congestion component
continues to operate.

The transfer tax applies to the covered user-facing transfers of registered Ark currencies. It uses a fixed
governance-set rate and a cap derived from an amount in the protocol reference unit. A zero cap means uncapped; a zero
rate means no transfer tax. NOAH transfers are untaxed. Ordinary self-directed conversions have their own conversion
charges and are outside this transfer-tax base.

The launch rate is 0.5%, capped at the equivalent of 1,000 XDR per taxable input. At that rate, the proportional
charge reaches the cap at 200,000 XDR of transferred value. The base gas-price floor is 0.02 XDR per 200,000 gas,
with a 50% utilisation target and a maximum adjustment of 2.5% per block. [Ark: Launch genesis][ark-genesis]

The tax is part of the transaction's declared payment contract. Client-visible transfers and transfers generated during
supported contract or interchain execution are assessed through the relevant execution paths. Successful taxable
transfers pay their tax in addition to the principal; recipients do not silently receive less to fund it. Internal fund
movements are not treated as user transfers. Failed message execution pays applicable gas but does not complete a
transfer-tax payment for the reverted operation.

Governance must evaluate the tax alongside conversion costs because users can change their transfer route. Ark's
recorded policy keeps the rate at or below the minimum conversion spread reachable under a committee's mandate. This
cross-module relationship is a governance review constraint, not an automatically enforced validation rule. A reduction
in tax is a reduction in a funding source and must be assessed as such. [Ark: Fees and funding][ark-economics]

### 7.3 Allocation over a funding window

The protocol accumulates reward targets and the recognised NOAH value of gas fees over a funding window. This lets
strong revenue in one block offset a weak block before subsidy is used. Gas fees themselves continue through the
ordinary validator distribution path; the accumulated values measure how much of the target they have already met.

At settlement, transfer tax first protects the oracle target. Only tax beyond that protected amount can fill a validator
shortfall. Any remaining tax also goes to the oracle. The protocol distributes the actual denominations collected, using
their values to determine the allocation; it does not automatically trade them for NOAH.

The subsidy pool then fills the remaining validator and oracle shortfalls with existing NOAH. If the pool cannot meet
both, it allocates its balance in proportion to the two shortfalls, with base-unit rounding handled so total payment
equals the available pool. Exhaustion leaves targets unmet. It does not trigger new issuance, a transfer from the
capital funds, or an automatic halt.

Consider a window with a validator target of 100 NOAH and an oracle target of 20. Gas contributes 70 of recognised value
and tax contributes 30. Tax allocates 20 to the oracle and 10 to validators, leaving a validator shortfall of 20 for
subsidy. With only 8 NOAH of subsidy, validators receive that 8 and the remaining 12 is unfunded. The shortfall is an
observable result, not an instruction to create the missing money.

### 7.4 Subsidy and the transition to earned revenue

The subsidy pool is seeded through the initial NOAH allocation and can receive later NOAH deposits. With a fixed
combined target of `v + o` NOAH per block, balance `S`, and no qualifying revenue, its approximate coverage is
`S / (v + o)` blocks. Actual payments occur at funding-window boundaries. Changing targets or revenue changes that
runway; a larger launch seed extends it without creating a recurring source of income.

The launch seed is 100,000,000 NOAH. At 0.025 NOAH per block per seat and 5,256,000 assumed blocks per year, each
seat adds 131,400 NOAH to the annual funding target. With no qualifying revenue, replenishment, target changes, or
withdrawals, the seed covers approximately 50.7 years for fifteen seats or 7.6 years for one hundred. These examples
do not fix the number of genesis seats. The opening-price assumption used for bootstrap pricing is one USD per NOAH;
it is not a market-price guarantee or a dollar-denominated reward commitment.

Governance can return subsidy NOAH to the community pool through a fixed-destination transfer stating an amount
and a minimum remaining subsidy balance. The operation neither mints nor changes reward targets, but it shortens
the runway. This authority belongs to governance, not the economic committee. [Ark: Launch genesis][ark-genesis]

The finite pool makes the funding problem visible. Long-term operation at a chosen reward level requires earned revenue,
voluntary replenishment, or a lower target. Governance can choose among those responses, but the accounting does not
assume that future adoption will make the choice unnecessary. A funded NOAH target can still be inadequate for real
operating costs, and inadequate compensation can ultimately threaten validator participation even though subsidy
exhaustion does not itself halt the software. [Ark: Treasury][ark-treasury]

## 8 Governance and authority

### 8.1 Collective authority and bounded delegation

Ark uses stake-based governance for changes to monetary policy, asset lifecycle, appointments, and the wider protocol.
Within the existing binary, governance can act through the messages it authorises and can propose coordinated upgrades
when those rules need to change. It remains subject to current message validation and cannot make an otherwise invalid
state transition valid merely by approving it.

The launch distributes equal staking grants to the founding validator seats, locked for four years and vesting over
the six after, and leaves the remaining unallocated supply in the governance-controlled community pool. Entry after
launch is open: a validator
bonds NOAH it holds, and no proposal grants a seat. The initial trust model is therefore authority-based membership
operating on proof-of-stake machinery until distributed NOAH is bonded; equal grants establish equal starting stake,
while voting and rewards remain governed by the underlying stake-based rules. Section 10.2 sets out the grants and
community allocation. [Ark: Threat model][ark-threat]

Some decisions require a faster response than a full voting cycle. Governance may appoint committees to specific roles,
each with its own address, term, activation height, and expiry height. A committee transaction must name the current
term and execute within the appointment window. Replacement or disabling advances the term, making old prepared
transactions unusable even if the same members are appointed later.

| Role            | Delegated authority                                                                |
| --------------- | ---------------------------------------------------------------------------------- |
| Economic policy | Adjust reward targets, fund ratios, and exposure weights within a corridor         |
| Conversion      | Adjust pool policy and Tobin charges within a corridor                             |
| Reserve         | Deploy, record positions, commit funds, and burn within specific bounds            |
| Claims          | Submit covered claims within a term allowance and cancel eligible committee claims |
| Asset emergency | Suspend an eligible currency once per currency per term                            |
| Security        | Schedule or cancel its own upgrade plan and recover an IBC client                  |

The oracle's feeds and protocol reference remain governance decisions. There is no delegated power to change every
parameter, spend every fund, or submit arbitrary messages with governance authority. Separate roles make it possible to
replace a failed operator without reappointing all the others. [Ark: Authority model][ark-economics]

### 8.2 Why limits differ by decision

A reversible policy adjustment can be constrained by minimum and maximum settings. A deployment needs a gross allowance
and approved destinations because money leaves custody. A claim needs funds reserved against a particular recipient and
a period in which it can be cancelled. A suspension needs speed because delay preserves the very conversion exposure the
action is intended to stop.

These distinctions determine how discretion is bounded. A committee cannot restore a consumed deployment allowance by
cycling funds through the same position. Cancelling a claim frees its reservation but does not restore the committee's
gross allowance. Replacing a Claims appointment resets the new term's allowance without releasing reservations attached
to existing claims. The accounting follows commitments independently of who currently holds office.

The launch appointment policy uses separate 3-of-5 multisignature accounts for Security and Asset emergency, appointed
in the first governance cycle, and 4-of-7 accounts for the deliberative committees when their work is needed. Terms
last one chain year, with reappointment required. Committee keys are dedicated hardware-backed keys, and committee gas
comes from members' liquid floats. The protocol authenticates the appointed address; governance chooses the members
and enacts the appointments. Separate addresses do not prove that memberships or interests are independent.
[Ark: Launch genesis][ark-genesis]

### 8.3 Emergency powers and their limits

The asset emergency committee can suspend ordinary conversion for a currency. It cannot change a settlement rate, write
off holders, restore issuance, change the reference unit, or transfer their balances. One suspension per asset per term
prevents the same appointment from repeatedly undoing governance's recovery of that asset.

The security committee deals with code and connectivity repair. It may schedule an upgrade when the slot is empty or
contains its own plan, cancel its own plan, and invoke the specified client-recovery path. It cannot replace or cancel a
governance-scheduled upgrade. These powers do not give it arbitrary fund-transfer authority, but an upgrade schedule can
still halt the whole chain. The mandate is therefore consequential even though it owns no money. [Ark:
Security][ark-security]

No committee receives a general power to freeze individual holders or selectively stop their Bank transfers. Asset
suspension closes protocol conversion while leaving ownership and transferability intact. Governance retains its broader
responsibility for the network's operating rules, and validators retain responsibility for the software they run.
Neither the mandate system nor a whitepaper can eliminate the possibility of governance capture or coordinated rule
changes.

### 8.4 Making authority usable and inspectable

Emergency authority is useful only if authorised transactions can reach the chain. Ark's default mempool policy reserves
admission capacity and bounded preferential service for eligible committee and governance transactions. Eligibility is
authenticated rather than inferred from an unauthorised claim of urgency. The policy reduces ordinary traffic's ability
to crowd out these classes; it does not guarantee inclusion, protect against every competing privileged sender, or
prevent validator censorship. [Ark: Mempool][ark-mempool]

Appointments, policy changes, claims, transfers, and resolution acts leave public records. The useful question is
whether an observer can reconstruct who authorised an action, which limit applied, and where the resources went.
Transparency cannot reverse an executed loss, but it gives replacement and correction a factual basis.

## 9 Stress and failure

### 9.1 Contraction without a currency failure

Ordinary contraction reduces issued supply through redemption. The Buffer offsets a share of new NOAH issuance; the
virtual pool increases the cost of sustained one-way flow; fees continue to accrue; and subsidy covers operating
shortfalls while it lasts. None of these mechanisms requires stablecoin supply to grow in the same period.

They nevertheless consume or redistribute finite capacity. Buffer-funded redemption releases NOAH from protocol
inventory into holder balances, so circulating supply can rise even when total supply does not. Residual issuance
increases total supply as well. If holders sell that NOAH, the external market must absorb it. A falling NOAH price then
increases the NOAH value of the remaining fiat-denominated liability.

This feedback cannot be resolved by accounting alone. A Reserve commitment can improve the next Buffer draw, and an
exposure adjustment can retain more future inflows, but neither guarantees external demand for NOAH. A system-wide loss
of native-asset confidence can overwhelm the intended moderation. The protocol's failure procedures address specific
currency obligations; they do not provide a separate currency in which NOAH holders are guaranteed recovery.

### 9.2 Unavailable or unreliable prices

A feed outage closes ordinary conversion paths that need the missing fresh rate. Other currencies with usable rates can
continue, subject to any shared reference requirement. A stale protocol reference may close all NOAH-pair paths that
depend on it, while direct exchanges between freshly priced currencies can remain available.

Liability reporting distinguishes fresh values, stale evidence, untrusted supply, and obligations already written off.
Incomplete valuation prevents an ordinary surplus burn and parks expansion in the Reserve. External reserve credit
vanishes when its evidence is unusable. These are explicit degraded-data states rather than a general rule that every
unavailable input stops the chain.

Some failures must still stop execution. Corrupt custody, invalid state, or arithmetic outside the supported domain
cannot safely be converted into a partial payment. Checked arithmetic makes such failures diagnosable; input bounds and
validation are what seek to prevent them from reaching block execution. Continued operation under an expected price
outage is different from continued operation with inconsistent financial records. [Ark: Threat model][ark-threat]

### 9.3 A currency lifecycle

Ark distinguishes an orderly stop to issuance from a loss of confidence in ordinary redemption terms.

| State           | Issuance and ordinary conversion                                   | Holder position                                                                   |
| --------------- | ------------------------------------------------------------------ | --------------------------------------------------------------------------------- |
| Active          | Currency can be issued, exchanged, and redeemed with usable prices | Ordinary conversion rights under current rules                                    |
| Issuance halted | Currency can be consumed but cannot be newly produced              | Redemption continues; issuance can later resume                                   |
| Suspended       | Ordinary conversion is closed                                      | Balances transfer; an activated plan may provide fixed-rate settlement            |
| Written off     | Conversion is closed and the obligation is derecognised            | Balances transfer; the loss and residual supply remain recorded                   |
| Retired         | Terminal state; the denomination cannot be reused                  | Any permitted residual balances transfer without a protocol redemption obligation |

Halting issuance is appropriate when further creation should stop while existing holders retain their exit. It does not
contain a run through the redemption leg. Suspension closes that leg as well and is the asset emergency committee's
delegated response. Governance can perform either action through its own authority.

Suspension takes effect when its transaction executes. The code cannot guarantee how quickly distress will be detected,
signatures gathered, or the transaction included. A mandate can support a response within minutes only if its operators
and the network can actually deliver it. Until execution, valid redemptions continue under the current rules; a public
pending suspension can create incentives to redeem ahead of it.

The action preserves balances and transfers. It changes what the protocol will issue or accept, rather than confiscating
units from holders. That preserves evidence and the ability to trade claims externally, but it cannot preserve their
market value. [Ark: Asset lifecycle][ark-asset]

### 9.4 Fixed-rate settlement

Governance may open a settlement plan for a suspended currency, or reinstate a written-off currency into suspension with
a plan. The plan specifies a fixed NOAH redemption rate and an earliest closing height. Activation follows a governed
delay. Before activation the plan can be cancelled; after activation its terms cannot be amended and write-off cannot
occur before the announced earliest closing height.

Holders settle by surrendering and burning currency for the committed NOAH amount, rounded to whole base units.
Settlement does not use the suspended currency's oracle rate, the virtual pool, or an ordinary conversion spread. It
joins the same aggregate Buffer-funding mechanism as other redemptions. The part not offset by inventory is new NOAH
issuance.

Because suspended currency cannot be newly issued and the plan rate is fixed, the maximum aggregate entitlement is
bounded by the outstanding supply times that rate. For example, one million remaining units settled at 0.2 NOAH each
commit no more than 200,000 NOAH of gross output. Buffer use may reduce the net issuance needed, but the plan does not
require that amount to be pre-funded in escrow. Governance is accepting a bounded potential dilution cost.

The fixed commitment is in NOAH. Its external purchasing power can change during the settlement window, and the rate may
recognise only part of the currency's former reference value. An earliest closing height protects time to exercise the
stated exit; it is not an automatic expiry. A later governance act is needed to write off the residual.

Recovery is a separate route. Governance can restore an asset to ordinary redemption, initially with issuance still
halted. Recovery closes any settlement plan and returns pricing to the ordinary oracle rules. Thus the plan is immutable
while it operates, but recovery can replace it with the ordinary conversion regime; it does not preserve the frozen rate
as an additional option. [Ark: Settlement][ark-settlement]

### 9.5 Write-off, retirement, and insurance

A write-off records that the protocol no longer recognises the remaining obligation. It closes any settlement plan
subject to the promised window and records outstanding supply without removing balances from holders. A later governance
decision can reinstate a written-off asset for recovery or settlement. Retirement is terminal and leaves a permanent
record that prevents reuse of the denomination.

Residual supply requires an explicit resolution path. A suspended asset cannot simply be retired with positive supply in
place of acknowledging a write-off. An orderly issuance-halted wind-down can retire within an expressly approved
residual-supply bound. These records distinguish an obligation that was paid, one that remains unresolved, and one that
governance decided to extinguish.

Insurance provides another response to covered losses, but holding an Ark currency does not create automatic eligibility
or a guaranteed payout. Governance or the authorised Claims committee decides which claims to submit. Each claim fixes a
recipient and NOAH amount, reserves existing unencumbered Insurance funds, and enters a cancellation period. Committee
submissions must also fit their remaining gross allowance and close within the term.

Before the closing height, governance can cancel a pending claim; the committee's cancellation power is narrower. At the
closing height the chain pays automatically, without requiring the recipient to submit another transaction. A claim that
cannot be paid under the specified record and custody checks ends as failed and releases its reservation. Reservations
prevent the same NOAH being promised twice, but they cannot make an adjudication fair or an unfunded loss disappear.
Insurance payment in NOAH also retains exposure to NOAH's market value. [Ark: Claims][ark-claims]

## 10 Network and launch

### 10.1 A monetary application on a sovereign chain

Ark uses Cosmos SDK application modules, CometBFT consensus, and NOAH staking. The current implementation uses Cosmos
SDK v0.54.3. Oracle vote extensions are part of the block-processing pipeline, and the transaction gas budget is finite.
Six-second blocks are the assumption used to translate many policy windows into hours or days; elapsed wall-clock time
still depends on actual block production. [Ark: Application][ark-app]

The chain supports CosmWasm contracts and IBC interoperability. Contracts can build services using the issued
currencies, but their balances and promises do not become protocol liabilities merely because they run on Ark. Likewise,
a bridged token is not an Ark-issued currency and does not automatically qualify for conversion or fund recognition.
Contract-generated and supported interchain transfers must follow the applicable execution-tax rules.

The curated launch configuration allows contract upload and instantiation while keeping IBC closed through an empty
client-type allowlist and disabled transfer and interchain-account settings. Opening external routes is a governance and
operational sequence, including the intended rate limits. Installed interoperability code is not evidence that a live
route is enabled or that its counterparty is safe.

### 10.2 Launch allocation and validator seats

Ark starts with 1,000,000,000 NOAH. Of this, 100,000,000 funds the subsidy pool, 50,000,000 the Reserve, 10,000,000
the Buffer, and 5,000,000 Insurance. The remaining 835,000,000 enters the community pool before validator-seat grants.
There is no separate investor, backer, or public allocation at launch. The initial supply can subsequently change
through conversion and authorised burns; it is not a permanent maximum supply.

Each founding validator seat receives 5,000,000 NOAH in a staking grant that vests continuously from the fourth year
after genesis to the tenth, and 300,000 NOAH of liquid float, both from the community pool. The grant is
self-delegated at genesis and can participate in staking and governance and be slashed, but cannot be transferred
until it vests. The float pays operating expenses and permits initial currency conversion.
For `n` seats, the community pool retains `835,000,000 − 5,300,000n` NOAH. Seat assembly transfers existing supply
and raises both reward targets by the per-seat shares in Section 7.1; it creates no additional NOAH.

The validator-set cap is one hundred, unbonding takes twenty-one days, and the minimum commission is 5%. Governance
uses a 50% quorum, a 66.7% ordinary approval threshold, and a 75% expedited threshold, with the SDK's applicable
tally rules. Ordinary voting lasts two days and expedited voting one day; deposits are 1,000 and 5,000 NOAH
respectively. Evidence age is aligned with unbonding. The settlement activation delay is three days, allowing a
normal governance vote to cancel a mistaken plan before activation; claims retain a one-week cancellation period.

The economic settings are settled. What remains at genesis assembly is to add the actual validator seats and their
transactions, derive aggregate reward targets from the seat count, and set the start time. The zero aggregate targets
in the base artefact represent zero assembled seats. Empty mandates implement the decision to appoint committees
after launch, following the schedule in Appendix B. [Ark: Launch genesis][ark-genesis]

### 10.3 What remains to be established

Correct implementation establishes that state transitions follow the rules. It does not establish demand, liquidity,
adequate validator compensation, reliable emergency response, or appropriate economic calibration. Evaluating a launch
requires scenarios that combine falling NOAH prices, asymmetric conversion, provider outages, delayed governance, and
reserve impairment, with explicit assumptions about external liquidity and participant behaviour.

This paper supplies accounting relationships and conditional properties, not an empirical claim that Ark has survived
those conditions in a live market. A derived basket currency and other deferred changes remain future work until
implemented and deliberately adopted. Subsequent versions of the paper should distinguish evidence obtained from
operation from assumptions retained from design.

## 11 Conclusion

Ark's monetary design connects issuance, retained resources, operating revenue, and the authority to recognise losses.
Expansion builds designated inventory before reducing NOAH supply. Redemption uses that inventory proportionally and
creates only the residual net supply. Security funding follows a separate budget whose subsidy can run out. Committees
act through bounded terms, and a failing currency has explicit routes through suspension, settlement, write-off, and
retirement.

These rules make commitments inspectable across expansion and contraction. They do not remove the market's role in
valuing NOAH, restore resources already lost, or prevent governance from choosing a different course. Their value lies
in making those limits part of the system participants can evaluate before they commit to it.

A credible monetary protocol must be able to explain both how it intends to succeed and what it will do when an
assumption fails. Ark places those explanations in its conversion rules, its accounts, and its allocation of authority.
Its claim is to a defined and governable treatment of monetary risk, with the adequacy of that treatment remaining open
to evidence and correction.

## Appendix A: Economic relationships

These relationships summarise the main text in economic notation. The implementation remains authoritative for integer
arithmetic, price eligibility, and execution order. They are not a second normative specification.

### A.1 Units and symbols

| Symbol              | Meaning                                                           |
| ------------------- | ----------------------------------------------------------------- |
| `r_i`               | NOAH per unit of currency or reference denomination `i`           |
| `s_i`, `h_i`        | Outstanding supply and Reserve-held supply of currency `i`        |
| `L_g`, `L_n`        | Gross and net recognised liability in NOAH                        |
| `G`                 | Gross NOAH received in the block's expansions                     |
| `R`, `Q`            | Liability retired and NOAH output paid by the block's redemptions |
| `L`                 | Redemption basis, equal to post-redemption `L_n + R`              |
| `B`                 | Buffer NOAH after expansion credits, before the draw              |
| `p`, `N`            | Buffer contribution and residual net redemption issuance          |
| `a_f`, `T_f`, `C_f` | Target ratio, target amount, and recognised resources of fund `f` |
| `m`                 | Bounded exposure multiplier                                       |
| `S`                 | Available subsidy NOAH                                            |

### A.2 Liability and targets

Each included currency uses the rate assigned by its pricing branch: fresh oracle, active settlement, or separately
identified last-known evidence. Unvalued exposures remain disclosure entries rather than terms with fabricated rates.

```text
L_g = Σ_i s_i × r_i
L_n = L_g − Σ_i h_i × r_i

T_f = ceil(a_f × m × basis)
gap_f = max(T_f − C_f, 0)

basis = L_n for ordinary expansion allocation
basis = L_g for target-based committee bounds
```

Incomplete valuation disables the ordinary target-based expansion allocation. Published targets are zeroed with explicit
disclosure rather than presented as valid partial requirements; actual balances are unaffected.

### A.3 Conversion flows and supply

```text
credit_Buffer    = min(G, gap_Buffer)
credit_Reserve   = min(G − credit_Buffer, gap_Reserve)
credit_Insurance = min(G − credit_Buffer − credit_Reserve, gap_Insurance)
overflow         = G − credit_Buffer − credit_Reserve − credit_Insurance

p = min(Q, floor(Q × B / L))
N = Q − p

change in total NOAH from conversion = N − overflow
```

The supply identity excludes separate Reserve burns and any change to protocol rules. During incomplete valuation,
`credit_Reserve = G`, the other expansion credits and overflow are zero, and the draw still uses the available
recognised net basis. Direct issued-currency exchanges create no NOAH.

### A.4 Conditional coverage property

Consider only redemptions, with fixed rates and liability membership, no other balance movements, `0 ≤ B < L`, and
`0 < R < L`. Valid outputs satisfy `Q ≤ R`. Before base-unit rounding, let `c = B/L` and `p = cQ`. Then:

```text
(B − p)/(L − R) − B/L = c × (R − Q)/(L − R) ≥ 0
```

In exact arithmetic, flooring the contribution retains additional Buffer inventory. When `B ≥ L`, the contribution is
capped at the actual output; excess inventory is not awarded to the last redeemer. At `R = L`, there is no remaining
liability against which to define a coverage ratio.

The proof does not cover price revaluation, changes in which liabilities are counted, expansion, or discretionary
movements. It describes the proportional mechanism rather than a bound on secondary-market outcomes. In execution, the
fixed-decimal quotient followed by integer truncation can differ from the exact rational floor by one base unit near a
rounding boundary. The implementation retains exact custody and output bounds: `0 ≤ p ≤ min(B,Q)`. [Ark: Arithmetic
contract][ark-treasury]

### A.5 External Reserve recognition

Let `B_R` be Reserve NOAH, `u_j` an attested eligible quantity, `d_j` its haircut factor, and `k_j` its share cap. Rates
must satisfy the holding's freshness policy; ineligible or unusable evidence earns zero credit.

```text
credit_j = min(d_j × u_j × r_j, k_j × C_R)
C_R = B_R + Σ_j credit_j

Σ_j k_j < 1
C_R ≤ B_R / (1 − Σ_j k_j)
```

The cap restricts recognised credit even if reported quantities are arbitrarily large within their allowed domain. It
cannot prove custody or ensure liquidation proceeds. Ark-issued currency contributes no such credit.

### A.6 Reward funding

For one funding window, let `V` and `O` be the accumulated validator and oracle targets, `F` eligible gas-fee value, and
`T` priced transfer-tax value, all measured in NOAH.

```text
validator gas gap      = max(V − F, 0)
protected oracle tax   = min(T, O)
desired validator tax  = min(T − protected oracle tax, validator gas gap)
```

The desired validator fraction is applied to each tax denomination, rounding its allocation down. The oracle receives
the remainder. The actual coin allocations are revalued to obtain the two remaining shortfalls `U_V` and `U_O`. A
sufficient subsidy pays both; otherwise:

```text
oracle subsidy    = floor(S × U_O / (U_V + U_O))
validator subsidy = S − oracle subsidy
```

No shortfall requires no subsidy. With constant positive per-block targets `v + o`, zero qualifying revenue, and no
replenishment or withdrawals, `S/(v+o)` is the approximate funded number of blocks, subject to window settlement and
rounding.

## Appendix B: Launch snapshot

The launch policy was settled and checked against the curated genesis on 11 September 2026. **Decided** identifies
an explicit launch choice; **Default** identifies a retained module or SDK default confirmed in that review.
**Assembly** identifies values derived when the validator seats and final start time are added. No economic setting
in this snapshot remains an open launch-policy decision. Block-to-time translations assume six-second blocks; the
maintained [launch record][ark-genesis] owns the full configuration and assembly procedure.

### Supply and operating budget

| Area | Launch value | Status |
| --- | --- | --- |
| Initial supply | 1,000,000,000 NOAH | Decided |
| Subsidy pool seed | 100,000,000 NOAH; 10% of initial supply | Decided |
| Reserve seed | 50,000,000 NOAH; 5% | Decided |
| Buffer seed | 10,000,000 NOAH; 1% | Decided |
| Insurance seed | 5,000,000 NOAH; 0.5% | Decided |
| Community pool | 835,000,000 NOAH before seats; subtract 5,300,000 per seat | Decided |
| Seat staking grant | 5,000,000 NOAH, self-delegated at genesis, vesting from year four to year ten | Decided |
| Seat liquid float | 300,000 NOAH | Decided |
| Validator funding target | 0.0175 NOAH per block per seat | Assembly |
| Oracle funding target | 0.0075 NOAH per block per seat | Assembly |
| Transfer-tax rate and cap | 0.5%; 1,000 XDR per taxable input | Decided |
| Buffer, Reserve, Insurance target ratios | 30%, 15%, 5% | Decided |
| Reward funding window | 100,800 blocks, approximately one week | Decided |
| Distribution community tax | 0; the community pool is separately seeded | Decided |

For fifteen seats, the allocations are 75,000,000 NOAH in seat grants, 4,500,000 liquid NOAH, and 755,500,000 NOAH
remaining in the community pool, alongside the four fund seeds. For one hundred seats the corresponding amounts are
500,000,000, 30,000,000, and 305,000,000. These are illustrations of the fixed allocation rule, not an announced
initial seat count. The subsidy's zero-revenue runway is approximately 50.7 and 7.6 years respectively under the
unchanged per-seat targets, before any subsidy return to the community pool.

### Currency, conversion, and pricing

| Area | Launch value | Status |
| --- | --- | --- |
| Currency family | AUD, CAD, CNY, EUR, GBP, JPY, KRW, MXN, SGD, USD; eighteen decimals | Decided |
| Currency names and symbols | ArkAUD / arkAUD and the corresponding currency codes | Decided |
| Protocol reference | XDR feed; no issued XDR currency | Decided |
| Routine NOAH issuance | No mint module or staking inflation | Decided |
| Conversion pool depth | 5,000,000 XDR; initially balanced | Decided |
| Conversion recovery | 14,400 blocks, approximately one day | Default |
| Minimum conversion spread | 2% | Default |
| Direct currency conversion charge | 0.25% default Tobin rate; no overrides | Decided |
| Oracle price quorum | `0.666666666666666667` of total commit power | Default |
| Oracle reward band | 2% total width around the relevant median | Default |
| Ordinary price freshness | 60 seconds | Default |
| Oracle reward and attendance windows | 100,800 blocks, approximately one week | Default |
| Oracle reward distribution horizon | 1,310,400 blocks, approximately one quarter | Decided |
| Oracle participation floor | 20% of targets; at least one positive rate | Default |
| Functioning-block threshold | 50% of commit power participating | Default |
| Minimum window attendance | 5% of eligible blocks | Default |
| Claim cancellation period | 100,800 blocks, approximately one week | Decided |
| Settlement activation delay | 43,200 blocks, approximately three days | Decided |
| Exposure weights | All zero; initial multiplier one | Decided |
| Exposure refresh | 600 blocks, approximately one hour | Default |
| Exposure multiplier cap and maximum step | 2 and 0.1 per update | Decided |
| Volatility and flow decay | 0.99995 and 0.99885 per block | Default |
| External Reserve recognition | Empty policy; NOAH recognised at par | Decided |
| Base gas-price floor | 0.02 XDR per 200,000 gas | Default |
| Base-fee utilisation target and maximum adjustment | 50%; 2.5% per block | Default |
| Initial NOAH fee-conversion factor | 1.371 NOAH per XDR, replaced by usable oracle observations | Decided |

The initial fee factor reflects the bootstrap assumption of one USD per NOAH and 1.371 USD per XDR. It supplies a
fee conversion before the first usable reference observation and is not a promise of either market exchange rate.
The oracle distribution horizon smooths payments from the oracle reward account; it is separate from the weekly
window that funds that account.

### Consensus, governance, and access

| Area | Launch value | Status |
| --- | --- | --- |
| Chain identifier and initial height | `ark-1`; height 1 | Decided |
| Genesis time and validator transactions | Added to the final file during assembly | Assembly |
| Consensus authority | Governance module address | Decided |
| Block gas budget | 100,000,000 | Decided |
| Block byte budget | 22,020,096 bytes, or 21 MiB | Default |
| Vote extensions | Enabled from height 1 | Decided |
| Validator cap and unbonding | 100 validators; 21 days | Default |
| Minimum validator commission | 5% | Decided |
| Evidence maximum ages | 302,400 blocks and 21 days | Decided |
| Evidence byte limit and consensus key type | 1 MiB; ed25519 | Default |
| Governance ordinary and expedited deposits | 1,000 and 5,000 NOAH | Decided |
| Governance initial deposit ratio | 10% | Default |
| Governance quorum and approval thresholds | 50% quorum; 66.7% ordinary, 75% expedited approval | Decided |
| Governance veto threshold | 33.4% | Default |
| Voting and deposit periods | Two-day ordinary vote; one-day expedited vote; two-day deposit period | Default |
| Consensus signing window and minimum attendance | 10,000 blocks; 5% signed | Decided |
| Downtime jail and slash | 600 seconds; 0.01% | Decided |
| Double-sign slash | 5% | Decided |
| Transaction signature limit | Seven member keys | Decided |
| IBC | No admitted client types; transfers and interchain accounts disabled | Decided |
| Contract upload and instantiation | Open | Decided |

Governance percentages retain the SDK's tally rules, including the treatment of abstentions; they do not replace
stake weighting with a separate seat-counting implementation. Consensus attendance and slashing are separate from
oracle attendance jailing. Evidence expires only once both its height and duration bounds have been exceeded.

### Committee appointment schedule

All mandates are deliberately empty in genesis. The launch policy fixes the intended account forms and appointment
triggers; actual addresses, members, and mandate windows are enacted through later governance proposals.

| Committee | Appointment trigger | Account form |
| --- | --- | --- |
| Security | First governance cycle | 3-of-5 multisignature |
| Asset emergency | First governance cycle; separate account and, where possible, separate members | 3-of-5 multisignature |
| Economic policy | After the first expansions; exposure weights remain zero until calibration | 4-of-7 multisignature |
| Claims | Once Insurance has a charter specifying covered perils | 4-of-7 multisignature |
| Conversion | When governance chooses to delegate conversion policy | 4-of-7 multisignature |
| Reserve | When a deployment is approved | 4-of-7 multisignature |

Appointments use dedicated hardware-backed keys, accounts registered before appointment, gas funded from members'
floats, and one-chain-year terms followed by reappointment. The initial economic corridor is to bound reward targets
between half and twice the seat-scaled launch values and fund ratios between half and one and a half times their
launch values, with Insurance capped at 10%. Exposure weights stay at zero until calibration authorises their use.
These are appointment instructions; an empty mandate grants no committee authority.

The base artefact already holds all five seeded balances and the full initial supply. Assembly moves each seat's
grant and float out of both the Distribution balance and its community-pool ledger, adds the seat's reward shares,
collects validator transactions, and sets the genesis time. The start must be scheduled at least forty-eight hours
after publication of the final file. Supply remains 1,000,000,000 NOAH throughout assembly. Full procedures and final
validation checks remain in [GENESIS.md][ark-genesis].

## References

### Historical sources

1. Kereiakes, E., Kwon, D., Di Maggio, M., and Platias, N. [_Terra Money: Stability and Adoption_][terra-paper].
   April 2019. Sections 2 and 3 describe monetary policy, validator incentives, and adoption funding.
2. Kwon, D. [_Proposal to burn all seigniorage_][burn-proposal]. Terra Research Forum, 3 March 2021.
3. Kwon, D. [_Columbus-5 Mainnet Upgrade Proposal and Recommendations_][columbus-five]. Terra Research Forum, 11
   August 2021. The upgrade specification identifies the new burn and oracle-reward treatment.
4. TheIntern. [_Proposal to Reduce the Terra Tax Rate to Zero_][tax-proposal]. Terra Research Forum, 6 January 2022.
5. Terra Classic documentation. [_Treasury_][classic-treasury]. Historical module description documenting the cessation
   of the original tax and seigniorage levers. This is evidence about the pre-collapse policy, not a claim about
   present-day Terra Classic settings.
6. Jump Trading. [_Liquidity Parameters — 3_][liquidity-proposal]. Terra Research Forum, 29 January 2022.
7. gs390. [_Help UST Pegging: Increase estimated minting capacity to $1200M_][crisis-proposal]. Terra Research Forum, 11
   May 2022. Capacity estimates are attributed to the proposal.
8. Liu, J., Makarov, I., and Schoar, A. [_Anatomy of a Run: The Terra Luna Crash_][anatomy-run]. Working-paper version
   hosted by Sveriges Riksbank for its 2023 conference; see the analysis of Anchor's yield reserve and governance.
9. bitn8. [_Dynamic Anchor Earn Rate_][anchor-rate]. Anchor Protocol forum, 1 March 2022, subsequently edited.
10. Kwon, D. [_Terra Ecosystem Revival Plan 2_][revival-plan]. Terra Research Forum, first posted 16 May 2022; amended
    proposal marked passed by the source.

### Ark design and implementation sources

The current working checkout, including uncommitted development, is the implementation basis for this draft. The
following maintained documents explain the contracts summarised here; their linked source files define the executable
behaviour.

- [Economic design][ark-economics]: conversion allocation, targets, fees, funding, and authority boundaries.
- [Asset][ark-asset], [Market][ark-market], and [Oracle][ark-oracle]: currency lifecycle, conversion, and price state.
- [Oracle aggregation][ark-aggregation] and [Pricefeed][ark-pricefeed]: consensus reports and external observations.
- [Treasury][ark-treasury], [Reserve][ark-reserve], and [Claims][ark-claims]: financial calculations and custody.
- [Security][ark-security], [Application][ark-app], and [Mempool][ark-mempool]: emergency authority and execution.
- [Threat model][ark-threat]: trust boundaries and their controls.
- [Launch genesis][ark-genesis] and [curated genesis data](../../app/genesis/genesis.json): configuration and status.
- Direct source checks include [conversion settlement](../../x/treasury/keeper/settlement.go), [liability
  valuation][ark-liability], [fixed-rate redemption](../../x/market/keeper/settle.go), and [asset settlement
  plans][ark-settlement].

[terra-paper]: https://assets.website-files.com/611153e7af981472d8da199c/618b02d13e938ae1f8ad1e45_Terra_White_paper.pdf
[burn-proposal]: https://classic-agora.terra.money/t/proposal-to-burn-all-seigniorage/438
[columbus-five]: https://classic-agora.terra.money/t/columbus-5-mainnet-upgrade-proposal-and-recommendations/1840
[tax-proposal]: https://classic-agora.terra.money/t/proposal-to-reduce-the-terra-tax-rate-to-zero/3524
[classic-treasury]: https://classic-docs.terra.money/docs/develop/module-specifications/spec-treasury.html
[liquidity-proposal]: https://classic-agora.terra.money/t/liquidity-parameters-3/3895
[crisis-proposal]:
  https://classic-agora.terra.money/t/proposal-help-ust-pegging-increase-estimated-minting-capacity-to-1200m/6287
[anatomy-run]:
  https://www.riksbank.se/globalassets/media/konferenser/2023/session-1-liu_makarov_schoar-anatomy_of_a_run-_the_terra_luna_crash.pdf
[anchor-rate]: https://forum.anchorprotocol.com/t/dynamic-anchor-earn-rate/3042
[revival-plan]: https://classic-agora.terra.money/t/terra-ecosystem-revival-plan-2-passed-gov/18498/1
[ark-economics]: ../design/ECONOMIC_DESIGN.md
[ark-asset]: ../../x/asset/README.md
[ark-market]: ../../x/market/README.md
[ark-oracle]: ../../x/oracle/README.md
[ark-aggregation]: ../../abci/oracle/README.md
[ark-pricefeed]: ../../pricefeed/README.md
[ark-liability]: ../../x/treasury/keeper/liability.go
[ark-treasury]: ../../x/treasury/README.md
[ark-reserve]: ../../x/reserve/README.md
[ark-claims]: ../../x/claims/README.md
[ark-security]: ../../x/security/README.md
[ark-mempool]: ../../app/mempool/README.md
[ark-threat]: ../design/THREAT_MODEL.md
[ark-settlement]: ../../x/asset/keeper/settlement.go
[ark-app]: ../../app/README.md
[ark-genesis]: ../governance/GENESIS.md
