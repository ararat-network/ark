# Economic decisions

The register of Ark's economic rules: each entry states a rule in force and the reason for it. [Economic
design](ECONOMIC_DESIGN.md) describes how the rules compose; [launch genesis](../governance/GENESIS.md) owns the launch
values and committee appointments. D/P identifiers are stable citation keys for code and documents, and an entry is
rewritten in place when its rule changes; a number with no entry has no rule.

## Find a decision

- D1–D40: [D1](#d1), [D2](#d2), [D3](#d3), [D4](#d4), [D5](#d5), [D6](#d6), [D7](#d7), [D8](#d8), [D9](#d9), [D10](#d10), [D11](#d11), [D12](#d12), [D13](#d13), [D14](#d14), [D15](#d15), [D16](#d16), [D17](#d17), [D18](#d18), [D19](#d19), [D20](#d20), [D21](#d21), [D22](#d22), [D23](#d23), [D24](#d24), [D25](#d25), [D26](#d26), [D27](#d27), [D28](#d28), [D30](#d30), [D31](#d31), [D32](#d32), [D33](#d33), [D34](#d34), [D35](#d35), [D36](#d36), [D37](#d37), [D38](#d38), [D40](#d40).
- D41–D83: [D41](#d41), [D42](#d42), [D43](#d43), [D44](#d44), [D49](#d49), [D53](#d53), [D54](#d54), [D55](#d55), [D56](#d56), [D57](#d57), [D58](#d58), [D59](#d59), [D61](#d61), [D62](#d62), [D63](#d63), [D64](#d64), [D65](#d65), [D66](#d66), [D67](#d67), [D69](#d69), [D70](#d70), [D71](#d71), [D72](#d72), [D73](#d73), [D74](#d74), [D75](#d75), [D76](#d76), [D77](#d77), [D78](#d78), [D79](#d79), [D80](#d80), [D81](#d81), [D82](#d82), [D83](#d83).
- D84–D111: [D84](#d84), [D85](#d85), [D87](#d87), [D88](#d88), [D89](#d89), [D91](#d91), [D92](#d92), [D93](#d93), [D95](#d95), [D96](#d96), [D101](#d101), [D102](#d102), [D103](#d103), [D104](#d104), [D105](#d105), [D106](#d106), [D107](#d107), [D108](#d108), [D109](#d109), [D110](#d110), [D111](#d111).
- D112–D140: [D112](#d112), [D113](#d113), [D114](#d114), [D115](#d115), [D116](#d116), [D117](#d117), [D118](#d118), [D119](#d119), [D120](#d120), [D121](#d121), [D122](#d122), [D123](#d123), [D124](#d124), [D125](#d125), [D126](#d126), [D127](#d127), [D128](#d128), [D129](#d129), [D130](#d130), [D131](#d131), [D132](#d132), [D133](#d133), [D134](#d134), [D135](#d135), [D136](#d136), [D137](#d137), [D138](#d138), [D139](#d139), [D140](#d140).
- Launch and analysis: [P1](#p1), [P2](#p2), [P3](#p3), [P4](#p4).

## D1

Ark has no `x/mint` and no scheduled staking-reward inflation. No NOAH is minted outside a conversion: validators and
the Oracle are paid from gas, the transfer tax, and a non-minting subsidy pool ([D9](#d9) to [D12](#d12)), so no routine
issuance dilutes holders.

## D2

Market is the sole minter, and it uses its mint and burn authority only for conversion settlement. Conversion is the
chain's one issuance path; every other flow moves NOAH that already exists.

## D3

Market keeps `BasePool`, `ArkPoolDelta`, and `PoolRecoveryPeriod` as virtual imbalance controls. Virtual-pool pricing
penalises sustained one-way flow and remembers imbalance, and time-based recovery limits how fast cheap capacity
returns.

## D4

Each block's redemption output is funded by the Redemption Buffer's coverage share of the claimable liability, capped at
100%, and the residual is minted: of the summed output `Q` the Buffer pays `min(Q, floor(Q × B / L))`, where `B` is the
Buffer after the block's expansion credits and `L` the claimable liability before the block's burns, net of Reserve-held
paper ([D67](#d67)) and never scaled by the exposure multiplier ([D73](#d73)). The claimable aggregate counts every
obligation the chain can value: fresh-priced supply, a stale member at its last known rate, and a suspended asset at its
plan rate from the block the plan opens. It excludes only supply the chain cannot value, a member never priced or
suspended supply without a plan, and discloses that gap through `EventLiabilityIncomplete` and `FundStatus`'s
`stale_member_supply` and `untrusted_suspended_supply`; no completeness flag is stored or reported, and the draw never
switches off. A zero-draw fallback on incomplete valuation would switch the Buffer off chain-wide during exactly the
stress it exists to dampen; excluding a stale member would pay its holders' share of the Buffer away to whoever redeems
during the outage; and a pending plan is the protocol's committed rate until governance cancels it.

## D5

Each block's gross expansion offer, principal with its spread and dust ([D6](#d6)), goes down one waterfall: the
Redemption Buffer's gap, then the strategic Reserve's, then Insurance's, and the overflow burns ([D33](#d33)). Burning
only what overflows the three targets is what lets expansion contract NOAH supply once the funds are full.

## D6

Spread and integer dust enter the expansion waterfall as principal does, fund targets first and overflow burned, and
route to neither the subsidy pool nor the fee collector. Redemption spread is NOAH never issued and stays unissued. The
fiscal design is two circuits. Gas and transfer tax fund security, so validator income never depends on the money-supply
cycle; conversions insure themselves, so spread, a premium against stale-oracle and adverse-selection risk, capitalises
the funds that bear that risk, whose targets already scale with the volatility and flow indicators that widen it. With
the funds at target spread burns exactly as an unconditional burn would; with a fund short it is capital arriving as the
requirement rises. The subsidy pool takes no protocol inflow, so its countdown stays honest, and nothing is paid from
the funds beyond target, so no constituency gains from a wider spread.

## D7

Fund targets are passive routing thresholds: they decide only where expansion NOAH goes and never trigger minting,
trading, or an automatic withdrawal. They are not custody caps, so a deposit above target is valid and a falling target
releases, burns, or moves nothing. A target that acted would be an automatic, price-reactive controller over supply or
custody; passive targets leave every fund movement either a conversion's own flow or an authorised act.

## D8

Every fund's target is sized on one shared basis, the stablecoin liability valued in NOAH ([D24](#d24)) and scaled by
the exposure multiplier ([D72](#d72)); there is no per-fund covered-risk model. The Buffer counts its NOAH balance,
Insurance its NOAH balance less pending NOAH claim reservations, and the Reserve its NOAH plus haircut-and-capped credit
for attested holdings ([D58](#d58)). With every fund's capital counted in NOAH, every fund's exposure has liability as
its only base, and Reserve deployment risk is priced by recognition haircuts and caps, so a separate per-fund model
would collapse to the existing ratios and multiplier; one earns a place only when a fund carries material risk that does not scale with
liability.

## D9

The subsidy pool, `treasury_subsidy_pool`, is a dedicated, balance-constrained, non-minting account seeded at genesis.
Its only outflows are the funding-window settlement draw on reward shortfalls and governance's return to the community
pool ([D83](#d83)), and depleting it never mints, borrows from a fund, or stops block production. A bootstrap with no
protocol inflow keeps its countdown honest: a deposit extends coverage, and nothing refills it on its own ([D25](#d25)).

## D10

Validator and Oracle reward funding have separate per-block NOAH-value targets in `EconomicPolicy`, but Treasury adds
the applicable targets once per observed block and aggregates them, with the eligible validator fees, over one Treasury
funding window, whose countdown initialises from `reward_funding_window` ([D34](#d34)), before calculating shortfalls. A
quiet block then spends no subsidy that a busy block in the same window would have covered.

## D11

There is no `oracle_tax_share`. Transfer tax accumulates in `transfer_tax_collector` for the same Treasury window; at
settlement the aggregate Oracle reward target is protected first, tax above that floor may close the aggregate validator
target gap, and every residual goes to the Oracle. One validator ratio applies to every tax denomination, rounding the
validator amount down, the Oracle takes the exact remainder of every coin, and no coin is converted. Validators also
earn gas through Distribution, while the tax is the Oracle's only organic income, so tax reaches validators only to
close a gap gas left.

## D12

When the subsidy pool cannot cover both reward shortfalls, it pays them in proportion: of the pool balance `B` the
Oracle receives `floor(B × oracle_shortfall / total_shortfall)` and validators the remainder. Flooring the Oracle's
share gives the rounding to validators and spends exactly the pool.

## D13

Distribution's `community_tax` is zero in the launch and testnet genesis artefacts, `app/genesis/genesis.json` and
`app/genesis/testnet.json`. No application code overrides the distribution module basic, so `arkd init` writes the SDK
default and a genesis built from it must set the zero. Nothing skims validator income into the community pool
([D14](#d14)).

## D14

Community-pool and protocol residual accounting stays an SDK concern inside Distribution, and `x/protocolpool` is not
wired. No Treasury flow routes revenue to the community pool; governance's `MsgReturnSubsidy` ([D83](#d83)) is the one
Treasury path into it. The community pool is governance's discretionary fund, not a protocol funding lane.

## D15

Insurance is the `claims_insurance` module account, owned by `x/claims` ([D54](#d54)), and pays only through unique,
recorded claim IDs under the bounded, governance-owned Claims mandate ([D30](#d30)). Treasury sizes its target and
credits it through the waterfall but cannot debit it. Custody, appointment, reservations, and settlement share one
owner, so no fiscal module operates a second mandate over Insurance.

## D16

Ordinary redemption never debits the strategic Reserve, and the Reserve never directly pays redeemers, rewards, or
claims. Its only debits are governance's commitments and burns and the Reserve committee's bounded mandate acts:
commitments, burns, and deployments to the mandate's exact destination list ([D27](#d27), [D56](#d56), [D57](#d57),
[D61](#d61)). No message withdraws Reserve custody to an arbitrary recipient. The Reserve is governed emergency
capacity, never an automatic Market source: Market cannot request, size, or trigger a Reserve debit, and redemption
pressure reaches the Reserve only through a commitment into the Buffer.

## D17

Each denomination's tax cap applies independently to each taxable input, and each `MsgMultiSend` output is its own input
with its own cap ([D138](#d138)). Two sends from one source, two outputs, and a signed input beside an
execution-generated one are never combined, so a batch pays what its separate sends would.

## D18

One calculator taxes every enabled user-facing stablecoin transfer surface, and internal Bank movements are never taxed.
A global Bank hook would tax settlement, draws, claims, grants, reward funding, and commitments unless it recreated an
exemption system.

## D19

Queries report the Redemption Buffer, strategic Reserve, and Insurance separately and never present a combined backing
ratio. Each fund answers one mandate, and the Buffer is conversion inventory rather than collateral, so a combined
figure would claim a backing no fund provides.

## D20

Ark launches from a clean genesis with no Terra Classic state or wire compatibility and no migration path. Every Classic
type, message, and store layout is modernised rather than carried, and a compatibility layer would hold Classic's shapes
for the life of the chain.

## D21

Governance owns Treasury's tax-cap amount, `Params.reference_tax_cap`, in base units of `Params.reference_denom`. That
unit is the Oracle's protocol reference, which launches as `axdr`: `InitGenesis` refuses a mismatch, `MsgUpdateParams`
refuses a denomination change, and only Oracle's `MsgSetReferenceDenom` moves it, rebasing the cap as a quantity in the
same transaction ([D76](#d76)). Treasury's cap and Market's pool share the unit but not the amount, so changing either
amount needs no update to the other, and a re-point needs no Treasury proposal. One owner of the unit keeps the two from
drifting into different units ([current contract](ECONOMIC_DESIGN.md#53-calculation-and-caps)).

## D22

Market's `BasePool` is a denomination-bearing `sdk.DecCoin` in `ConversionPolicy`, denominated in the Oracle's protocol
reference, which launches as `axdr`. Market's `MsgUpdatePolicy` and, inside the conversion corridor,
`MsgCommitteeUpdatePolicy` move its amount and refuse a denomination change; the denomination moves only through
Oracle's `MsgSetReferenceDenom`, which requires the successor's feed Active and freshly priced and runs Market's rebase
with every other wired `ReferenceRebaser`, Treasury's, Disbursement's, and Claims', in one transaction or fails whole. Treasury
never drives or intermediates the pool transition. The delta never exists without its unit being observable, and the
pool's unit decides no stablecoin's convertibility, so a re-denomination changes no eligibility ([current
contract](ECONOMIC_DESIGN.md#34-the-pool-unit)).

## D23

Every applied `BasePool` change, of amount or denomination, atomically rescales `ArkPoolDelta` to preserve `delta /
BasePool.Amount`. On a reference re-point Market derives the new amount from one fresh, deterministic Oracle conversion
of the stored pool, a quantity rebase under [D76](#d76); governance may state the outgoing rate when the old feed is
stale or has never priced, and `EventPoolUpdated` carries the old and applied pool state. No unit or depth change resets
imbalance or reprices without flow.

## D24

Treasury values liabilities and targets directly in NOAH equivalents; stable-to-stable pricing never uses Market's pool
denomination. The funds hold NOAH, so a NOAH-valued liability compares with them directly, and re-pointing the reference
moves no liability figure.

## D25

Anyone may send `anoah` to the subsidy pool, irreversibly and without refund; every other denomination is refused, and
no automatic refill exists. A deposit changes neither supply nor the reward targets, only how long coverage lasts.

## D26

The subsidy pool and the Redemption Buffer admit only positive `anoah` credits; a mixed or non-NOAH credit fails
atomically, whatever surface it came through, and no sender is exempt. The strategic Reserve and Insurance admit NOAH
and asset-registry members by membership alone ([D65](#d65), [D70](#d70)): the Reserve's rule admits the derecognised
transfer tax Treasury settlement routes there as inert custody that earns no credit and waits for a burn
([D61](#d61)), and Insurance's admits the paper a stablecoin claim buys ([D140](#d140)). Each NOAH-only fund counts its
balance at par, so a non-NOAH coin there would be custody its capital figure cannot see; Insurance counts its NOAH
alone ([D28](#d28)).

## D27

Governance may irreversibly transfer a discrete `anoah` amount from the strategic Reserve to the Redemption Buffer
(`MsgFundBuffer`) or to Insurance (`MsgFundInsurance`), refused if it would leave the Reserve below a minimum remaining
balance the proposal states; no target, price, Oracle, or Market trigger applies, and the receiving fund's gap does not
cap it. The Reserve committee's `MsgCommitteeFundBuffer` and `MsgCommitteeFundInsurance` are bounded by the receiving
fund's shortfall on the gross basis ([D67](#d67), [D72](#d72)) and by the mandate's NOAH floor ([D56](#d56)). A
commitment reclassifies NOAH that already exists and is one-way: there is no Buffer-to-Reserve path, since a public
removal path would make committed inventory reversible and invite redeeming ahead of it. The stated minimum is a
per-proposal stale-state guard, not a protocol floor, and is checked against the remaining balance so a harmless deposit
cannot fail the proposal.

## D28

Required capital is sized on the liability a fund covers, never on the gross value of what it holds: held assets satisfy
a requirement without defining it. A non-NOAH holding reduces a gap only through explicit, risk-adjusted recognition,
the Reserve's haircut and capped eligibility policy ([D58](#d58), [D64](#d64)); Insurance counts NOAH alone, and the
registry paper it holds for stablecoin claims is custody for those claims, never capital ([D140](#d140)).
Liquidity sits apart from recognition, in the Buffer as the liability-scaled liquid tranche and in the Reserve mandate's
NOAH floor ([D56](#d56)). Ark-issued stablecoins always receive zero credit and cannot be named by an eligibility entry
([D69](#d69)), because Ark paper is a liability until burned. Full credit for a holding would close a gap that is not
closed, and zero credit would reopen the whole spend and invite an acquisition-and-refill loop that captures expansion
principal; a haircut reopens exactly the unrecognised part.

## D30

Governance owns the Insurance Claims mandate. The shared cancellation period, `claim_cancellation_period` in seconds of
block time ([D133](#d133)), is an `x/claims` parameter applying to both origins. Committee submissions are bound to the
active mandate window and the exact term and consume a fixed gross term allowance in the reference unit ([D140](#d140));
governance submissions depend only on params, record no mandate term, and charge nothing. Governance may cancel any pending claim regardless of the current mandate, while the
current committee may cancel only non-governance claims. A period held in the mandate would make governance claims
depend on an enabled committee, and the committee may not overturn a governance decision.

## D31

Each delegated role, the economic-policy, Claims, and Reserve committees among them, uses its own role address and typed
authority domain even where human memberships overlap; no role receives Treasury's general authority or general Bank
authority. Separate addresses and messages keep every committee power enumerable from the proto service, so one role's
key never reaches another's powers.

## D32

Claims are submit-then-pay with encumbered pending amounts: submission reserves the amount, moving coins only to buy what
a stablecoin claim's custody lacks ([D140](#d140)), and the `x/claims` EndBlocker pays the immutable recipient at the
closing time with no signer. Cancellation ends for every actor
at the closing time, block time in Unix seconds ([D133](#d133)), and a cancelled claim's allowance stays consumed. No
authority can claw back a paid claim or bypass the held-balance, denomination, uniqueness, no-mint, no-borrow, or
no-cross-fund invariants. Payment needs no signer because nothing about it is discretionary, and the recipient of a
covered loss is the party least able to send a transaction; an allowance freed by cancellation would let
approve-and-cancel cycles redirect expansion funding.

## D33

Conversion settles in two moments. In the transaction Market owns custody, mint, and burn and makes no Treasury call: an
expansion escrows its gross NOAH offer whole and mints and pays the stablecoin output, a redemption burns its offer and
mints its complete quoted NOAH output, and each records its facts (`GrossOffer`, `EligiblePrincipal`,
`RedemptionOutput`, `RedeemedValue`) in transient accumulators. A conversion whose minted value exceeds its offer, or
whose output exceeds the liability it redeems, fails its own transaction. Market's EndBlocker hands the block's
`ConversionTotals` to Treasury's `SettleConversions`, which values the liability partition once against canonical Bank,
registry, and Oracle state, runs the waterfall over the gross total ([D5](#d5), [D6](#d6)) and the coverage draw over
the summed output ([D4](#d4)), moves each credit out of Market's account and the draw into it, and returns the NOAH
Market burns: the overflow plus the draw. Treasury owns valuation and allocation, and that return value is how Market
finishes settlement. An incomplete valuation parks the whole gross total in the Reserve, since an incomplete aggregate
can land either side of the truth and parking is revisable where a burn is not. A block that converted nothing values
nothing, liability is never cached, and a settlement error fails the block. Governance's EndBlocker runs before
Market's, so a proposal's conversions enter the block's totals and settle with it, against state that includes the
proposal's effects. Why: valued once after every write in the block, liability needs no snapshot that each writer of
member supply must keep coherent, so the silent mis-allocation a stale snapshot causes is closed by construction; every
expansion allocates under one valuation and every redemption draws at one coverage ratio, leaving no within-block
ordering to compete for; and settlement's variable recognition cost stays out of conversion gas. Running governance
after Market would strand a proposal's conversion offer in escrow, and carrying facts into the next block would break
block-level settlement. What it costs: a settlement error is a halt-class failure rather than a failed transaction, so
write-time domain bounds are the liveness defence, and a proposal reads fund balances as they stand before its block's
settlement.

## D34

Governance sets `reward_funding_window`, defaulting to one week of blocks (`chain.BlocksPerWeek`) and capped at 2^32
observations so accrual cannot overflow. When an empty Treasury window records its first observation, `blocks_remaining`
initialises from it; a parameter change never rewrites a positive countdown and takes effect at the next empty window,
so a window in flight settles over the length it began with. The window counts blocks, as every per-block process does.

## D35

`FundStatus` reports fund stocks, liabilities, disclosures, and targets, with the multiplier that scales them; active
reward-funding accounting has its own direct-state query, `Query/RewardFunding`, which stays available when fund
valuation is not. Funding accounting reads stored state and needs no current Oracle price, so an outage that blinds
valuation never hides it.

## D36

Governance may appoint one economic-policy committee account, its key shape recorded at appointment, under a bounded
mandate: a chain-derived term, a half-open activation and expiry window in block time ([D133](#d133)), and a minimum and
maximum policy as its corridor. The committee controls only the eight reversible `EconomicPolicy` fields, the two block
reward targets, the three fund target ratios, and the three exposure weights, and only within that corridor; governance
may set any valid policy and replace or disable the mandate at any time. The committee acts faster than a voting period
allows and its appointment expires on its own, while governance replacing or disabling it at proposal latency is the
brake.

## D37

Governance owns Treasury `Params`: `reference_tax_cap` in the Oracle-owned reference unit ([D21](#d21)),
`reward_funding_window`, `transfer_tax_rate` ([D80](#d80)), the exposure instrument, and the base-fee controller. The
reversible economic levers persist once in `EconomicPolicy` ([D36](#d36)). The boundary between the two messages is
stance against machinery, not a field count: a lever stating how much of something the protocol wants, reversible and
safe to hold between a mandate's minimum and maximum, belongs to the committee, while the instrument that computes it,
the cadence it runs on, and the rails bounding how far and how fast it may move stay with governance ([D72](#d72)).
`transfer_tax_rate` passes the stance test but lives in the fee every wallet signs, so it is governance's. The Claims
mandate is claims-only, and its cancellation period is an `x/claims` parameter ([D54](#d54)).

## D38

Claims stays minimal. The mandate stores the shared envelope, a monotonic term, the exact committee, and a half-open
activation and expiry window in block time ([D133](#d133)), plus a fixed gross committee claim limit,
`committee_claim_limit`, in the reference unit and charged with what each claim draws ([D140](#d140)); an appointment's
active span must be at least the cancellation period, which lives in
`x/claims` `Params` ([D30](#d30)). Claims carry no category or per-claim cap, and there is no guardian or
governance-cancellation flag: the term allowance bounds what a committee can commit, governance can veto any claim
before it closes, and which authority cancelled reads off the typed `ClaimAuthority` domain rather than a flag callers
must set consistently.

## D40

Every transfer adapter implements one execution-time tax contract, fixed before the adapter is enabled: the dispatching
account pays execution-generated tax in addition to the complete principal, assessment is exactly once by call path, the
tax rolls back with its transfer, and neither the outer fee payer nor feegrant sponsors it ([D41](#d41), [D42](#d42)).
With no IBC ([D118](#d118)), the Wasm runtime is the one adapter beyond the ante: Treasury's execution hook is the
policy router wrapping the message router Wasmd dispatches through, and the runtime ships open from height one because
those seams are in place ([genesis](../governance/GENESIS.md#11-cosmwasm)). A contract's transfers are absent from the
signed message list, so an adapter enabled before its tax semantics were fixed would move value untaxed or tax it twice.

## D41

Exactly-once tax assessment rests on call-path ownership. The ante owns signed top-level inputs, Wasm attached funds
included; the execution policy router owns only the taxable inputs of messages a contract dispatches: Bank sends,
execute funds, and instantiate funds. There are no persistent transfer IDs, context markers, global Bank tax hook, or
implicit execution-time feegrant: structure puts each input before exactly one seam, and a global Bank hook would tax
settlement, draws, claims, grants, reward funding, and commitments unless it recreated an exemption system. The
dispatching contract pays the tax in addition to the complete requested principal.

## D42

Execution-generated tax goes directly to `transfer_tax_collector`, collected with its matching transfer in the same
cached execution: the router charges the dispatching account before it runs the dispatch, so a synchronous or caught
failure rolls both back and a recipient's amount is never reduced to fund tax. Escrow, Market settlement, fund
movements, claims, and reward distribution are never new taxable inputs.

## D43

Contracts get a read-only tax estimate through Treasury's own `Query/ComputeTax`, admitted to the Wasm query accept
list rather than served by a custom querier ([D74](#d74)). A contract prices the proto form of the message it will
dispatch, so the calculator and its rate and cap math are never duplicated; for the JSON-native `CosmosMsg` variants
the contract builds that proto itself, and keeping it identical to what it returns is the author's responsibility. The
result is an advisory current-state estimate only: it reserves no funds, grants no authority, and never stands in for
execution-time recomputation.

## D44

`Params.transfer_tax_rate` is the sole tax activation switch: at zero nothing is taxed and no configuration is lost.
Caps are never stored; each is derived on read as the reference cap times the denomination's conversion factor, so no
stored cap can drift from the factors, and a factor that persists through a feed outage defers a cap rather than
failing a block. A zero reference cap means uncapped taxation. A denomination with no factor, a member before its
first refresh among them, is untaxed rather than an error, so missing pricing never blocks a transfer; NOAH is never
taxed, since `GetTaxCap` excludes the numeraire. A positive cap whose product truncates to zero is clamped to one base
unit, so a finite ceiling never becomes the uncapped sentinel, while a product too large to represent reads as
uncapped, its true ceiling lying past the supported range.

## D49

Ark runs upstream Wasmd `v0.70.x` on `wasmvm/v3` against SDK `v0.54.4`, with no Wasmd fork and no replace-directive
compatibility layer. Why: a fork or a replace directive has to be re-applied to every upstream security release and
runs code Wasmd's maintainers neither test nor audit, so an SDK move waits for an upstream Wasmd that compiles against
it ([future changes](../direction/FUTURE_CHANGES.md#1-upgrade-to-cosmos-sdk-v055)).

## D53

Ark has no NFT module. Why: a non-fungible token is a contract protocol, which CW721 contracts on the open Wasm
runtime serve without new chain state, messages, or tax-calculator surface, and a native module would put a second
schema beside them.

## D54

`x/claims` owns Insurance claims: the Claims mandate, the claim record, the Insurance reservation, and the
`claims_insurance` custody account. Treasury keeps liability valuation, all three fund target ratios, the expansion
waterfall, tax, and reward funding, and reads Insurance through the one-way `ClaimsKeeper.RecognisedCapital`
interface. The boundary is the capital accounting contract of
[economic design §4.3](ECONOMIC_DESIGN.md#43-recognition): Treasury owns `required_capital`, the operating module owns
`recognised_capital`, and held assets satisfy a requirement without defining it. `x/reserve` follows the same pattern
([D55](#d55)), so Treasury holds one mandate, the one-mandate-per-module convention `x/security`, `x/asset`, and
`x/market` also follow.

## D55

`x/reserve` owns the strategic Reserve on the [D54](#d54) pattern: the `strategic_reserve` custody account,
governance's `MsgFundBuffer` and `MsgFundInsurance`, the Reserve mandate, and the send restriction over that account,
which admits NOAH or a registry member from every sender, with no sender exemption ([D65](#d65)). It answers the same
`RecognisedCapital` contract as `x/claims`, through Treasury's own `ReserveKeeper` interface, since depinject resolves
module inputs by type and needs the two named apart, so Treasury sizes both committee-operated funds identically and
holds one mandate. Owning the restriction beside the account is what lets the Reserve's admission rule differ from the
other funds' without touching them.

## D56

One committee holds the Reserve mandate, appointed through `MsgSetReserveMandate` on the shared envelope pattern:
chain-derived monotone term, half-open activation window in block time ([D133](#d133)), an empty committee disables,
and replacement resets allowance usage. The mandate carries the term's gross deployment allowance (consumed
permanently by NOAH outflows, which closing a position does not undo), the minimum `anoah` balance no committee
disposal may breach (absolute and appointment-scoped; the liability-scaled liquid tranche is the Redemption Buffer),
and an exact destination list. There is no guardian and no pause state — governance replacing or disabling the mandate
at proposal latency is the brake, as for every other mandate on this chain — and no governance deployment message,
because deployment needs an off-chain counterparty relationship only a committee operationally has. Per-transaction,
rolling-window, and lifetime caps, price, slippage, and deadline constraints, and adapter pins are each deliberately
omitted: the bounded on-chain outflow and the off-chain custody relationship carry different evidence.

## D57

The Reserve keeps a quantity ledger priced at read time: it records what is held, never what it is worth. An
append-only journal of seven kinds — DEPLOYMENT, QUANTITY_UPDATE, RETURN_ATTRIBUTION, RETURN_REVERSAL, IMPAIRMENT,
CORRECTION, CLOSURE — carries proven coin movements and committee-attested quantities with bounded off-chain
references, and valuation happens only at read time against the Oracle's available rates. Corrections restate, so the
journal keeps the error and the fix. A return reversal (`MsgReverseReturn`, `MsgCommitteeReverseReturn`) undoes one
attribution and carries its movement verbatim, never re-priced, so reversing and re-attributing cannot harvest a rate
move. Genesis reconciles every position's deployed and returned figures and the term's allowance usage against the
journal and refuses a mismatch. Return value is crystallised at attribution by the keeper, so realised profit or loss
at closure is arithmetic over proven legs, which is why closure needs no governance gate. See the
[Reserve README](../../x/reserve/README.md#mandate-custody-and-journal).

## D58

Recognition policy is one governed list of `EligibilityEntry` rows — an external symbol ([D69](#d69)), a haircut factor
in (0, 1], a maximum credit per NOAH in (0, 1] ([D64](#d64)), and a staleness window ([D71](#d71)) — which
`MsgSetRecognitionPolicy` sets whole. `RecognisedCapital` counts the Reserve's `anoah` balance at par and credits each
listed asset `min(haircut × attested open unimpaired quantity × rate, max_credit_per_noah × anoah_balance)`
([D64](#d64)). The multiplication is the chain's one rate orientation, NOAH per unit ([D75](#d75)), and the conversion
goes through `RateSet.Convert` like every other valuation on the chain rather than being re-derived here. Every rate
comes from the Oracle, so a dark or stale feed zeroes exactly one asset's credit and recognition degrades to zero rather
than to a frozen number, without exception: there is no governance-supplied fallback price, and a slow-cadence feed is
served by the entry's own window. Both factors are strictly positive, because an entry granting no credit would be
indistinguishable from no entry while still occupying the policy and raising no feed-guard claim; refusing zero makes
every stored entry a live claim and the guard ([D63](#d63)) total over the policy. The list is credit policy alone:
custody admission never reads it ([D65](#d65)). Cross-fund concentration caps are per-asset Reserve caps because
Insurance recognises only NOAH. The `RecognisedCapital` query decomposes every row, attested-but-unlisted holdings
included, so each zero credit shows its reason. Bank custody other than NOAH is not a subject of the decomposition and
the fold reads no other balance: a listed name is an external symbol the account can never hold ([D70](#d70)), and the
only other custody, registry-member paper, can never be recognised and is already published by Bank and by Treasury as
self-held supply ([D66](#d66)).

## D59

The Reserve has no adapter or evidence-upgrade machinery, and none is planned
([Reserve evidence model](../../x/reserve/README.md#mandate-custody-and-journal)). Evidence upgrades can verify only
value that routes through chain-visible machinery — a contract read proves Wasm state — and the Reserve's plausible
asset universe is custodian-held off-chain instruments reached by wire transfer, which none of that can ever see.
Manual committee attestation with bounded references is therefore the permanent evidence model, not an interim one,
and the Reserve account itself never holds an external token ([D70](#d70)). The journal's append-only design keeps
this reversible without migration: machine-authored entries could land beside committee-authored ones if a
chain-verifiable venue ever exists, but that requires its own spec and the
[first-asset governance gate](../governance/GOVERNANCE_OPERATIONS.md#3-ark-governance-operations).

## D61

Reserve burn authority is split by what a burn can destroy. Governance (`MsgBurnReserveAssets`) may burn any Reserve
custody including NOAH, the NOAH component under a per-proposal minimum remaining balance, the stale-state guard the
Buffer commitment carries: a NOAH burn of chosen size is the protocol's only discretionary supply contraction, and no
committee chooses the size of a supply move. The committee may burn registry-member paper that carries no recognition
credit (`MsgCommitteeBurnPaper`), because destroying what the capital system counts at nothing cannot reduce
recognised capital, and destroying Ark paper reduces consolidated liability; NOAH and non-member custody are refused,
and the credit check is a provably unreachable backstop under [D69](#d69). The committee may also burn NOAH surplus
(`MsgCommitteeBurnSurplus`), bounded by the keeper at the largest amount after which recognition at the remaining
balance still covers `RequiredReserveCapital`, and never more than `balance - minimum_noah_balance`; the handler re-checks the
amount at the balance it leaves (`CheckSurplusBurn`). The bound is read at the post-burn balance because clipped
credits lever the balance: a clipped stake's credit is its ratio of the balance, so a unit burned lowers recognised
capital by `1 + Σ clipped ratios` and the pre-burn surplus would overstate the power. Sized this way the power
exhausts itself at the target line and cannot reach through it, leaving the committee timing rather than size. That
burn refuses while valuation is incomplete, since the requirement is then unavailable — the same condition that parks
the block's gross expansion in the Reserve freezes disposal out of it. Burns open no position and append no journal
entry; the signed message and Bank's canonical burn event are the audit trail. `strategic_reserve` holds `Burner` and
never `Minter`. Parking is a Bank credit Treasury makes on the incomplete-valuation branch, disclosed by
`EventLiabilityIncomplete`; the Reserve keeps no record of it, since the interface between the two exchanges numbers
rather than transfer capability.

## D62

Ark-issued denominations are enforced credit-ineligible, not merely declared so ([D28](#d28)): no Reserve eligibility
entry can name an asset-registry member, whatever its lifecycle status, since a written-off or retired asset is still
Ark-issued. The enforcement is the namespace of [D69](#d69), so the rule needs no registry read at the policy write,
at genesis, or in the per-block recognition fold, and a denomination listed before it is ever registered cannot exist.
Member paper may sit in Reserve custody, admitted by membership ([D65](#d65)), and earns nothing there.

## D63

`x/reserve` registers as an Oracle feed-removal guard beside `x/asset` in the wiring-owned guard set. It reports two
kinds of claim, both derived from Reserve state at call time. Every eligibility entry claims its derived series,
because recognised capital counts what the fund holds valued through that feed, so removing the feed would shrink
Reserve capital and change what a committee may burn with nothing in the proposal disclosing it; that claim exists to
force sequencing — delist, then remove. Every open position claims the series pricing what it holds, the prefix's
series for an external symbol and the denomination's own for Ark paper, listed or not: with the series gone an
external holding can never be listed for recognition, which requires an Active feed ([D70](#d70)), and held paper
cannot leave through a deployment, which must be booked at a fresh valuation, leaving a committee burn its only exit.
A closed position raises no claim: its recovery is crystallised, so no later rate read can change what the journal
says about it. Every stored entry claims, whatever it currently credits: under the strict positivity of [D58](#d58) a
credit-granting test would be vacuous, and the two readings differ in direction — an entry crediting nothing, could
one exist, would have its feed removed silently, where walking the whole policy blocks removal until governance
delists it. The guard gates an act recoverable only at the cost of the activation delay and a re-warm, so it fails
closed.

## D64

Recognition caps are each asset's share of the Reserve's NOAH balance: `EligibilityEntry.max_credit_per_noah`, in (0, 1]
with the policy's values summing to at most one, clips credit at `max_credit_per_noah × anoah_balance`. The denominator
beats three alternatives. A flat `anoah` amount states a tolerance at one balance-sheet size and decays into a binding
constraint or a vacuous one — and a binding cap reopens the gap at full value, the acquisition-and-refill loop. A ratio
of the capital requirement scales the ceiling with the size of the need rather than the fund's substance, so one
corrupted attestation against an almost-empty fund could manufacture credit up to a share of a large target. A ratio of
recognised capital itself measures each cap against a total that includes the credits being capped, so one asset's
credit raises every other asset's ceiling: beside honest holdings held back by their caps, a single misreported entry
carries the total to `base ÷ (1 − Σ ratios)`. With 100 NOAH and gold capped at a half that design recognises 200, and
one fictitious entry capped at 0.4 takes it to 1,000. Against the NOAH balance, which Bank proves, every credit is
independent: a misreported holding or a wrong price adds at most its own `ratio × balance` and moves no other asset's
credit, a dark feed zeroes its own asset and nothing else, a fund holding no NOAH counts no asset at all, and with the
ratios summing to at most one recognised capital never exceeds twice the NOAH held, whatever is attested. This is the
structure of the Basel III liquidity coverage ratio, which states its Level 2 limits as shares of the total but computes
them as multiples of Level 1, so no lower-tier holding can lift another's allowance. A ratio therefore does not read as
a share of the reported total: governance sizes it as a multiple of the NOAH held, and sizes entries sharing a price
series by their combined ratio ([D70](#d70)). The arithmetic is one checked multiplication per asset with no solve;
`RecognisedCapital(ctx)` is a pure local read, nothing is threaded from Treasury, and the surplus a committee may burn
is monotone in the requirement ([D61](#d61)). The query publishes each asset's resolved `cap`, the absolute ceilings a
debt-ceiling design would have governance push by hand.

## D65

The Reserve's send restriction admits positive NOAH or any asset-registry member, by membership alone, and refuses
everything else, whoever sends it; no sender is exempt. Ark paper is protocol liability wherever it sits, can never
earn recognition credit ([D28](#d28)), and the Reserve is the one account with a committee able to retire it, so
refusing it would protect none of the fund's figures and only push the paper somewhere the chain cannot see.
Membership covers written-off and retired assets, so the transfer tax Treasury settlement derecognises enters under
the ordinary rule. The recognition policy admits nothing to custody: no mint path produces an external symbol, genesis
refuses one, and movement valuation cannot price it, so admitting them would open the on-chain external-custody lane
[D70](#d70) forecloses. In-kind returns arrive as NOAH or members. Accepted cost: anyone may push member dust into the
account, bounded by registry size and burnable by the committee.

## D66

Treasury's liability partition measures and discloses self-held supply: for every member the aggregate counts, the
strategic Reserve's balance of it, valued through the same branch that counted it (fresh, settlement, or last known).
Accrual happens inside each counted branch rather than over the registry, so the two sides of the subtraction can
never price on different bases and `net = gross - self_held` is non-negative by construction. `FundStatus` reports it
as `self_held_supply`, `self_held_liability`, and `net_liability`.

## D67

The claimable aggregate resolves to two bases, and one principle assigns every consumer: a flow serves claims that can
arrive, and no claim can arrive from self-held paper; a bound serves discretion, and the discretion-holder can
re-issue that paper, so bounds price it as if they already had. Flows take net — the expansion waterfall's gaps and
`drawRedemptionCoverage`. Bounds take gross — `grossFundTargets` and everything derived from it:
`RequiredReserveCapital` (hence `BurnableSurplus`), `InsuranceShortfall`, `RedemptionBufferShortfall`. The
`FundStatus` query reports both target families. Net extends to Reserve-held paper the exclusion the partition applies
to written-off and planless suspended supply.

## D69

External holdings are named by external symbols — `<feed>-<tag>`, the prefix passing the priced-denom rule and the tag
bounded lowercase alphanumeric — partitioning the namespace from Ark-issued assets by shape rather than by state. The
registry's charset admits no dash and eligibility requires the external shape at the types level, so
policy ∩ registry = ∅ is a theorem about strings with no read, no ordering, and no cross-module cooperation to keep
true. A registry check at the policy write would be a temporal guard on a property the strings already guarantee; the
paper-burn credit refusal is a provably unreachable backstop ([D61](#d61)). The journal is permanently unambiguous
about which instrument its history names — a property no write-time guard provides, because closed history raises no
claim. An external symbol can never be protocol paper, being unregistrable by shape while conversion mints registry
members alone, and an off-chain NOAH holding is unnameable, since the prefix rule excludes the numeraire. See the
[external-symbol rationale](../../x/reserve/README.md#external-symbols-and-freshness).

## D70

An external symbol's pricing feed derives from its prefix, unconditionally: `chain.ExternalFeed(symbol)` is a pure
function, never derive-if-exists, which would silently re-point every external symbol on a series the moment a twin feed
activates. The derivation is many-to-one — several tags share one series with per-entry haircut, cap, and window, so
multi-custodian holdings need no aliasing machinery and no duplicate feed, and a holding honestly priced on a different
series is a different prefix. Listing requires the derived feed Active — exactly, not merely scheduled: an entry's
haircut and window are judgments about how a series behaves, voted blind if it has never printed a rate, and a series
scheduled for removal has already passed its guard. This matches registration's rule, and Reserve genesis imports after
`x/oracle` to check it. The feed guard maps entries to their series by a filtered pass over the policy and open
positions through their resolved feed ([D63](#d63)). A deployment's acquired leg and any denomination-changing
correction must name an external symbol or a registry member, closing the bare-issuable-name side door. The send
restriction admits NOAH or members alone ([D65](#d65)), and the refusal is permanent: an external-shaped name is a class
no mint path produces, genesis refuses, and movement valuation cannot price, and admitting it would open an on-chain
external-custody lane [D59](#d59) forecloses, so on-chain custody of an external token will never be built. Governance
sizes the combined maximum credit per NOAH of the entries sharing a series rather than each row.

## D71

Staleness tolerance is entry policy, not feed state: `EligibilityEntry.max_rate_age` — seconds, required positive, at
most `MaxRecognitionRateAge` (30 days) — states how old the derived feed's rate may be and still back this entry's
recognition credit, never inherited from the Oracle default, which answers for conversion-grade reads. Cadence is a
feed fact and tolerance consumer policy, and one series can serve conversion and Reserve recognition with different
tolerances, so the window never enters Oracle state and the Oracle carries no per-denomination age override. The
Oracle judges per request: `GetRateSetWithin` takes one request per entry — the entry's symbol and window — derives
the series from the name itself, and answers under the name, so two tags on one series may state different windows and
receive different verdicts, keyed apart rather than colliding on the shared feed, and no request can route a name to
any series but its own. Nothing unjudged crosses the module boundary. `Params.max_exchange_rate_age` is the single
default behind every gate-enforcing read, including `valueMovement`'s conversion-grade pricing of proven movements;
`GetLastKnownRateSet` ignores freshness and serves accounting for outstanding supply only, never quotes, minting, or
payments.

## D72

Fund targets are risk-scaled. Every target is sized on `m ×` its liability basis — net for the expansion waterfall,
gross for the bounds on committee acts ([D67](#d67)) — where `m` is a multiplier over three indicators the chain
maintains: the liability ratio (net liability over circulating NOAH, the reflexivity term, which collapses to that
quotient because liability is valued in NOAH under [D24](#d24)), annualised realised volatility of the protocol
reference rate, and an EMA of each block's redemption value net of expansion, floored at zero after each fold so two-way
churn cancels and an earlier inflow cannot hide a later run. Samples fold in `SettleConversions`, which Market's
EndBlocker calls every block with the flow facts already NOAH-valued in `ConversionTotals`; volatility samples at most
once per `volatility_sample_seconds`, so the oracle median's per-block jitter is never a sample of its own, and divides
an EWMA of squared returns by an EWMA of the seconds each return spanned, annualised over a year of seconds
([D133](#d133)). The multiplier recomputes every `exposure_refresh_period_blocks`, step-limited by `multiplier_max_step`
and clamped to `[1, multiplier_cap]`. The three indicator weights live in `EconomicPolicy` and the guardrails behind
them in `Params`. Putting all eight fields in `Params` to keep the committee's mandate narrow would mistake the field
count of [D36](#d36) for its principle; the criterion that sorts the two messages is stance against machinery
([D37](#d37)), and a weight stating how much extra capital a unit of leverage should demand is the same kind of lever as
the target ratios it modifies — reversible within hours by zeroing it, and the most time-sensitive knob the module has,
which is the committee's founding use case. What governance keeps is what makes the delegation safe: the decays and the
volatility sample spacing define the measuring
instrument and are not reversible in any useful sense, because a series folded under one memory is not recoverable by
restoring the old value; the cap is the ceiling on how far the response may go; the step is the anti-gaming rate
limiter, which a committee able to set it could raise to reach the cap in a single update; and the cadence is machinery.
A committee at the maximum of its mandate can therefore move the multiplier no faster and no further than organic stress
already can, so the delegation adds no blast radius. Weights sit inside the mandate's minimum and maximum like every
lever, letting governance floor them above zero to deny the committee the power to switch the model off, or leave the
floor at zero to grant it, per appointment. All three funds scale on one basis: the ratios are voted as a set and read
as relative fund sizing, so scaling a subset would move those proportions without a vote, and the indicator applies to
every fund sized against the same liability. A per-fund covered-risk basis ([D28](#d28)) stays deferred behind the
single shared basis of [D8](#d8). Every weight defaults to zero, which pins `m` at one and leaves the targets unscaled.
The liability aggregate plays three roles and only one is scaled: requirement bases read `m × L`, the redemption draw's
payment denominator reads raw `L` ([D73](#d73)), and the multiplier's own liability-ratio input reads raw `L`, because a
controller that fed its own output back into its input would compound.

## D73

The redemption draw is never scaled by `m`, in either direction. Dividing its basis rations the run's opening phase —
the phase that decides whether a spiral ignites — to guard against an exhaustion that cannot happen: proportional
coverage gives `B = B0 x (L/L0)`, so the Buffer depletes exactly in step with the liability it covers and reaches zero
only when the last stablecoin is redeemed. Multiplying coverage fails the other way: it double-counts one signal in
the payment path, since `m` has already raised coverage by raising the Buffer through the targets, and it front-loads
finite inventory on a bet about run depth (`B = B0 x (L/L0)^m`), leaving a thinner Buffer after any partial episode
and pinning coverage at one until depletion releases it mid-run. Both directions reintroduce the price-reactive Buffer
share [D4](#d4) exists to prevent. The draw's risk response is mediated by capital instead: the scaled targets grow
the Buffer before a run, and the `m`-widened Redemption Buffer shortfall widens the committee's Reserve-to-Buffer
commitment cap during one.

## D74

Contracts reach Ark's own modules through proto, not a hand-written JSON surface. There is no custom message encoder:
a contract calls Market with `CosmosMsg::Any` carrying `/ark.market.v1.MsgSell` and proto bytes. There is no custom
querier either: the estimate of [D43](#d43) is Treasury's `Query/ComputeTax`, reached through the accept list, so
contracts hold one proto surface for reads and one for writes. The argument is one schema instead of two: a custom
encoder mirrors each message as JSON on the Go side and again by hand in every contract, and nothing detects the drift
— the same objection D43 raises against a second message model and the repository guidelines raise against a second
validation site. Three things that look like costs are not. A parser's `Trader = contractAddr` override is not
protection: Wasmd rejects any dispatched message whose signers are not exactly the contract, so impersonation is
closed either way. Tax does not differ: a custom encoder's messages would route through the same execution policy
router. And no installed base breaks, because genesis is fresh ([D20](#d20)) and a Terra contract cannot port
unmodified regardless — `anoah` renames the denomination, 18 decimals move the money math, and the type URLs are
Ark's; a JSON envelope would preserve its own shape, not compatibility. Stargate/gRPC **queries** require an explicit
accept list — Wasmd ships no permissive default — and every entry must carry the `cosmos.query.v1.module_query_safe`
annotation, which keeps contracts to deterministic reads. The SDK's annotated auth, bank, and staking queries are
admitted whole, since the annotation is upstream's review ([D85](#d85)); Ark's own modules are listed by hand, 29
paths in `app/wasm_query.go`, because listing a path freezes its response shape. Messages need no such list; the
signer check is the gate. A Rust bindings crate is deliberately **not** committed to: authors can generate from Ark's
published protos, and publishing later is additive. That asymmetry decides the whole entry — adding a custom encoder
later breaks nothing, removing one later breaks every contract using it, so the reversible direction is the one to
start in.

## D75

Oracle rates are NOAH per one unit of the feed's denomination (`USD/NOAH`, not `NOAH/USD`); `RateSet.Convert` is
`amount × rate[offer] / rate[ask]`, so valuing in NOAH multiplies. Settlement plan rates and the governance-supplied
outgoing reference rate carry the same orientation. Every stored rate — settlement plan rates included, since a plan
sits beside oracle rates in the registry's rate sets — is bounded at `MaxExchangeRate`, the magnitude a direct report
is held to by `MaxEncodedVoteRateBytes`, and the tally omits a derived price above it, so the halt-class folds that
multiply a 2^128-capped quantity by a rate stay inside the LegacyDec domain by construction. See
[x/oracle/README.md §1.4](../../x/oracle/README.md#14-rate-orientation-noah-per-unit).

## D76

Prices and quantities rebase differently. `RateSet.Convert` re-expresses a quantity (`q × rate[old] / rate[new]`); a
stored price of the reference unit — the exposure anchor — moves by the reciprocal factor
(`p × rate[new] / rate[old]`), which is the same `Convert` call with the two units passed in the opposite order, so
the chain keeps one arithmetic path and reversed arguments at a price site are the operation rather than a bug. Every
rebase site states which it holds: the Market base pool, the Treasury tax cap, base-fee floor, and base gas price, and
Disbursement's compensation figures and Claims' limit and charges ([D123](#d123)) are quantities; the exposure anchor and the conversion-factor
table, each a figure per one reference unit, are prices, the table rebased through the one-unit cross.

## D77

The sidecar resolves in the chain's orientation end to end: a feed's output pair is `UNIT/NOAH`, routes end at NOAH
(`KRW/USD × USD/NOAH`), bootstrap prices are stated in leg orientation, and `PricesByFeed` re-keys a resolved price
without inverting. Provider markets stay in the orientation their venue quotes; the resolver's per-sample
normalisation (`providerSamples`) is the one place a venue quote meets a leg, so nothing after the provider cache
inverts, and route averaging is an arithmetic mean of the published figure, which a reciprocal taken at the feed
boundary after averaging would not give.

## D78

The signed fee declares the transfer tax. A transaction's fee is gas plus the exact tax its messages owe; the ante
refuses a fee short of the tax before deducting anything, and the charge is the figure the declaration is held to, so
a signer sees the whole charge in the fee they sign and is never taxed past it. The fee field carries the declaration
because it is the one slot every wallet already builds and every sign mode renders, amino included; an extension
option would not survive amino signing, and the ante rejects them. A wallet that omits the tax is refused with an
error naming it, which forces every client to price the tax before signing — the deliberate cost. The fee is a ceiling
and the tip is NOAH ([D80](#d80)): Ark's own `FeeDecorator`, in place of the SDK's, deducts the gas fee and tip alone,
so simulation deducts what execution deducts, and the tax goes directly to `transfer_tax_collector` after the messages
([D82](#d82)) with no routing step.

## D79

The committee's policy message is `EconomicPolicy`, and the tax is the transfer tax. The committee's eight levers are
two fiscal — the two block reward targets, which set what the protocol pays out — and six capital: the three fund
target ratios and the three exposure weights of [D72](#d72), which set how much capital it holds against its
liabilities and how fast it accumulates. The transfer tax rate is fiscal too, but governance holds it ([D80](#d80)),
so the committee holds no lever over what the protocol takes in. None is strictly monetary. The committee cannot mint,
quote, set spread, or move the pool, so it holds no rate or supply instrument; spread and pool depth are Market's, and
Reserve commitments to the Buffer belong to governance and the Reserve committee. `Economic` is the accurate umbrella
over both halves, where `Fiscal` would misname the capital half and `Monetary` misnames the whole. The tax's name
follows the same argument. Terra's stability tax funded validators through an adaptive controller holding unit mining
rewards stable against the seigniorage cycle; Ark has no such controller, no `x/mint`, and no seigniorage ([D1](#d1)),
and [D11](#d11) gives the proceeds to the Oracle target first. What the tax does here is fund security and the price
feed out of stablecoin usage, and compensate stakers for the dilution residual redemption minting causes
([economic design §1.1](ECONOMIC_DESIGN.md#11-what-the-feedback-loop-is-and-is-not)). It moves no peg, quote, spread,
or supply figure, so "stability" would claim a role it does not have, while "transfer" names the base [D18](#d18)
taxes: every user-facing stable transfer surface. Bare `Tax` spellings — `TaxCap`, `ComputeTax`, `reference_tax_cap` —
are accurate, because inside Treasury the tax is the transfer tax and the Tobin tax belongs to Market and Oracle.

## D80

### Fee ceiling and NOAH tip

The signed fee is a ceiling, and the tip is NOAH. Every fee leg but NOAH is charged at most what the chain computes it
owes — the exact transfer tax in that denomination, plus the base fee if it is the first leg, in the order the payer
encoded the fee, whose slack above the tax covers the requirement — and the rest never leaves the payer. The NOAH leg
is charged whole: it pays the base fee when no stable leg did, and the remainder is the tip, ranked in reference units
per gas through NOAH's factor. NOAH is the one denomination the tax is never owed in (`GetTaxCap` excludes the
numeraire), so it is the second signed number the fee field lacks: the chain can always tell a tip from a padded tax,
which one denomination split by subtraction could not, and a wallet pads a stable leg for rate drift at no cost. Were
excess above the tax turned into gas by subtraction, a one percent pad on a tax the size of Terra's cap would hand
validators half a gas fee. What it costs: priority needs NOAH, so a pure-stable payer gets base-fee inclusion in
arrival order within its lane, which the base-fee ramp guarantees; and a NOAH-paid gas fee has no free headroom, its
pad being a tip bounded by the fee it pads rather than by a tax. A NOAH leg is refused while NOAH's factor is absent,
as a NOAH gas fee is. A fee short of the tax is refused before anything moves ([D78](#d78)); a fee may carry several
denominations, and excess is never refused. This is not a refund: nothing is escrowed or returned, the gas limit is
charged whole at the base price, and the ante deducts less rather than handing anything back.
[Client fee construction](../clients/CLIENT_FEES.md) describes the contract for wallets.

### Transfer-tax parameter ownership

`transfer_tax_rate` is a governance `Params` field, field 12 under the `[0, 1]` domain cap, not a committee lever in
`EconomicPolicy`. The rate passes the test of [D37](#d37) — a lever stating how much the protocol wants, reversible and
safe to hold between a mandate's bounds — but under [D78](#d78) it is part of the fee every wallet signs, so a raise
refuses every in-flight transfer until clients re-query, and a committee update would land the instant the message does;
every lever in `EconomicPolicy` moves protocol-internal allocation. Market's committee band over the Tobin tax is not a
precedent: a swap carries the trader's own bound (`minimum_receive` on a sell, the required `maximum_offer` on a buy),
so a Tobin raise only fails a swap that breaches a bound the user chose, where a transfer-tax raise fails a transfer the
chain itself refuses. Governance's voting period is the notice a raise needs, and it already exists; the rate sits
beside `reference_tax_cap`, so the one calculator [D18](#d18) insists on has one owner and one proposal can activate
rate and cap together; and the committee keeps the two reward targets without being able to raise the charge that funds
them. The fast-cut argument for the committee is weaker than it looks: a cut never refuses an in-flight transaction,
since the declared fee still covers the lower tax and the fee is a ceiling, so the only fast move worth having is in the
safe direction and an expedited proposal covers it; a calculator fault is an upgrade, not a rate change. The rate is
fiscal ([D79](#d79)) and governance holds it. The economic mandate's bounds carry no tax field; `Query/Params` carries
the rate and `Query/EconomicPolicy` does not; wallets are unaffected because they price through `ComputeTax`.

## D81

`transfer_tax_rate` is held at or below the conversion spread floor. NOAH is never taxed ([D44](#d44)) and
`market.MsgSell` is exempt, so `MsgSell` stable→NOAH, `bank.MsgSend` NOAH, `MsgSell` NOAH→stable delivers the same
value to the same recipient for spread instead of tax. The cap neither softens the requirement nor changes its shape:
with `tax = min(rate × A, cap)` against a spread cost of `floor × A`, the substitution pays exactly when
`rate > floor`, at every principal — below the cap crossover both charges scale with the amount, and above it a capped
tax meets an uncapped spread. The bound is on the corridor rather than the live floor, because a conversion committee
may lower `min_stability_spread` to `ConversionMandate.minimum_policy`; that minimum is the number governance must
stay under when it sets either side. Nothing enforces this in code — Market depends on Treasury, so Treasury cannot
read the floor, and neither module's stateless `Validate` sees the other's state — so it is a governance-time
constraint on the launch rate of [P1](#p1) and on every conversion appointment.

## D82

The transfer tax is charged after the messages, on success only. The ante prices the tax once, holds the declared fee
to it ([D78](#d78)), deducts the gas fee and tip alone ([D80](#d80)), and hands the figure on through the context;
`TransferTaxDecorator`, a post decorator on the messages' branch, charges that figure to `transfer_tax_collector` and
draws the granter's allowance for it. The tax is computed once, the charge cannot exceed the declaration by
construction, and a post chain finding no figure fails the transaction rather than passing a transfer untaxed.
Collection and the tax allowance draw are execution-only: admission, recheck, and both proposal stages enforce the
signed tax declaration and the upfront gas fee but do not rehearse collection, and the post decorator collects only in
finalisation and simulation, after successful messages, reading the balances and allowances those messages left. A
transaction whose messages fail pays gas and no tax; a payer the messages leave short of the tax fails the transaction
at the charge, gas kept, principal unmoved; an allowance records what left the granter and nothing more; and a
transfer whose tax an earlier message in the same transaction funds succeeds. Nothing is escrowed or returned: this is
deferred collection, not a refund, and charging in the ante with a refund in post fails because failed messages
discard post writes. Signed transactions thereby meet the terms [D42](#d42) sets for a contract's dispatches, and the
two seams agree. What it costs: a doomed transaction pays gas for messages that then revert, rather than being refused
before they run, and one unable to pay its tax may enter a block, fail execution, and pay gas with its sequence
consumed; and feegrant emits two use events per sponsored transaction. An upfront tax affordability check, in ante or
a post rehearsal, would refuse transactions execution would have funded and repeat balance and allowance work without
reserving either. See the [tax commitment rationale](../../app/ante/README.md#transfer-tax-commitment-and-rollback).

## D83

Governance may return subsidy NOAH to the community pool through `MsgReturnSubsidy`, a fixed-endpoint message carrying
an amount and a per-proposal minimum remaining balance, evaluated at execution as `MsgFundBuffer` evaluates its own.
Without it the pool's only outflow is the settlement draw, so a chain whose gas and tax cover the targets never
touches it, while the community pool can top it up by a spend to its address ([D25](#d25)); the return closes that
asymmetry without changing the pool's default. No floor parameter: a floor read from the live targets is a projection
governance can move under by lowering the targets first, and an over-return leaves only an unfunded shortfall the next
window reports and a deposit reverses, never a halt. No automatic sweep: a rule that judges revenue sufficient reads
targets governance sets, and no target triggers a movement between funds ([D7](#d7)). No committee power: the return
is fiscal, as the tax rate is ([D80](#d80)). Governance already reaches the pool in one direction by raising the
targets, so the message adds a bounded, auditable exit rather than a new class of access. The pool is the non-minting,
balance-constrained bootstrap of [D9](#d9), with no automatic refill ([D25](#d25)).

## D84

Validator entry after launch is open: a validator bonds NOAH it holds through the ordinary create-validator transaction,
and no proposal grants it a seat or draws on any pool for it. The founding seats are the only granted stake, written
before launch by `arkd genesis add-validator-seats` from the community pool, each adding one seat's share to both reward
targets. The reward targets therefore do not track the set: they are the founding count's shares at assembly and move
afterwards only by `MsgUpdatePolicy`, inside the economic committee's corridor or by governance beyond it. The seats'
sizes are launch facts, not enforced properties. Why: a granted seat makes the founders the gate to consensus for as
long as they hold the vote, which is the control the [disbursement plan](../governance/DISBURSEMENT_PLAN.md) exists to
hand over, and a seat budget drawn from the pools would compete with that distribution for the same NOAH.

## D85

The contract query accept list admits every query the SDK annotates `module_query_safe` for auth, bank, and staking,
thirty-four paths, beside twenty-nine hand-written entries for Ark's own modules; a test requires the annotation of
every entry. Distribution, gov, and slashing carry no annotation in the pinned SDK and stay out until upstream annotates
them. Why: the annotation is upstream's review, and a list widened one path at a time for each contract need would
re-review what upstream already has; the hand-written review is for Ark's modules, whose response shapes Ark freezes by
listing them. Wasmd's native bank, staking, and distribution queriers are on; the listed paths add what they lack: the
staking pool total, the modules' params, unbonding and redelegation views, `HistoricalInfo`, spendable balances,
send-enabled, and account lookups.

## D87

Distribution grants and ongoing compensation are held and paid by the native [Disbursement
module](../../x/disbursement/README.md): one transparent commitment ledger for member grants, contributor ownership
grants, and compensation, stablecoins included. Governance creates fully funded contributor awards through typed
messages, `MsgGrantOwnership` and `MsgCompensate`; members are registered outside proposals by the registrar the grants
committee appoints ([D120](#d120)), so no proposal carries the member roll. A compensation award pays NOAH or an active
Asset registry entry; that is the only denomination gate, and the committee's allowance bounds what it awards
([D110](#d110)). Every payment, stablecoin compensation included, is an untaxed protocol disbursement, funded and paid
in its own denomination with no conversion guarantee, and compensation never counts toward ownership. Funded obligations
survive later asset lifecycle changes. The block pays each grant as its schedule falls due ([D105](#d105)), nothing pays
before two weeks after any award ([D126](#d126)), and ownership is bound by the single `ownership_cap` ([D115](#d115)).

Founding status is fixed at genesis on the beneficiary record; no award supplies or changes it. Beneficiary identity and
committed ownership persist across controllers, payees, and grants. A member changes payee only with its registered key;
the registrar has no redirection authority, and a registered address can never register again, so a cancel followed by a
registration cannot redirect a grant. Cancellation retains accrued unpaid entitlement, frozen at the effective
suspension time for a suspended member, sends nothing itself, and frees only unearned principal: a member's reopens in
the member tranche, a contributor's NOAH returns to the contributor pool, and other denominations stay uncommitted in
custody for the next award.

The module records terms, work references, payments, destinations, cancellations, and administrative changes in an
append-only journal with indexed pagination and typed events, and pays without scanning past grants. A continuation
export preserves absolute clocks and history; import rebuilds derived state after validating records and Bank backing.
CosmWasm remains available for other applications. Why: one ledger serves the distribution, which ends, and
compensation, which does not, in any active denomination; and a proposal naming member addresses would publish them for
its voting window and pace registration by votes.

## D88

Genesis holds the distribution in `x/disbursement`: 782M NOAH in two module accounts, 469.2M in `disbursement_members`
and 312.8M in `disbursement_contributors`, 60/40 ([D125](#d125)), with nothing open. An account's pool is its NOAH
outside grants; the module records the part of the member pool governance has opened, `member_tranche`. Each member step
is opened by a governance `MsgOpenTranche`, which moves no coins; members draw on the open tranche, and a cancel reopens
a member's unearned NOAH in it. The member account accepts NOAH deposits from anyone, unopened until a tranche opens
them ([D107](#d107)), and has no exit. Ownership awards and NOAH compensation draw on the contributor pool, which has no
tranche ([D117](#d117)). `MsgReturnUnallocated`, governance-only, returns any denomination outside grants from the
contributor account to the community pool through `FundCommunityPool`. Why: with the distribution in the community pool,
the 60/40 split and the member pool would be a promise the founders, the only voters at launch, could revoke by one
proposal; held by the module, the member pool pays members or nobody, and tranches keep the member pace a governance
decision, where the gates belong. What it costs: the custody accounting and tranche checks need audit before launch, and
a member remainder nobody registers leaves only by a chain upgrade.

Contributor NOAH funds stablecoin compensation in the award itself. A stablecoin award draws uncommitted custody first
and buys the shortfall through Market with contributor-pool NOAH, inside the message that creates it, offering at most
the `max_offer` it carries; Market sizes the smallest offer that buys the shortfall, so the pay and the worst price are
one decision and the NOAH cost is what the market charges. Market quotes and mints it in that message like any trader's
conversion and settles it at EndBlock; a governance award converts as its proposal executes, before Market's settlement
in the same block ([D33](#d33)). Cancelled stablecoin compensation stays in custody and funds the next award before
anything converts, and an award never converts after it is funded: obligations stay funded in their own denomination
([D87](#d87)). Rejected: standing conversion orders with an executor role and a spread cap, which pay for timing nobody
owns when sized days ahead of their fills and cannot serve a proposal.

## D89

The grants committee is Disbursement's one committee for delegated work, appointed by governance under a
`GrantsMandate`, a type that names its power as `ClaimsMandate` and `ConversionMandate` do, within amino's 39-character
message-name limit. It appoints the registrar ([D120](#d120)), suspends and reinstates members ([D103](#d103)), and
awards compensation within its term's compensation allowance ([D110](#d110)) under the per-record cap ([D109](#d109)); a
bounty is compensation of one period. A term's usage is the sum of its awards' charges, cancelled ones included, so a
cancel frees no allowance and each appointment starts the new term at zero. A term has no award-count limit: the
allowance and the per-record cap bound its value. Governance lists a term's awards by the grants query and cancels them
by ID with `MsgCancelGrants`; a second, expedited proposal cancels any the key made while the first vote is open, inside
the period before anything pays. Why: one committee keeps one appointment, term, and key process for the module's
delegated work, and the first period is what makes combining safe: nothing a committee awards pays before one period
after it, at least two weeks ([D126](#d126)), so a stolen key's added exposure is awards committed but unpaid, which the
replacement vote cancels. What it costs: one key carries every committee power, so recovery is a proposal that appoints
a new committee, which clears its registrar, cancels the old term's awards, and reinstates wrongly suspended members,
followed by the expedited cancel; and governance judges each award on the old term's list, re-awarding legitimate ones
it cancels.

## D91

The member grant is set per step rather than fixed: 20,000 NOAH at launch, set in the artefact, then the plan's 15,000,
10,000, 7,500, and 5,000, each carried as a `MsgUpdateParams` beside the `MsgOpenTranche` that opens its step's first
tranche. A member keeps the amount and schedule they registered under, so a later step never touches an earlier member,
and a tranche remainder that rolls into the next step is consumed at that step's amount. Nothing on chain forces the
schedule, and no oracle is involved. Why: a fixed NOAH amount fixes votes but not value, so what a member receives and
what a faked member is worth both float with the price, and the open tranche bounds how many fakes a stolen key
registers but not what each is worth. A falling schedule keeps both flatter if the chain succeeds and pays the first
cohort for carrying the most risk and doing the vouching. Rejected: anchoring to a stablecoin value, since the oracle
has no NOAH price until NOAH trades somewhere, a cheap NOAH would concentrate the pool into a few large grants, and it
would put the member pool behind an oracle it does not otherwise depend on. What it costs: members from earlier steps
hold more votes each; step 1 seats 1,500 people, and the pool reaches about 66,300 members ([D125](#d125)). The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) carries the schedule.

## D92

The open member tranche is the only bound on registration: there is no rolling issuance window and no registration count
in the registrar's or the committee's mandate. Why: every committee in the system is bounded by a figure governance sets
and the holder consumes, and none carries a rate limit; the member tranche is drawn by the registrar alone, so it
already is that figure, in NOAH, set by the vote that opens a step and refilled only by another vote. A second count
would restate it in another unit, and a rolling window paces by a clock no other mandate has. Registrations pay nothing
before the two-week hold ends ([D126](#d126)), so the tranche bounds what a stolen key commits rather than what it
takes. What it costs: a stolen key can commit whatever is open, so governance paces a step by opening it in quarters
([D126](#d126)); nothing closes an open tranche short of registering it, so the lever against a registrar governance
wants to slow is to open no more, and against a stolen key the committee's reappointment of the registrar, which
advances its term ([D120](#d120)).

## D93

The founding seats do not grow with the distribution. No step moves NOAH from the pools into the seats, and no rule
weights a seat's vote above its stake. Resistance to a buyer of distributed NOAH comes from the cost and visibility of
the purchase, the unbonding period, validators voting the delegations they carry, and the relaunch, never from founder
stake. Why: a buyer who stakes what they buy holds a third of the vote, enough to block any proposal or halt the chain,
with about half of what everyone else has bonded; passing alone takes twice that, a third of supply or more, bought from
tens of thousands of holders on an open market. Founder stake barely moves that bar: each NOAH moved from the member
pool into the seats raises it by less than half a NOAH, and for the founders to keep a veto once a buyer holds one they
must hold more than the whole public bonded stake, the chain the plan hands over. Larger seats also enlarge the cheaper
route: four nodes at ten seats can halt the chain from block one, so a hostile actor pressures a few known people rather
than buying a tenth of the supply, and seats transfer from year four, so a larger seat is a larger block for the same
buyer. Two levers stay open, each its own change: a minimum lead time, fixed in code, between a vote and an upgrade or a
large pool spend, so a bought vote is visible before it executes; and opening the distribution schedule once, with the
on-chain gate releasing each step, since a third of the vote stops every `MsgOpenTranche` and every committee
re-appointment. A member tally over the irreversible message classes, the one electorate a buyer cannot purchase, is the
direction if those prove insufficient; it is not planned. The [threat
model](THREAT_MODEL.md#27-governance-and-committees) carries the figures.

## D95

Conversion is closed at launch and opens by vote once NOAH has a market price. The launch artefact registers no asset;
the eleven feeds, the sidecar's placeholder NOAH leg, and the seeded gas factor stay, and Market's depth is 100,000 XDR.
Governance registers the ten launch currencies in one proposal once the oracle reads a live NOAH price, after operators
have added NOAH venues to their sidecars together and only once a venue has depth. Treasury refreshes the NOAH gas
factor from the reference rate whether or not a currency is registered, so fees track the first live price block by
block rather than jumping at registration. Depth is a human lever, raised by the Market committee inside its corridor
and by governance beyond it, the first appointment's corridor reaching about ten times the genesis depth, and sized by
[D104](#d104). Why: every oracle rate is NOAH per unit, and its NOAH leg is a configured placeholder, one NOAH a XDR
([D128](#d128)), until a provider reports a live sample. A stablecoin is a claim on NOAH at the oracle rate, so a
currency minted at the placeholder is re-marked when the live price arrives: if NOAH trades at five cents, an XDR that
cost one NOAH redeems for about 27, the difference is minted, and every holder pays whoever minted early; the liability
and every fund target are valued at the placeholder until then; and the first live price comes from the first venue to
list NOAH, so whoever controls a thin venue sets the redemption rate of everything minted before it. An empty registry
is the closed position because Market mints only an `ACTIVE` ask and there is no status between registration and
admission, and it is the asset module's own admission cycle, feed first and registration once it prints, applied to the
launch set. Nothing else on day one needs a priced NOAH: gas is seeded, the oracle lane grades FX legs that are real,
tax and the Treasury waterfall key off registered currencies, and the funds hold NOAH at face value; closed conversion
makes the placeholder harmless, and the oracle lane is the only reason to keep it. The depth is set in the artefact
rather than by a later vote because it is inert until registration and sized for members converting real amounts, about
2,000 XDR a swap at the floor and 1,000 a day, small enough that a pushed thin price mints little at the wrong rate; it
never grows on its own, since depth is the throttle on how fast NOAH mints and burns and the 2022 Terra expansion is the
whitepaper's accelerant. Rejected: registering the currencies and relying on the missing rate, which opens conversion by
itself at a thin price the moment any provider samples NOAH, at the launch depth and without a vote; a low placeholder
with conversion open, which bounds the loss without removing it; and `ISSUANCE_HALTED` at genesis, a wind-down status
that admits redemption. What it costs: no stablecoins before NOAH trades, when there is little to spend them on;
Disbursement's stablecoin compensation waits for registration; the seat floats play no bootstrap role; and the testnet
artefact registers the ten currencies and keeps a 5M depth, since a testnet exists to exercise conversion and the e2e
suites run on it. The opening sequence is [governance operations
§6](../governance/GOVERNANCE_OPERATIONS.md#6-opening-conversion).

## D96

Each founding seat carries a liquid float of 50,000 NOAH, granted at genesis from the community pool beside the seat.
Why: the float's job is the week before seat income starts, when unvested coins cannot pay fees: gas at a few hundredths
of a NOAH a transaction, proposal deposits of 1,000 and 5,000 refunded unless vetoed, and a committee account's funding,
under 10,000 in a busy week. 50,000 covers that five times over and is about six months of a regular seat's income at
ten seats ([D124](#d124)). A larger float would serve founders minting the first currencies, a job closed conversion at
launch rules out ([D95](#d95)); above operations it buys only sellable supply on day one and a larger sum for a coerced
founder to lose. It is per seat rather than a fixed total because it is an operating cost per operator, not a share of
the vote, and a genesis grant rather than a later award because nothing else could pay for the proposal that awarded it.
What it costs: ten floats are 500,000 NOAH, a twentieth of a percent of supply. The [launch
genesis](../governance/GENESIS.md) carries the figure.

## D101

Steps two to five open on fixed minimums of public bonded stake, each three quarters of the planning rate applied to the
step's distribution, with the measure and figures of [D132](#d132), and a gate is the only step condition: the previous
step's contributor budget need not be spent, its remainder rolls forward with the member remainder, and no record at its
cap holds a step. The plan is sized at a 20% staking rate of paid coins, the rate a live chain paying what Ark pays
falls back to in a bad year; 30%, the rate Terra ran at on fee-funded rewards before its collapse, is the active rate,
and 15%, Terra Classic today, is a scenario every genesis figure must survive, since Ark's rewards, 1.3M NOAH a year
across ten seats, do not make 20% a floor. Genesis figures are sized to hold at 20% and improve on their own at 30%;
figures set by vote are set so the plan works at 20%, and the reward targets are the margin if the rate comes in under
it. The seat is a founding allocation with long vesting that a founder who leaves keeps, not pay for service. Why: a
gate read as a share of payments is met by the first members' tenths and stays met while later steps commit grants that
pay nothing for a year, so the same small stake could open every step with the founders holding nearly all of it; a
fixed figure makes each step prove new participation, at the cost of pace, which is the handover's real speed. A rule
requiring a step's contributor budget to be granted before the next opens would reward awarding ownership to unlock a
stage, so grants are sized to the work and what is not awarded rolls forward. At equal staking from about 20% up,
members hold the largest share of bonded stake once grants are delegated, narrowly at 20% ([D129](#d129)), and the
majority only with the other contributors, since founders bond everything. What it costs: slow member recruitment can
delay later contributor rounds, the price of a gate that measures handover; and a step number says how far the handover
has come, since escrow cannot lower a gate.

## D102

Membership obliges nothing: a member holds, stakes, votes, or sells by choice, and the chain needs both choices, since
those who stake carry security and the vote and those who sell are the float. In the first year member payments are
nearly all the liquid NOAH there is, beside the founders' 50,000 floats and about 1.3M of seat income, so the market
outside the chain forms from member sellers, and the pace governance opens a member tranche at is also the pace of the
float. The grant is therefore ownership rather than pay, and the joiner and the faker bound it, not the work: the floor
is the cost of joining, an identity check, a voucher, and a wallet, under which it reads as an airdrop; the ceiling is
the price of a bought identity, since with nothing owed a fake does nothing a real seller does not, so each step's vote
holds the tenth, the part no finding after the hold recovers, under that price; and the pool over the grant is the roll.
The launch figure is 20,000: nothing is priced at launch and the first cohort is founder-vouched, so it risks least, and
the later figures in [D91](#d91) are illustrations each step's vote applies the rule to. Why: a labour anchor for the
floor, the skilled-year rate or the bounty range, would condition the grant on participation, which pays for votes,
which the app's rule of casting none forbids, and a motive test cannot separate a mercenary from a fake once selling is
legitimate; identity and vouch carry the whole guard. What it costs: the member vote self-selects to those who stay, and
to buyers who paid to, which is the handover by market the plan accepts; the step-1 cohort holds four times step 5 in
NOAH and sells into the thinnest market, the price of having carried the most risk; and counsel's question narrows to
how a grant with nothing owed is characterised.

## D103

A suspension stands until the committee or governance reinstates it, across a committee replacement or a mandate expiry
alike, and records only its own time, which a later cancellation uses as its cutoff. Nothing lifts suspensions in bulk
on a term change: there is no voided-term set and no void message. Why: lifting suspensions as a side effect of a term
change, by a void or by scoping them to the term, would make a routine re-appointment drop the outgoing committee's
legitimate pauses, and the explicit lifts exist, `MsgCommitteeReinstate` and governance's `MsgReinstateMembers`; a
stolen key's mass suspension costs governance one `MsgReinstateMembers` per hundred members, the price the key paid to
make them. What it costs: recovery from a stolen committee key is a proposal of three message kinds, appoint, cancel the
term's awards, and reinstate; and the committee term a suspension is made under is not stored, since the journal's actor
and height identify it.

## D104

Market's depth is sized from the conversion it should pass at the spread floor: the larger of a hundred times the daily
net one-way flow and fifty times the largest routine swap, each valued in the pool unit at the oracle rate. The flow is
estimated from distribution, liquid NOAH times its live price times the share expected to convert in a month, over
thirty. The rule is run with a live price before conversion opens, again when a tranche adds liquid NOAH
([D102](#d102)), and when the pool holds an expansion-side imbalance beyond a hundredth of its depth. A raise is checked
against what it exposes: a mint at a wrong price stays profitable up to roughly the mispricing times the depth, so while
the venues pricing NOAH are thin the raise is cut and the spread carries the excess. Depth is not raised while the
imbalance is on the redemption side, and never by shortening recovery ([D95](#d95)). Why: a swap of `x` from a rested
pool pays `x / (depth + x)`, which reaches the 2% floor at about a fiftieth of the depth; each block returns one part in
the recovery period of the imbalance, so steady one-way flow holds the imbalance at one period's flow, and the marginal
spread, about twice the imbalance over the depth, stays at the floor while a day's flow is within a hundredth of the
depth. Under another policy the factors are two over the floor times the recovery period in days, and one over the
floor. The genesis 100,000 XDR is this rule applied to members converting in the first months ([D95](#d95)).
Distribution is the input because liquid NOAH is all that can be offered to mint; liability is what that flow leaves
behind, so it confirms a sizing and does not produce one. Rejected: a larger depth in the artefact, which serves no
holder at opening, scales what the first thin price can mint, and would be set before the price or the float is known;
sizing to swap volume, which is what depth throttles and part of what Terra's January 2022 raise cited; sizing to
liability alone, which lags the flow and yields no figure; and any automatic adjustment ([D95](#d95)). What it costs:
the converting share is a judgement, not a measurement; flow above the sizing pays more than the floor until someone
acts; and no alert reports the imbalance, so whoever sizes reads `Query/Pool`. The procedure is the [committee
runbook](../governance/ECONOMIC_COMMITTEE_RUNBOOK.md#conversion-depth).

## D105

The block pays. Every grant that owes principal and is not suspended carries `due_time`, the block time at or after
which the module next looks at it, and the end of each block pays the grants whose time has come, oldest first and at
most 1,000 a block, each what its schedule has accrued and not yet paid; one payment catches up every boundary passed,
and the grant then waits for its next boundary. Registration queues a member for the end of its hold, a contributor
award its first boundary, cancellation its retained debt and reinstatement its arrears for the block they happen in or
the end of a member's hold, a payee change a look in its own block, never before the grant could first be paid, and
suspension leaves the queue. There is no release message: no transaction pays a grant, and `Query/Grant` reports each
grant's `paid`, `remaining`, and `due_time`. A send Bank refuses at the payee is deferred a day, or to the next boundary
if sooner, and published in `EventPaymentDeferred`, since the payee is the one input no write can vet against every
later restriction; every other failure in the hook is an accounting invariant and fails the block. Why: a member grant
is the handover, and a payment that needs a signature is a payment some members never collect, a transaction the app has
to make for them, and a gas cost on receiving; with the block paying, every member is paid in full by doing nothing, the
app's transaction after each payment is a delegation, and the chain has no any-sender payment message. The queue holds
one entry a grant, so a halt of any length costs one catch-up payment each, and the per-block bound drains a backlog at
1,000 a block. Rejected: lockup accounts in the payee's own account, which would delegate and vote unpaid principal and
offer no clawback for suspension or cancellation; and a release message beside the block, an any-sender surface for
payments the block already makes. What it costs: Disbursement has a block hook, which the write-time refusals and the
deferral keep from halting; and a cancellation pays its retained debt in the same block.

## D106

There is no founder-bloc rule. An ownership award to a seat holder is bound by `ownership_cap` alone, like anyone's, the
seat outside it ([D115](#d115)): no rule binds the seat holders to each other, the module keeps no founder totals, and
the beneficiary query reports the record's own room, shared with no one. Why: a group rule binds the founding seats to
each other, one founder's grant paying less because another's is paid, which assumes they act in unison. They are
independent validators who may vote in different directions, and the chain cannot tell a coalition from strangers, so a
cap that names holders by genesis identity is not neutral: every other set of holders is bounded record by record, and a
bloc rule holds the founders to a stricter standard for who they are rather than what they hold. What such a rule would
protect is the launch window, when the founding set holds the whole vote and could grant itself; that stays policy: an
ownership proposal states its case and the grantee abstains ([D127](#d127)), and the founder's own awards are written at
genesis, approved by every gentx ([D136](#d136)). Rejected: a pro-rata share of a group room, and a per-seat cap, both
of which keep the group assumption. What it costs: the founding set together is bounded by what the public votes it and
by a cap per record, not by a group rule, and whether the seats together stay under a third is the public's to decide.

## D107

The member pool accepts NOAH deposits from anyone. The member account is off Bank's blocklist, and the Disbursement send
restriction, which admits exactly one positive NOAH coin into it on every Bank path, module-to-module included, is the
guard. A deposit joins the pool unopened: it is spendable only through a tranche governance opens, and the account has
no exit, so it pays members or nobody. Why: the contributor account accepts deposits in what it may hold, and a
NOAH-only guard that binds every path makes the two accounts symmetric, so a donor or a proposal can top up either pool
without a module change, where a blocklist guards only the message paths. What it costs: the member pool's size is its
genesis figure plus what arrives, and governance's tranche votes are the only thing that turns any of it into grants.

## D108

The grants committee compensates a founding seat holder as it does anyone; by policy no one is granted anything for
validator work, which seat income pays. Why: a refusal to compensate a seat holder binds a record, not a person, so it
binds the honest path only; the kickback it would guard against, the appointing founders paying themselves through their
delegate, is bounded by the term's allowance governance sets and the per-record cap ([D109](#d109)) and undone by the
replacement vote; and a seat holder paid on the seat record is visible there, where a second address would not be. What
it costs: a seat holder's pay does not need a vote, so the plan's "grantee abstains" check applies to proposals alone.
Every committee award is compensation; ownership is governance's alone ([D113](#d113)).

## D109

One beneficiary record's compensation is capped: at most `compensation_cap`, 1,000,000 in the reference unit at launch,
a rolling `compensation_window` of 26 fortnights, 31,449,600 seconds ([D129](#d129)), on the proposal and committee
paths alike. An award is valued in the reference unit at the fresh oracle rates when it is created, rounded up, and the
value is written on the grant as `award_value`; the check at award sums the record's charges as [D119](#d119) and
[D122](#d122) measure them, cancelled awards included for their first window, so a cancel frees no room inside it, and
refuses an award that would take the sum past the cap. Both figures are operational params. Why: without it compensation
has no per-person bound: the term allowance caps the committee's total and the contributor pool the programme's, and
nothing stops one record taking all of either. The window sum is the pay analogue of the ownership cap; it bounds the
quantity itself, composes with streams and bounties, and sits at the write, where a human is in the loop, rather than in
the block. The reference unit holds the figure steady where a NOAH cap would move with NOAH's price, and locking the
value at award time keeps a past award's counted size from moving afterwards. Params, because pay scale follows the
market. Rejected: one live award per record, which bounds concurrency rather than quantity and chains short awards to
many times the figure; a per-award cap alone, which overlapping awards defeat; revaluing past awards at current rates,
which reintroduces the volatility; a window accumulator in state, which the grants already hold. What it costs: a
compensation award needs a fresh rate for its denomination and the reference unit, so none can be awarded before the
first reference observation and a stale feed refuses pay until it recovers; a pushed rate changes how much of the
allowance one record can take, within the oracle's own bounds; a record at its cap waits for the window to roll or a
params vote; and a record with many grants pays a longer walk at award time.

## D110

The grants committee's term allowance is one figure, `compensation_allowance`, in the reference unit: every committee
award, NOAH or any active asset, charges what it drew, its custody and the NOAH it converted, at the rates
[D109](#d109) values it at, written on the grant as its charge ([D140](#d140)), so the check reads no rate the award did
not already need. There is no denomination list. Why: compensation is pay, and the reference unit
holds pay steady where a NOAH figure would float with NOAH's price. Rejected: one figure in NOAH, which charges nothing
for an award custody already holds; and a denomination list, since every compensation award already needs a live
reference rate, so restricting pay to NOAH bounds nothing. What it costs: the live term's usage is one figure the module
keeps and genesis carries, zeroed at appointment as Claims zeroes its allowance and checked against the term's grants at
import; it and the allowance follow a reference re-point ([D123](#d123)); and there is no per-asset bound on what the
committee converts through Market, which the allowance bounds in value, the spread included.

## D111

Every founding seat but the founder's, which [D112](#d112) sizes, is 5,000,000 NOAH, the same at any count the launch
review admits, granted from the community pool, which holds 103M before seating and keeps 35M after ten seats with the
founder's. The count sets the pool's cost, never the seat. Why: 5M is the year-one floor's seat. At the minimum launch
count of seven it gives a founding stake of 45M, the founder's 15M among it, clear of the 40M a buyer of the first
year's liquid NOAH could take a third against ([D112](#d112)); 4M seats would put seven at 36M, under that line, and
force a larger minimum count. Per seat rather than per total, because what a founder is promised at the review should
not shrink because the review admitted another. Rejected: a fixed founders' total divided by the count, which would let
the count move every founder's promise; the lines it would hold steady, what the seats can pass or block together, are
analysis rather than rules, since no rule reads a founder total ([D106](#d106)), and the [disbursement
plan](../governance/DISBURSEMENT_PLAN.md#5-handover) states them per seat. What it costs: a double-sign takes 250,000
NOAH of a seat, and the community pool's remainder moves with the count. The [disbursement
plan](../governance/DISBURSEMENT_PLAN.md) and [launch genesis](../governance/GENESIS.md) carry the figures.

## D112

The founder's position is sized per seat, at genesis. Every other seat is 5M ([D111](#d111)); the founder's seat is half
the other seats' sum, a third of the founding stake at any count, 22.5M at ten: one continuous vesting account on the
seat schedule ([D114](#d114)), drawn from the community pool like every seat, so the pool keeps 35M at ten. The
founder's gentx self-delegates one seat, and the founder delegates the rest across the other seats in the first blocks,
so no node holds the third and halting still takes four nodes at ten seats. Launch assembly seats seven to thirteen:
under seven the founding stake falls under the 40M a buyer of the first year's liquid NOAH could take a third against,
and over thirteen the founder's seat passes 3% of supply. Why: with every seat independent, a grant voted years later by
strangers is no assurance to the founder, and one seat among the set is no weight in the launch electorate, so the
founding allocation is genesis state sized by the threshold; "a third" is a share, so its NOAH amount follows the count,
while every other seat is the absolute promise of D111. The third is the veto, chosen on purpose: the founder blocks
alone and the other seats together cannot pass over an active no, since two thirds exactly is under the strict 66.7%;
the founder passes nothing alone, a third being under quorum; passing takes the founder plus half the other seats, and
blocking the founder alone or half the others. It dissolves on a per-seat schedule that does not depend on the count:
the solo veto at public bonded stake of about 0.3% of the founder's seat, roughly 68k NOAH at ten, and the founder plus
m seats at 15M times m. Spreading the delegations prevents an accidental halt; a deliberate one stays one redelegation
away, inherent to a third. Rejected: sizing every seat from a fixed total, which would undo D111 for D111's own reason
and make the dissolution schedule move with the review; a seat that grows below seven, since a six-seat launch is a
decision rather than a silent adjustment; the largest position under a third, which gives up the veto for one base unit.
What it costs: the year-one floor is a count rule at seven; the founder is the largest wallet until bonded stake passes
ten times the founder's seat, grant, and stream ([D129](#d129)); the plan's goal excepts the founder at genesis; and the
spread is a launch practice genesis cannot hold, since validators exist only once the gentxs execute. The [disbursement
plan](../governance/DISBURSEMENT_PLAN.md) and [launch genesis](../governance/GENESIS.md) carry the figures.

## D113

The grants committee awards compensation only. Every committee award is compensation, NOAH or an active asset, charged
at its reference value against the term's compensation allowance and under the record's compensation cap
([D119](#d119)), and nothing it awards pays before one period after the award ([D116](#d116)); the award message
carries no kind and the mandate no ownership allowance. The committee may compensate a seat holder like anyone.
Ownership is a governance proposal only, in a review round with the case in the summary and the grantee abstaining
when the grantee votes. A member grant records the registrar term that registered it and a committee award the
committee term, so the grants query lists a stolen key's registrations and awards by term; ownership carries no term,
and import refuses one that does. Why: a contributor grant is a decision about who owns the chain, argued in public,
and the committee is a body the launch set appoints. Treating the kinds as one menu because a NOAH compensation award
is stakeable NOAH too is true of the coins and false of the scale: under the compensation label the committee's output
to one record is bounded at pay scale, 1M in the reference unit a year, while ownership is bounded by the 15M
`ownership_cap` ([D115](#d115)), an order of magnitude more, which the committee would exercise without a vote per
grant. The first period and governance's cancel by ID bound a stolen key, not an appointed committee's judgment about
ownership. What it costs: a hire's ownership grant needs a proposal while its pay stream can come from the committee,
the shape of the plan's hire package. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) and
[module README](../../x/disbursement/README.md) carry the surface.

## D114

Every founding seat, the founder's included, vests behind a four-year cliff and over a four-year tail, fully vested at
year eight. Why: vesting gates transfer alone, since unvested seats delegate and vote from block one, so the schedule
decides when founding stake can be sold and nothing in the handover arithmetic. The cliff covers the launch window, when
the founding set is the electorate deciding every grant and the founder's coalitions still block ([D112](#d112)), and
keeps the floats the only liquid NOAH a founder holds while the first member cohorts are the whole float. The tail paces
the exit, not service: the seat is an allocation a leaver keeps ([D101](#d101)). It is sized against the member flow,
roughly 30M to 45M a year through the middle of the distribution at the planning pace: a one-year tail unlocks the whole
founding stake inside year five, above that year's member flow and with the founder's seat still about a fifth of the
vote; two years leaves founders about half of what is sold; three about even; four keeps them a minority, 16.9M a year
at ten seats, and finishes about when public bonded stake passes the seats, near the step-5 gate, so the founding stake
is fully for sale only once the public outweighs it and a buyer assembling a third faces the open market rather than a
few known sellers. Rejected: the one-plus-three norm, sized for teams who are not the electorate; tails of one to three
years, for the reasons above; a shorter cliff. What it costs: the founder's seat unlocks 5.6M a year at ten; the
founding stake is fully for sale at year eight, with the founder's seat near an eighth of the vote then; and the signal
is weaker than a longer tail's, though four years locked and four more vesting is still double the norm on both counts.
`SeatVestingCliffYears` and `SeatVestingEndYears` in `pkg/chain/launch.go` carry it; Disbursement reads no seat
schedule. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) and [launch genesis](../governance/GENESIS.md)
carry the figures.

## D115

One figure bounds ownership: `ownership_cap`, a Disbursement parameter governance sets, 15M NOAH at launch, the most
one beneficiary record's ownership grants commit from the pools over every grant, checked when each grant is awarded
([D121](#d121)). There is no ownership fraction, ceiling, band, or founder-bloc cap. The module reads neither bonded
stake nor the seat: the seat stays on the record as a record nothing reads, and the beneficiary query reports the
record's committed ownership and its room under the cap. The founder's 15M grant for the work before launch equals the
cap and is written at genesis ([D136](#d136)). Why: a fraction of bonded stake bounds a record's share of the vote by
assuming the record stakes everything it is paid, the one assumption the module cannot replace with a measurement, and
against a public that sells most of what it is paid it holds contributors to a tenth of a small vote while their
grants promise figures it would not pay for years. With the founder's seat a third of the founding stake, any
fraction up to a third leaves the founder's record no room unless the seat leaves the fraction's base, and a fraction
that excludes the seat bounds nothing a seat holder does. One figure says what the plan means: nobody is paid more
ownership than the founder's grant, so the founder sits at the edge and no record leaves the pools heavier. The seat is
consensus weight, from the community pool and vesting on the seat schedule; the 15M is pay for work, from the
contributor pool on the ownership schedule, bound by the cap like anyone's. What the figure means for the vote is
stated rather than enforced: at the founding stake a record at the cap is about 18% of the vote, two are 31%, and three
block; at 150M of bonded stake a record is a tenth; at the planning-rate end about 6%. Each seat holder's record is
bound by the same cap, and nothing binds the seat holders to each other, since they are independent validators and a
cap that pays one less because another is paid treats them as a bloc. Rejected: a fraction with the seat outside
it, which bounds pool payments but not a seat holder's vote; a fraction with the seat inside it, which pays the founder
nothing in any projected case; a discount on own stake for an assumed selling rate, which is a projection in a
halt-class rule and equivalent to a larger fraction for the one holder who does not sell; removing the seat from the
record, kept as a record. What it costs: the cap does not track the vote, so on a sell-heavy chain records at the cap
are a larger share of a small vote, and three wallets at the cap block at the founding stake; governance can raise the
cap by a parameter vote, bounded only by the positive 128-bit check, which the launch electorate could do for itself
([D127](#d127)); and the seat-count bound at thirteen rests on the founder's seat reaching 3% of supply. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md), [launch genesis](../governance/GENESIS.md), and the
[module README](../../x/disbursement/README.md) carry the figure and the formula.

## D116

Each grant kind's shape is a module parameter, not a message field. Every shape is one `Schedule` of five integers:
`first_length` and `first_weight`, each further period's `period_length` and `period_weight`, and `periods`, the count
of further periods. Ownership is a cliff of 26 fortnights paying a quarter, weighted as the fortnights it spans, then
biweekly for three years, 79 payments over 104 fortnights. Compensation is a count of `compensation_period` periods,
fortnights at launch, in equal shares, one for a bounty, at most ten years at any cadence (`MaxCompensationDuration`,
260 fortnights). Members take a two-week hold paying a tenth, then thirteen four-week periods ([D126](#d126)). A grant
copies its shape at creation rather than an expanded period list, and accrual is one division, so its cost does not
grow with the schedule. The grantor sets the clock's start, up to a century ahead: an ownership clock no earlier than
its award, since the cliff is the point and the amount credits past work; a compensation clock at most a quarter back
(`MaxCompensationBackdate`, [D122](#d122)), by proposal or committee alike, so a stream aligns to the day a hire
started. Each grant records the block time of its award, `created_time`, beside its start, and nothing accrues
before one period after it, wherever the clock starts: a backdated award pays its elapsed periods at that first look,
a clock already run out is due then, and a cancel before it returns everything. That period is the window governance
has against a stolen committee key, and every first period is at least `MinFirstPeriod`, two weeks
([D126](#d126)); the compensation window measures from the same time. Durations are whole weeks and a year is 26
fortnights ([D130](#d130)). Why: a free schedule in the messages makes every award a shape decision and the plan's "one
schedule from approval" a convention nothing checks; a shape in params is the convention as a rule, and governance
moves it for future grants without touching live ones, which keep their copy. Biweekly is pay's cadence and puts
ownership on the same clock, so the block pays everything fortnightly, and a count of periods rather than years keeps
the cadence out of the count, so a six-month engagement is thirteen fortnights rather than a rounding. Rejected: a
settable count for ownership, since ownership is the plan's one stated instrument and the cap bounds it; a shape per
award kept in a policy table, which is the free schedule again; bounties as a kind of their own, since a bounty is one
period; and a backdated ownership clock, which pays the cliff at the first look and would let the founding set pass
itself grants with clocks already run out. What it costs: the smallest award pays nothing for two weeks; a proposal
that took weeks to pass cannot reach an ownership clock back to the agreement, which sizing answers; and ten years is
the longest award, so a longer engagement renews. The [plan](../governance/DISBURSEMENT_PLAN.md#4-distribution), the
[module README](../../x/disbursement/README.md#schedules-and-cancellation), and the
[governance runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) carry the shapes and the messages.

## D117

There is no contributor tranche. `MsgOpenTranche` names no account and opens the member pool alone. The contributor
account's NOAH is one pool: every contributor award draws uncommitted contributor custody at creation, governance's by
proposal and the committee's within its term allowance; a stablecoin award converts its shortfall from the pool; a
cancel returns unearned contributor NOAH to the pool; and `MsgReturnUnallocated` draws custody outside grants. Why:
every draw on a contributor tranche would be either a proposal, which the same electorate could open in the message
that needs it, or a committee award already bounded by the allowance at reference value, the record's compensation
cap, and the first period before anything pays with governance's cancel behind it ([D113](#d113)). A ceiling
governance raises in the message that needs it bounds nothing, and the step accounting it would keep is the
reservations and payments the balance query shows. The member tranche stays because it bounds a key that is not
governance, the registrar ([D120](#d120)). Given up: an oracle-independent NOAH ceiling over committee awards, since
the allowance and the record cap are reference-valued and a pushed rate stretches both; the first period and the
cancel are the answer. What it costs: a NOAH deposit into the contributor account is grantable the block it lands, and
a step's contributor budget is plan arithmetic rather than a chain record, a guideline ([D131](#d131)). The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md), [module README](../../x/disbursement/README.md), and
[governance runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) carry the surface.

## D118

Ark has no IBC: no IBC core, transfer route, packet forwarding, rate limiting, callbacks, interchain accounts, general
message passing, or 07-Tendermint or 08-Wasm client, so no IBC store or genesis section, no redundant-relay ante
decorator, no Treasury tax input for `MsgTransfer` or `MsgSendPacket`, and no security-committee client recovery.
Wasmd keeps ibc-go as an indirect dependency, so the contract runtime is wired with nil channel keepers: a contract's
IBC messages and queries are refused with an explicit error, its IBC entry points are unreachable, and the `ibc2`
capability is withheld so a contract requiring it fails at upload. Why: no counterparty route is planned, and a dormant
stack would still have to be carried by the tax calculator, the policy router, the launch artefact, the threat model,
and a two-chain e2e tier. What it costs: interoperability, if ever wanted, starts from a decision and a store
migration rather than a switch.

## D119

A compensation award charges the record's cap at its yearly rate. An award whose duration is within the window charges
its whole value, so a bounty counts whole in the year it is paid; a longer stream charges its value spread over its
years, `value × window / duration` rounded up as a bound, while it runs; a backdated award's first window is charged
by [D122](#d122). The check at award sums the charges of every compensation award to the record that is still running
or made inside the trailing window, cancelled ones included for that window, and refuses an award that would take
the sum over the cap. Why: the plan says "1M a year" and means a rate. Counting an award's whole value in the year it
is awarded would make the cap a bound on a stream's total, 1M however long it ran, about 100,000 a year over ten years
([D116](#d116)), under the plan's own skilled-year rate. Charging by rate fits a ten-year stream at the design rate,
131,000 a year, and seven of them beside each other, while an eighth does not; a bounty is unaffected; and
award-cancel-reaward cycling stays bounded, since a cancelled stream's charge persists for the window after its award.
Rejected: counting the whole value at award, for the reason above; a cap scaling with duration, which lets two long
awards in one year take twice the cap; counting a stream's later years only as they arrive, which lets every year's
awards each take the cap. What it costs: a running stream counts against the record for its whole life, so a record
on a long stream has that much less room for bounties each year, and the check reads a stream's duration beside its
value. The [module README](../../x/disbursement/README.md) and the [plan](../governance/DISBURSEMENT_PLAN.md) carry the
rule.

## D120

A registrar registers members, appointed by the grants committee and under it. The registrar is a second appointment in
Disbursement, a bare mandate envelope the grants committee sets under its own term with `MsgCommitteeRegistrar`, for a
window of block time inside the committee's ([D133](#d133)); the committee may name itself, and each appointment
advances the registrar term. The registrar is the committee's delegate, so a committee appointment, replacement or
disabling, clears it, and governance reaches it only through the committee. `MsgRegisterMembers`, the one registration
message, is the registrar's: it carries the registrar term and draws on the open member tranche, the only bound on
registration ([D92](#d92)). A member grant records the registrar term and a compensation award the committee's, and the
grants query lists each by its own term. `MsgRegisterMembers` rides the normal lane: the lane is the property of being a
committee message, registration is routine traffic rather than incident response, and the appointment,
`MsgCommitteeRegistrar`, is the committee message that rides the committee lane. Why: at a hundred addresses a batch,
fifty thousand members is at least five hundred registration transactions, and with the committee registering each would
be a multisig signing ceremony, while the member platform registers as admissions clear, not on a signers' calendar. The
committee stops a stolen registrar in one transaction rather than two votes, the proposal appointing a stolen
committee's successor clears the stolen committee's registrar in the same write with nothing more to carry, and
governance cancels the fakes from the old registrar term's list. Rejected: a governance message setting the registrar
beside the committee's, which makes the registrar free-standing and needs its own place in every recovery proposal, when
one appointer makes the delegate that appointer's term-scoped state; a per-term registration allowance the appointer
sets, which would let the committee meter a step opened whole but adds a usage figure, a refresh cadence, and a second
bound when the tranche already is one and sizing it is governance's job; an `x/authz` grant from the committee, which
outlives the term and lets one key act under a shape recorded for a quorum. What it costs: the registrar is the hottest
key on the chain, a single key on the platform's signing service, and a stolen one commits grants against whatever
governance has opened until one committee transaction advances its term, which the two-week hold ([D126](#d126)) keeps
from paying; and each committee appointment is followed by one committee transaction naming the registrar again. It is
the one appointment a committee makes; the [mandate package README](../../pkg/mandate/README.md) carries the departure,
and the [module README](../../x/disbursement/README.md), the [plan](../governance/DISBURSEMENT_PLAN.md), and the
[governance runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) the surface.

## D121

The ownership cap is checked when a grant is awarded, not when it pays. A record's committed ownership is what its
ownership grants have paid or still owe, kept on the beneficiary record as `ownership_committed`; an award that takes it
past `ownership_cap` is refused, and every ownership grant then pays in full on its schedule. A cancel takes the grant's
unearned principal out of the figure, so a grant cancelled inside its cliff frees all its room and one cancelled later
keeps what it earned committed. A cap vote changes what later awards admit and leaves existing grants on their terms;
governance stops a grant by cancelling it. Why: with a fixed figure a record's room only shrinks unless governance
raises the cap, so whether a grant will be paid in full is known at award; a check at payment would let a proposal
award what the module never pays, leave the excess reserved in the contributor pool, and look at a capped grant every
day indefinitely. Award time also matches the compensation cap and the rule that a parameter change leaves existing
grants on their terms. Rejected: counting a cancelled grant's whole principal, which would make a mistaken award spend
the record's room until a cap vote, when ownership is governance's alone ([D113](#d113)) and the award-cancel cycle the
compensation cap guards against has no committee to run it; walking the record's grants at each award as the
compensation cap does, which a running total the award and the cancel both touch makes unnecessary. What it costs: a
lowered cap does not stop grants already made, so governance cancels them; a raised cap resumes nothing, since nothing
is held; and a payment Bank refuses is the only one that waits a retry ([D105](#d105)). The
[module README](../../x/disbursement/README.md) and the [plan](../governance/DISBURSEMENT_PLAN.md) carry the rule.

## D122

A compensation clock starts at most a quarter before its award, `MaxCompensationBackdate`, 13 weeks
([D130](#d130)), a constant checked when the award is made, by proposal or committee alike. In the window after it is
made, a backdated award charges what it pays there: its value spread over a window plus its backdate,
`value × min(duration, window + backdate) / duration` rounded up, where the backdate is how far its clock starts
before the award; after that window a stream charges `value × window / duration` while it runs ([D119](#d119)). An
award whose clock starts at or after the award charges by D119 alone. Why: the first look pays the backdated periods at
once, so spreading an award's value over its whole duration while counting it only from when it is made would pay a
record faster than it is charged. A stream two windows long and backdated one would pay its whole value inside the
window after its award while charging half of it, and once it ended and aged out an identical award would fit again,
so one record could draw twice the cap every window on ordinary awards, the committee's included, with no cancel.
Charging the first window what it pays closes that at any reach. The quarter follows practice: vesting commonly starts
at a hire date weeks or months before the board approves the grant, and back pay covers the gap, while a year of reach
is longer than practice. Rejected: no backdating, which gives up paying for work a proposal took weeks to approve; a
shorter reach without the first-window charge, which still lets a record draw a quarter more than the cap a window; a
parameter for the reach, which a params vote could widen; and checking the reach in grant validation, which every
payment runs, so a later binary lowering the constant would fail stored grants in the block. What it costs: a
backdated award uses more of the record's room in its first window, so a design-rate ten-year stream a quarter back
counts an eighth of its value there rather than a tenth, and seven such streams do not fit in one window; and arrears
older than a quarter need a separate award, which counts in the window it is made. The
[module README](../../x/disbursement/README.md) and the [plan](../governance/DISBURSEMENT_PLAN.md) carry the rule.

## D123

Disbursement's compensation figures rebase with the protocol reference ([D23](#d23), [D76](#d76)): the cap, every
compensation award's value and charge, the live term's usage, and its allowance, in the same `MsgSetReferenceDenom`
transaction as Market's pool and Treasury's reference state, or the transaction fails whole. Claims' committee claim
limit, every committee claim's charge, and the live term's usage rebase the same way ([D140](#d140)). All are
quantities, so each converts as `Convert(from → to)` and rounds up as the award-time valuation does; the usage is
re-derived as the sum of the live term's rebased charges rather than converted, so the import equality between the two holds exactly; the allowance is then
raised to the usage where the per-award ceilings outran its own ceiling, by under one base unit an award. A figure past
the module's 128-bit amount bound refuses the re-point at the write rather than the next export. Oracle dispatches
through one `ReferenceRebaser` contract and a variadic `SetReferenceRebasers` registration in the app, mirroring the
feed-removal guards, so a new consumer is one method and one name on one line. Why: the figures are bare Ints in the
unit the reference names at the time they are written, so without a rebase a re-point would leave new awards, valued in
the new unit, charged against old-unit records and bounds, loosening or tightening the cap and the allowance by the
cross rate. Rejected: per-consumer executors in Oracle, each with its own interface, keeper field, setter slot, nil
check, call, and mock; stranding the allowance until reappointment as Market strands its corridor, since the allowance
is a value budget whose meaning survives the move, not a depth corridor governance should re-size, and stranding would
force a grants-committee reappointment into every re-point; a stored unit on Disbursement's params, which is another
guarded copy of the reference; floor rounding of the charges, which can take a dust award to the zero a compensation
value may not hold; and depinject self-registration of consumers, which saves the app line at the cost of a second
wiring convention beside the hand-wired reverse dependencies. What it costs: a consumer left off the wiring line rebases
nothing and fails nothing, so the real-app re-point tests require every consumer's event; and the clamp lets a term's
charges stand a few base units past a strict conversion of its allowance.

## D124

The reward targets are one share a validator, the founder's included. Assembly adds 0.0175 and 0.0075 NOAH a block a
seat, sized to fund one highly available operation, but both lanes pay by bonded stake: Distribution by power, the
Oracle by power times rewarded targets. With the founder's seat at half the other seats' sum ([D112](#d112)), the pot of
131,400 NOAH a year a seat pays the founder's 22.5M a third of it: at ten seats each other seat earns about 99,000 with
its commission on the founder's delegation, about 11,300 USD a month at NOAH's opening price of one XDR ([D128](#d128)),
and the founder about 421,000. Why: the targets fund operations, and the founder runs one node. Rejected: weighting a
seat's share by its stake, 13.5 shares at ten, which pays every other seat 131,400 but raises the pot 35%, shortens the
subsidy's runway by a quarter, and pays the founder about 568,000 a year of subsidy for weight the plan calls consensus,
not pay. No rule raises the other seats to the sized share without paying the founder more, since bonded stake earns pro
rata. What it costs: each other operator runs on about three quarters of the sized budget, and the economic committee's
corridor, up to twice the launch targets, is the margin if that proves short. The [launch
genesis](../governance/GENESIS.md) and the [disbursement plan](../governance/DISBURSEMENT_PLAN.md) carry the figures.

## D125

The subsidy pool is seeded with 50M NOAH, and the disbursement pools hold 782M at 60/40: the member pool 469.2M and the
contributor pool 312.8M. Why: the targets follow the founding seats, not the set, so a 100M seed would last 76 years
at ten seats, past any horizon a bootstrap serves. 50M runs 38 years at the launch targets, 19 at the economic
committee's doubled ceiling, and about 13 if governance funds thirty validators at the seat rate; a shortfall is
refilled by any NOAH deposit, a community-pool spend included ([D25](#d25)), and an excess returns by
`MsgReturnSubsidy` ([D83](#d83)). The NOAH not seeded into the subsidy sits in the pools because the subsidy pool is
restricted money and the launch electorate is the founders: the community pool would make it spendable on anything by
that electorate, while the member pool, which has no exit, hands it to members. The 60/40 split is the one the plan
argues from; step 5 is 782M less steps 1 to 4, about 66,300 members at its amount. What it costs: the runway is half a
100M seed's at every target level, so the plan's first lever, raising the targets to draw public stake, spends a
smaller pool; members hold 46.9% of supply. The [launch genesis](../governance/GENESIS.md), the
[disbursement plan](../governance/DISBURSEMENT_PLAN.md), and the whitepaper carry the figures.

## D126

Every grant's first period is at least two weeks, `MinFirstPeriod`, so nothing pays before two weeks after any award: a
member's first payment waits out a two-week hold, `member_schedule.first_length` 1,209,600 seconds at launch, and pays
from the block when it ends like every other grant's first period, so registration pays nothing; `compensation_period`
is floored at two weeks, its launch value; and the ownership cliff may never fall under it. `MinFirstPeriod` is checked
on every grant and every shape in params, so it is an invariant of stored state, not a launch setting. Governance opens
each step's member budget in quarters, twenty tranche votes over the distribution: 7.5M at step 1, then 15M, 22.5M,
22.5M, and 49.8M. Why: the open tranche is the registrar's only bound ([D92](#d92)), and a tenth paid at registration
would let a stolen key take a tenth of whatever is open within minutes, defended only by tranches small enough to
lose, at a vote each. With the hold the committee suspends what a stolen key registered inside it, rotates the key, and
governance cancels the grants whole, so the tranche bounds what a thief commits rather than what it takes; a stolen
committee key's registrar is answered by the replacement vote, two days and an expedited day, inside the same
fortnight. Every grant pays nothing before one period after its award ([D116](#d116)); one floor for every kind makes
that window a property of the module rather than of each parameter. With the hold the tranche is a backstop behind the
daily check of registrations against admissions, so its size is set by the vote load it costs: a quarter of a step
keeps what an undetected theft could take to a tenth of the quarter, 750k at step 1, at about four votes a year at the
plan's pace. Rejected: tranches sized to an accepted loss alone, about a hundred votes over the distribution at 5M
each, each needing half the bonded stake to turn out; monthly tranches, about sixty votes over the distribution, which
train validators to pass them unread; a one-week hold, which leaves a stolen committee key's registrar under a week of
slack after two votes; a hold without a floor, which governance could set to a second; a rolling registration limit
([D92](#d92)). What it costs: a member waits two weeks for the first coins and cannot pay gas until then, since the
module grants no fee allowance, and the member grant takes 54 weeks; the testnet cannot compress the hold, so neither it
nor the end-to-end suites see a member paid, and in-process tests carry that path; raising the floor in a later binary
would refuse stored grants inside the block, so a raise needs a migration, while lowering it is always safe; and the
hold protects only if someone checks the chain's registrations against the platform's admission records inside it,
which each batch's reference makes possible. Sybils the member process admits are unaffected: their tenth pays after
the hold unless found inside it. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md), the
[module README](../../x/disbursement/README.md), and the
[governance runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) carry the floor, the hold, and the tranche
sizing.

## D127

Contributor ownership grants are sized by a written case under the cap, with no scale: no bands and no multipliers. The
founder's 15M grant for the work before launch is `ownership_cap` ([D115](#d115)), so no record is paid more ownership
than the founder, and every grant under it is argued case by case: its proposal summary states the work, its period,
and why this amount, against the founder's grant and the grants already made, and the grantee abstains. Earlier and
riskier work may earn more than the same work later, said in the amount rather than through a multiplier. Why: band
ranges are invented, a fivefold spread or a timing multiplier of two to three that nothing measures, so they lend a
rigour they do not have; a band anchors every case to its top, and a band table topping at the founder's own grant
reads as a rubric built back from one number; and the chain checks none of it, so the precedent the first review round
sets is the real scale. While the founding set votes its own members' grants, the written case is what carries
legitimacy. Considered and not taken: a ceiling on `ownership_cap` in params validation at the founder's grant, which
would make "never more than the founder" a rule no vote could lift; the cap is a parameter governance sets. What it
costs: no number to place a grant against before the first round, so that round's cases carry the whole weight of
precedent; and "never more than the founder" holds while governance keeps the cap at 15M, binds a record rather than a
person, and leaves compensation outside it, at most 1M in the reference unit a year a record. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) and the
[governance runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) carry the case requirement.

## D128

NOAH opens at one XDR. `pkg/chain/launch.go` holds the bootstrap pair, one NOAH a XDR and the 1.371 dollar/XDR cross;
Treasury's genesis gas factor is one NOAH per XDR, and the sidecar's `USD/NOAH` bootstrap leg is the same price in
dollars, 0.729394602479941648 NOAH per dollar, which times the cross rounds up to exactly one. Why: NOAH is the neutral
unit every currency is a claim on, and the protocol references XDR, so its opening price belongs in XDR rather than in
one member currency; it also makes the reference-unit figures read as NOAH at the opening price, so the compensation
cap's 1M a year is 1M NOAH. Rejected: an opening price of one dollar, which prices NOAH in one member currency; an exact
anchor that routes every default feed through XDR/USD to an XDR/NOAH bootstrap, which keeps NOAH at exactly one XDR
until it trades but makes all eleven rates depend on the XDR/USD sample, where only the XDR feed does otherwise; and the
resolver averages every route that resolves, so the anchor cannot sit beside the dollar route as a fallback. What it
costs: until NOAH trades, the bootstrap is a fixed dollar leg, so NOAH's XDR price drifts with XDR/USD by a few
percent, and fees and the valuation of any NOAH compensation award drift with it. At the opening price a seat's target
is about 15,000 USD a month, a regular seat's income about 11,300, the governance deposits about 1,400 and 6,900 USD,
a step-1 member grant about 27,400, and gas about 0.02 NOAH a transfer, since its price is set in XDR. Conversion is
closed at launch ([D95](#d95)), so no currency is issued at the bootstrap price. The
[launch genesis](../governance/GENESIS.md), the whitepaper, and the
[price-feed runbook](../operations/PRICEFEED_OPERATIONS.md) carry the price.

## D129

The founder's work after launch is paid by a compensation stream at the cap: 10M NOAH over 260 fortnights, ten years, 1M
a year, a governance award written at genesis beside the founder's 15M grant ([D136](#d136)). The figure is what the
cap, 1M in the reference unit a window, admits one record over the longest award at NOAH's opening price of one XDR
([D128](#d128)), and it is fixed in NOAH. It pays for the founder's work, never the hours seat income pays, and fills
the founder's compensation cap for its ten years. `compensation_window` is 26 fortnights, 31,449,600 seconds, so the
cap's year is the pay cadence's: the longest stream charges exactly a tenth of its value a window, where a 365-day
window against a 364-day fortnight year would cap it at 9.97M. Pay streams do not wait for a NOAH market outside the
chain: a hire's stream pays in NOAH from launch, and taking pay that cannot yet be converted is the hire's choice;
stablecoin pay waits for conversion ([D95](#d95)). Why: the grant pays for the work before launch and the seat is
consensus weight, so nothing else pays the founder's work from launch on; fixing the amount in NOAH keeps it from
growing as NOAH's price falls, where the cap's whole room at a tenth of the opening price would be 100M NOAH; and a
market rule would bound nothing, since the cap, the committee's allowance, and the two-week first period bound NOAH pay
whatever it can be spent on. Rejected: maxing the cap at the price on the day of the award, which grows the stream as
NOAH falls. What it costs: the stream is ownership-scale NOAH under the compensation label, the case the [threat
model](THREAT_MODEL.md) names, so the plan counts it as the founder's stake: seat, grant, and stream block alone until
public bonded stake reaches twice what grant and stream have paid, 50M once both are paid; the founder is the largest
wallet until bonded stake passes ten times seat, grant, and stream together, 475M at ten; at the 20% planning rate the
founder holds about a fifth of bonded stake and members lead the founding set only narrowly, 38.5% to 37.9%; no other
compensation reaches the founder's record for ten years; and step 1's contributor spend runs to 25M against its 20M
guideline ([D131](#d131)). Before NOAH trades the cap values NOAH at the bootstrap price, so it bounds every record's
NOAH pay at about 1M NOAH a window whatever NOAH is worth, committee awards included. The [disbursement
plan](../governance/DISBURSEMENT_PLAN.md), the [threat model](THREAT_MODEL.md), the [launch
genesis](../governance/GENESIS.md), and the whitepaper carry the stream.

## D130

Every disbursement duration is whole weeks, a year being 26 fortnights, 364 days, the pay cadence's. The ownership
cliff is 26 fortnights, 31,449,600 seconds, so the ownership shape's 104 weights are one a fortnight and the grant runs
exactly 104 fortnights, 1,456 days. `MaxCompensationDuration` is 260 fortnights, 3,640 days, so the longest award at the
launch period is the bound itself and exactly ten windows. `MaxCompensationBackdate` is 13 weeks, 91 days, a quarter of
the year. A member's balance pays in thirteen four-week periods, 2,419,200 seconds each, weighted 13 for the tenth then
9 each of 130, so the tenth holds and the year after the hold is 52 weeks. Nothing on chain is tied to calendar dates:
every clock runs from its own award and the compensation window rolls, so the 364-day year accumulates no drift against
anything the chain checks. Why: a duration that is not a whole number of days, such as a 30.4-day month, is a unit the
fortnight cadence does not divide; one grid makes every schedule a count of the cadence, keeps each payment at its
award's time of day, and turns the bounds into definitions rather than remainders of a division. Outside the grid:
`chain.BlocksPerYear`, 365 days of blocks, used only for caps, where block time strays by more than a day a year and
365 is the convention; Treasury's volatility annualisation, a 365-day year of seconds ([D133](#d133)); seat vesting,
four and eight calendar years after genesis, continuous and with no cadence; and `MaxPeriodLength`, an overflow bound.
What it costs: the year is about 1.24 days short of the calendar's, so a ten-year stream ends about twelve days before
the tenth anniversary of its award and a four-year grant about five, and 1M a year admits about 0.34% more a calendar
year; an agreement off chain states pay a fortnight rather than a year. A member is paid fourteen times, each payment
after the tenth 6.9% of the grant.

## D131

Each step's contributor budget, 40% of the step's distribution, is a guideline rather than a limit. A review round aims
to stay near it; it may run over when the work warrants, stating the overrun and the reason in its summary, and later
rounds carry less, so contributors stay roughly in step with members and 60/40 holds over the whole distribution. Why:
the chain does not record the budget ([D117](#d117)), and a strict wait would hold back an award the plan has already
decided, the founder's grant and stream ([D129](#d129)), which take step 1 to 25M against 20M, over an accounting line.
The budget's real job is a published pace for awards while the founding set is the only voter. What it costs: only
governance limits a run of overruns, and at launch that is the founding set; the step table and each proposal summary
make an overrun visible.

## D132

The founder's grant and stream count out of public bonded stake, and each gate is 15% of the step's distribution less
them. Public bonded stake is bonded stake less the genesis seats and less what the founder's 15M grant and 10M stream
have paid, counted as bonded since the founder bonds everything; the disbursement journal shows the paid figure. The
gates before steps two to five are 3.75M, 18.75M, 41.25M and 63.75M. Why: the gates measure public participation, and
counting the founder's own payments would let them meet a gate: at 20% member staking, with step 1's members
registering over the first year, a step-two gate of 15% of the full distribution, 7.5M, would be met about the month the
founder's cliff pays, more than half of it the founder's grant and stream. The handover figures of the
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) count them as the founder's ([D129](#d129)). Excluding them
from the stake alone would not do: the founder's 25M takes step 1's contributor share, so step 1's public distribution
is the members' 30M, whose 20% fully paid is 6M, under 7.5M. Netting the founder out of the base keeps each gate three
quarters of the planning rate on what the public received, and step two opens about month fourteen on members' stake.
Other seat holders' grants stay public, since the seats are independent ([D112](#d112)). What it costs: each gate is
3.75M under 15% of the step's full distribution, so a step opens on less public stake; the step-5 gate sits under the
seats' 67.5M and does not mark where public stake passes them, though the founder plus five seats still block at it,
47.5M of 131.25M; and reading a gate needs the journal beside the staking pool.

## D133

Every window a governance vote must fit is measured in seconds of block time, the clock the vote itself runs on, and
per-block processes stay in blocks. The Claims cancellation period is `claim_cancellation_period`, 604,800 seconds, with
claims queued by closing time; the settlement activation delay is `settlement_activation_delay` ([D134](#d134)), with
plans carrying activation, earliest closing, and opening times; and the shared committee envelope carries
`activation_time` and `expiry_time`, Unix seconds, for all eight appointments. Treasury's annualised volatility divides
its EWMA of squared returns, sampled at least `volatility_sample_seconds` apart, by an EWMA of the seconds each return
spanned, under the same retention, and
scales by a 365-day year of seconds: variance per second over one window. In blocks, as per-block processes: the oracle
reward and attendance windows, Treasury's per-block reward, funding window, and refresh cadence, Market's pool
recovery, vote-extension activation heights, the Security committee's upgrade height, which `x/upgrade` counts in
heights, and recorded heights. Why: block time is node-local, set by each validator's `timeout_commit`, and CometBFT
gives the app no say over it, so a block count promising to outlast a vote holds only while blocks stay near six
seconds: a three-day delay in blocks falls inside the two-day vote below four-second blocks, and a week of claims misses
a deposit period and vote below about 3.4. Committee terms are dates people plan reappointment around, and a year of
blocks runs from about 335 to 395 days across plausible paces, moving whenever validators retune. A per-block volatility
constant reads about 9% low at five-second blocks and 4% high at 6.5, and takes the first return after a halt as one
block's move, so a one-day halt would lift the reading about 31% for a day. The time-weighted fold corrects both, reads
correctly from its second sample rather than warming up, and keeps the bound a theorem, since each squared return is
at most one and each interval counts at least a second. What it costs: block time runs through an outage as the vote
does, so a halt takes time from the veto and cancel windows that no one could use; a coalition able to halt the chain
already holds a third of the vote and every veto with it, so it gains nothing, and the relaunch runbook has operators
decide whether to push pending closing and activation times forward by an outage.

## D134

The settlement activation delay is a week, `settlement_activation_delay` 604,800 seconds, the Claims cancellation
period; the testnet's is four hours, as Claims'. Why: the clock starts when the opening vote passes, so the delay has to
absorb noticing the mistake and any deposit period as well as the cancel vote; three days leaves one spare day over the
two-day vote, and a cancel opened at the 10% initial deposit could run four days and land after activation. A week fits
the full deposit period and a vote with three days to notice, and both correction windows rest on one rule. What it
costs: holders of a suspended asset wait a week before redemption opens. The delay is read at opening, so a change never
reaches an open plan.

## D135

The Oracle reward distribution window is four weeks, 403,200 blocks; the testnet's is four days of daily windows, the
same quarter of the pool per settlement. Why: Treasury tops the Oracle's account up to its target from the subsidy every
funding window, so oracle income has a floor while the subsidy lasts, and with conversion closed at launch there is no
tax above it. The window does not protect voters from a drought; it only spreads tax windfalls above the target. Four
weeks pays 77% of the target in the first quarter and 94% in the first year and still damps weekly windfall noise about
2.6 times, where a quarter would pay 40% and 77% for smoothing nothing yet needs. Revisit with the [D72](#d72) weights
once tax is measured. What it costs: a tax windfall reaches voters over a month, and the pool's cushion against a quiet
tax week is a month of income.

## D136

The founder's 15M grant and 10M stream are written into genesis as governance awards. Seat assembly,
`arkd genesis add-validator-seats`, adds both to the founder's beneficiary record beside the seat: grant 1 an ownership
grant at the cap on the ownership schedule, grant 2 a compensation stream of 260 fortnights valued at one XDR a NOAH
([D128](#d128)), both reserved in the contributor pool, created by the governance authority at the first block, and
clocked from `genesis_time`, so the stream first pays two weeks in and the grant a year in. Why: every founding
validator signs a gentx against the published file, so the founding set approves the awards unanimously and before
launch, where a first-round vote with the founder abstaining would need two thirds of the other seats after launch; and
the founder waits on no vote, the assurance [D112](#d112) gives for making the seat genesis state. The bootstrap
valuation fixes the stream at exactly 10M, where an award voted later would take whatever the cap admitted at the day's
XDR. Governance keeps the cancel: one proposal returns either grant's unearned principal to the pool, so the founding
set's approval becomes the public's standing veto. Rejected: genesis vesting accounts, which pay the same coins but
leave behind the cap, the journal [D132](#d132) reads, the cancel, and controller rotation; a flag, since the awards are
policy fixed in `pkg/chain` like the seat. What it costs: both clocks start at genesis, earlier than a first-round vote
would start them; the written case [D127](#d127) asks of every grant lives in the plan rather than a proposal summary,
each grant's reference pointing to it; a validator who objects to either must refuse the genesis or later propose the
cancel; and disbursement import admits a stamp at height one though InitChain runs at height zero, as Oracle's does.
The [disbursement plan](../governance/DISBURSEMENT_PLAN.md), [launch genesis](../governance/GENESIS.md),
[module README](../../x/disbursement/README.md), [threat model](THREAT_MODEL.md), and whitepaper carry the path.

## D137

A proposer leaves user transactions out of a block whose prices are stale by its own clock, and `max_exchange_rate_age`
is floored at 30 seconds. A block applies the prices attached to the previous commit and carries that commit's median
time, so after a stall or a halt both predate the outage while its transactions are chosen after it: a swap sent during
a two-hour upgrade would trade on prices two hours old that the age check reads as new, since nothing in the block
records the outage. A proposer whose clock finds the proposal's time older than `max_exchange_rate_age` proposes
commit-only, and waiting transactions trade in the next block on prices observed after the outage. The floor keeps the
rule off ordinary blocks, about five seconds behind the clock: under that gap every block would be empty, the vote to
undo it included. Why: currency crosses carry only the 0.25% Tobin spread and no pool, so a move over a halt larger than
that would be free to take at the protocol's expense, and conversion opens by vote, so the guard has to precede it.
Rejected: settling orders on the next block's prices, which closes the gap deterministically but ends
single-transaction swaps, buy-and-send payments, contract use of a fill, and award-time funding; re-checking in
`ProcessProposal`, which would make acceptance depend on validator clocks, so a third of stake running fast would stop
every transaction; a wider spread, which charges every trade for a rare event; proposer-based timestamps, absent from
the pinned CometBFT. What it costs: a stall or halt costs one empty block; the rule binds honest proposers alone, so a
validator proposing the first block after an outage can still trade it, on chain and attributably; and a proposer's
clock decides whether its block carries transactions.

## D138

`reference_tax_cap` launches at 100 XDR, and each `MsgMultiSend` output is taxed as its own principal with its own cap.
Why: the tax sees only the Bank ledger, so a ledger built on it, a contract's balances, an exchange, a custodian, moves
value between its users untaxed, and what a large mover saves by leaving the rail is the cap per move. Taking a
contract's risk at 1% a year, a 1,000 XDR cap would make a 100,000 XDR balance moved twice a year pay for wrapping; at
100 it needs ten moves, and a 1M balance a hundred. The chain mints nothing, so after the subsidy tax and gas are the
validators' pay and the cap cannot fall far: under a heavy-tailed size mix 10 XDR collects about a third of what 100
does, while under a retail mix the cap barely matters. Above 100 the extra comes only from transfers over 20,000 XDR,
the movers best placed to leave, so it is the least reliable share of that pay. An input capped once would let one
batch share a single cap however many payouts it carries; per output a batch pays what its sends would, and the SDK's
single input makes the outputs the whole principal. Rejected: refusing registered currencies to contracts outside a
governance code list, which takes them out of the open runtime and still needs every listed contract's claims, LP and
vault shares included, to be untransferable; 10 XDR; a 1,000 XDR cap; a cap controller, which [D79](#d79) rejects.
Revisit once conversion opens: if the share of each currency held by contract addresses stays small, large movers are
staying and a higher cap can be tried, at the cost of refusing the large transfers in flight at the change
([D80](#d80)). What it costs: a large transfer pays at most 100 XDR, and a payout batch pays per recipient. Nothing is
taxed before conversion opens.

## D139

Emergency suspension travels as an ordinary committee transaction, kept private until a carrier proposes it by the
[emergency submission runbook](../governance/EMERGENCY_SUBMISSION_RUNBOOK.md); no vote-extension transport carries
committee orders. Why: suspension answers a failure the market can already see, a breaking feed source, a reference
event, a discount on the asset, so arbitrage is running before the committee convenes, and the virtual pool
([D3](#d3)) caps what leaves each block at the recovery of an imbalance the discount and the depth set, however many
race for it. Hiding the transaction moves none of that, and the wait it would shorten, 30 to 60 seconds for a carrier's
slot at the roster's 10 to 20% of stake, is small beside hours of detection and signing. A dark or stale feed closes
conversion by itself, and no holder can change an asset's status, so front-running cannot make a suspension fail.
Rejected: committee-signed orders in vote extensions, executed in the next PreBlock, which land one block after
signing whoever proposes but add a permanent PreBlock execution path, a carrier listener, a coordinated upgrade, and a
carrier set holding over a third of stake for the guarantee. Revisit only if a mandate is expected to suspend ahead of
public information, where the transaction would be the first signal. What it costs: a suspension waits for a carrier's
proposer slot, and one sent publicly is visible for a block and can be left out by a hostile proposer.

## D140

Claims pay in NOAH or any active registry asset. A stablecoin claim is funded when it is submitted, as a compensation
award is: it draws uncommitted paper in Insurance custody first and buys the rest through Market's ordinary buy with
uncommitted Insurance NOAH, inside the submitting message and within the claim's `max_offer`; settlement only sends.
The committee claim limit is in the reference unit, and both committee allowances, it and the grants committee's
`compensation_allowance` ([D110](#d110)), charge what a committee award drew rather than its face value: its custody and
the NOAH it converted, valued at the fresh rates Market quoted with, each rounded up, and written on the record as its
`charge`. Every other check runs before the buy, the allowance at face value, which a buy can only raise. A governance
claim charges nothing and reads no rate. A cancelled or failed claim's paper stays uncommitted for the next claim in its
denomination; a claim pays even if its asset is suspended after submission; Insurance admits NOAH and registry members
from any sender ([D26](#d26)) and counts NOAH alone ([D28](#d28)). Why: a converted claim's NOAH is spent at
submission, so cancellation, which makes a stolen key's NOAH claims harmless, cannot undo it, and Market's buy prices on
a constant-product pool, where buying 90% of its depth costs ten times the value bought; a limit on face value would
bound nothing a stolen key burns through the spread. Converting at payout would put a buy in the Claims EndBlocker,
after Market's block settlement ([D33](#d33)), fail an approved claim on a stale price, and leave a NOAH reservation
unable to cover a fixed stablecoin amount after a week of price movement. Rejected: payout-time conversion, as above; a
NOAH-denominated limit, which floats with NOAH's price; selling a cancelled claim's paper back to NOAH on cancel, which
would make the veto depend on Market; and admitting paper only from Market's mint, a sender exemption [D26](#d26)
refuses. What it costs: a committee claim, a NOAH one included, needs a fresh reference rate, so through an oracle
outage only governance submits claims, which Insurance does not cover; a cancelled conversion's spread stays spent; a
claim near the pool's depth is ruinous or refused until the pool is resized; and paper in Insurance custody earns no
credit, so Treasury refills NOAH behind it until a claim in its denomination draws it.

## P1

The launch values, the transfer tax rate, `reference_tax_cap`, the three target ratios, the subsidy seed, the genesis
fund balances, and the [D72](#d72) exposure weights, are recorded with their reasoning in
[launch genesis](../governance/GENESIS.md). The subsidy seed is 50M ([D125](#d125)) and the reference tax cap 100 XDR
([D138](#d138)); the exposure weights are zero at launch, which keeps `m` at one and the targets unscaled.

## P2

Ark has no residual-mint limiter. Why: under coverage-based Buffer funding ([D4](#d4)) there is no recipient-output,
fixed-price cycle, or split residual-mint amplification for one to bound.

## P3

A reference re-point applies the pool amount derived from one fresh Oracle snapshot
([economic design §3.4](ECONOMIC_DESIGN.md#34-the-pool-unit)): `MsgSetReferenceDenom` carries no expected
amount and nothing compares one against a rejection threshold, and Market emits the old and applied pool state in
`EventPoolUpdated` for audit.

## P4

No committee is appointed in genesis. Appointments, their timing and key shapes, and the committee corridors follow
[launch genesis §13](../governance/GENESIS.md#13-committee-appointments): the economic-policy committee's corridor, the
Claims committee's multisig, its gross term limit and an appointment spanning at least the one-week
`claim_cancellation_period`, and committee accounts funded from committee members' floats, never from Insurance or the
Reserve. Why: the message path is what observes and records a committee's key shape, the multisig accounts must exist
and hold their own gas first, and appointments follow incident exposure rather than chain age.
