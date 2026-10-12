# Launch Genesis

- Status: decided. The artefact is complete except for what assembly adds: the validator seats and their
  beneficiary records, the seat-scaled reward targets, the founder's grant and stream, and the genesis time.
- Conversion is closed at launch: the artefact registers no asset and Market's depth is 100,000 XDR. The testnet
  artefact registers the currencies and runs a deeper pool, so a testnet converts from genesis (§15).
- Artefact: `app/genesis/genesis.json`, data rather than a generator. `app/genesis_test.go` is its build check: it
  validates the file with the manager `arkd genesis validate` uses, boots a chain from it two ways, and pins the
  values §14 lists.

Every table reads Setting, Value, Status, Why. Status is one of **Decided** (an explicit launch choice), **Default**
(the module or SDK default, kept deliberately), or **Assembly** (set when the final file is built, §12).

Block counts assume the chain's six-second block: 600 blocks an hour, 14,400 a day, 100,800 a week, 5,256,000 a year
(`pkg/chain/blocks.go`, and the 5 s commit timeout in `cmd/arkd/cmd/config.go`). Windows a governance vote must fit,
the Claims cancellation period, the settlement activation delay, and committee terms, are seconds of block time, as the
vote is, so block pace cannot shorten them. Dollar figures assume NOAH's
opening price of one XDR, 1.371 USD at the bootstrap cross, which the artefact's NOAH factor encodes (§4).

For launch coordinators assembling the final genesis from the curated artefact. This document is not a specification
of every `arkd init` default.

## Contents

- [1. Rules the artefact follows](#1-rules-the-artefact-follows)
- [2. Chain and consensus](#2-chain-and-consensus)
- [3. Accounts, supply, and validator seats](#3-accounts-supply-and-validator-seats)
- [4. Treasury](#4-treasury)
- [5. Market](#5-market)
- [6. Claims](#6-claims)
- [7. Reserve](#7-reserve)
- [8. Oracle](#8-oracle)
- [9. Asset registry](#9-asset-registry)
- [10. Cosmos SDK modules](#10-cosmos-sdk-modules)
- [11. CosmWasm](#11-cosmwasm)
- [12. Assembly sequence](#12-assembly-sequence)
- [13. Committee appointments](#13-committee-appointments)
- [14. Review before launch](#14-review-before-launch)
- [15. Testnet artefact](#15-testnet-artefact)

## 1. Rules the artefact follows

Ark launches from a clean genesis. No Terra Classic state, wire compatibility, or historical module-account
balance survives, so the artefact and the code obey these rules rather than a migration:

- No module registers a state migrator for launch; Treasury's consensus version is 1. No frozen legacy types,
  converters, reserved tags, store-prefix shims, or upgrade handler exist, and any that appear are removed.
- `x/mint` is absent from configuration and genesis: no store, query service, module account, or module-version entry.
The launch supply is the native supply for the life of the chain: only conversion mints NOAH, and supply
  falls only through burns, by conversion settlement, authorised Reserve burns, slashing, vetoed governance deposits,
  and contracts burning coins they hold, the module accounts holding `Burner` (§3).
- The subsidy pool, Redemption Buffer, strategic Reserve, Insurance, disbursement pools, and community pool seeds are
  bank balances, never minted by a module's InitGenesis. The community pool's fee-pool entry equals the Distribution
  module balance, which Distribution's InitGenesis requires.
- Distribution's `community_tax` is zero in the artefact itself; `arkd init` still emits the SDK default, so a
  genesis built from `arkd init` must set it.
- The four custody accounts are unblocked for inbound sends. The subsidy pool and Buffer admit only positive `anoah`,
and the Reserve and Insurance `anoah` and registry members; each owner's InitGenesis runs after
  Bank's and re-checks its own balances.
- Bank supply equals the sum of balances at every step: the artefact carries the disbursement pools and the community
  pool before seats, and assembly moves each seat's grant out of the community pool rather than adding supply.
- A relaunch takes a new chain ID and continues heights: `arkd export` refuses a zero-height export on purpose
  (`CLAUDE.md`, Export & Relaunch).

Export and import must preserve Treasury params and conversion factors, the Claims mandate, allowance used, the Insurance
reservation, every claim with its status, all fund balances, total supplies, the denomination-bearing `BasePool`, the
labelled `ArkPoolDelta`, and every governance proposal that authorised a Reserve-to-Buffer commitment or a claim. Only
the launch genesis starts with a zero `ArkPoolDelta`.

## 2. Chain and consensus

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `chain_id` | `ark-1` | Decided | A later relaunch takes a new one. |
| `genesis_time` | `0001-01-01T00:00:00Z` in the artefact | Assembly | A start gate, not a record: CometBFT waits for it, so nodes start early and the chain begins on its own. Set it before the seats are granted, which vest from it (§12). |
| `initial_height` | `1` | Decided | A fresh chain starts at one; continuation exports start at the next height. |
| `consensus.authority.authority` | the gov module address, `ark10d07y265gmmuvt4z0w9aw880jnsr700j2cu5hn` | Decided | Chain authority lives in consensus params; empty would fall back to each module's own gov string, which is the same address stated a dozen times. `arkd testnet` sets it the same way. |
| `consensus.block.max_gas` | `100000000` | Decided | The base-fee controller measures utilisation against this budget; without a finite `max_gas` its guard held the price at the floor forever ([local design](../../x/treasury/README.md#conversion-factors-and-dynamic-fees)). |
| `consensus.block.max_bytes` | `22020096` | Default | 21 MiB. A hundred validators carry at most 1.6 MB of vote extensions a block. |
| `consensus.abci.vote_extensions_enable_height` | `1` | Decided | Oracle rates arrive through vote extensions; `abci/voteextension.VoteExtensionsAvailable` treats zero as disabled. |
| `consensus.evidence.max_age_num_blocks`, `max_age_duration` | `302400`, 21 days | Decided | CometBFT expires evidence only once both bounds are exceeded. The SDK default of 100,000 blocks and 48 hours lapsed after a week at six-second blocks while stake stays bonded 21 days; both now equal the unbonding period. |
| `consensus.evidence.max_bytes` | 1 MiB | Default | SDK default. |
| `consensus.validator.pub_key_types` | `ed25519` | Default | SDK default. |
| `consensus.version.app` | `0` | Default | SDK default. |

## 3. Accounts, supply, and validator seats

Module accounts are code (`app/app_config.go`), listed here because the genesis review confirms them:

| Account | Permissions | Inbound user sends | Why |
| --- | --- | --- | --- |
| `market` | Minter, Burner | Blocked | The sole minter; mints and burns only in conversion settlement. |
| `treasury_subsidy_pool` | none | `anoah` only | Balance-constrained subsidy pool; permissionless NOAH deposits are the only refill. |
| `treasury_redemption_buffer` | none | `anoah` only | Coverage-based redemption inventory. |
| `strategic_reserve` | Burner | `anoah` and registry members | Reserve custody; the burn authority is split between committee and governance. External symbols are refused at the restriction, permanently. |
| `claims_insurance` | none | `anoah` and registry members | Insurance custody, paid only through recorded claims; registry members are what a stablecoin claim buys, and count for nothing. |
| `transfer_tax_collector` | none | Blocked | Holds the funding window's transfer tax until settlement routes it. Its send of written-off and retired residue into `strategic_reserve` meets the Reserve's ordinary NOAH-or-member admission; no sender is exempt. |
| `oracle` | none | Blocked | Oracle reward pool. |
| `fee_collector` | none | Blocked | Gas fees before Distribution. |
| `distribution` | none | Blocked | The community pool. Governance spends it by `MsgCommunityPoolSpend`; nothing else moves it. |
| `disbursement_members` | none | open, NOAH only | The member pool and every member grant's escrow; a NOAH deposit joins the pool unopened, governance opens tranches, nothing else draws it, and it has no exit. |
| `disbursement_contributors` | none | open | The contributor pool, ownership and compensation escrow in every denomination, and deposits; a NOAH deposit joins the pool, which has no tranche, and governance may return it to the community pool. |
| `gov` | Burner | open | Holds deposits, and is the signer of every proposal-executed message. |
| `wasm`, staking pools | as the SDK and Wasmd require | Blocked | Upstream module accounts; wasm holds Burner. |

The blocked list replaces the SDK default rather than augmenting it, so every blocked account above is named
explicitly and the four protocol-fund custody accounts, both disbursement accounts, and `gov` are deliberately left out.

### Supply ledger

Total supply is 1,000,000,000 NOAH, `1000000000000000000000000000` in base units. There are no investors, backers, or
public allocation at launch: the protocol funds are seeded, the member and contributor pools are held in the
Disbursement module's two accounts, the validator seats are granted, and the remainder is the community pool, which governance spends
by vote.

| Balance | NOAH | Share | Status | Why |
| --- | --- | --- | --- | --- |
| `treasury_subsidy_pool` | 50,000,000 | 5% | Decided | The bootstrap payroll until gas and tax cover the targets: at the ten-seat launch targets it runs about 38 years, 19 at the economic committee's doubled ceiling, and under four if governance funds a hundred validators at the seat rate. Drawn by settlement; governance may raise the targets or return idle balance to the community pool through `MsgReturnSubsidy`, and any NOAH deposit refills it. A larger seed would outlast any horizon a bootstrap serves; the NOAH beyond it sits in the disbursement pools at 60/40. |
| `strategic_reserve` | 50,000,000 | 5% | Decided | Governance's intervention capacity through the first growth phase; commitments to the Buffer and surplus burns are its only exits. |
| `treasury_redemption_buffer` | 10,000,000 | 1% | Decided | Full coverage on the first 10M NOAH of liability, so early redemptions pay from inventory rather than minting. |
| `claims_insurance` | 5,000,000 | 0.5% | Decided | The only claims capacity until the waterfall reaches Insurance, which is last in line. |
| `disbursement_members` | 469,200,000 | 46.92% | Decided | 60% of the distribution. Tranches open by vote; the account has no exit, so it pays members or nobody. |
| `disbursement_contributors` | 312,800,000 | 31.28% | Decided | 40% of the distribution: ownership awards and compensation, converting NOAH for stablecoin awards. Governance may return it to the community pool. |
| `distribution` (community pool) | 103,000,000 less the seats | 10.3% | Decided | 35,000,000 after ten seats with the founder's: governance's discretionary fund, and the backstop behind the Reserve and subsidy pool. Assembly moves 5,050,000 per seat out of it, the 5,000,000 grant and the 50,000 float, and the founder's excess over one seat (§12). |

Under the defensive target ratios in §4, fund targets are zero until conversion creates liability, so the seeds are
what give the funds capacity before the first expansion.

### Validator seats

The founding validators, seven to thirteen of them, hold the same seat, granted from the community pool, and the
founder's seat is half the other seats' sum, a third of the founding stake, on the same account and schedule and from
the same pool. Entry after launch is open: a validator bonds NOAH it holds, and no proposal grants a seat.
Until distributed NOAH is bonded the founders hold every vote, a proof-of-authority trust model on
proof-of-stake machinery; the [threat model](../security/THREAT_MODEL.md#1-components-and-what-each-is-trusted-with) records it.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| Seat grant | 5,000,000 NOAH, vesting from four to eight years after genesis | Decided | A continuous vesting account whose start is four years after genesis and whose end is eight: a founding allocation a leaver keeps, its cliff covering the launch window and its tail finishing about when public bonded stake passes the seats. Unvested coins can be delegated, vote, and be slashed, never transferred, so equal power is structural through the launch years and the floats stay the only liquid NOAH a founder holds until year four. A double-sign takes 250,000 NOAH, about two and a half years of a seat's income; downtime takes 500. The founding seats are the only granted stake, 5M a seat whatever the count, the founder's beside them, 6.75% of supply at ten; later validators bond NOAH they hold. |
| Founder's seat | Half the other seats' sum, 22,500,000 NOAH at ten seats, on the seat's schedule | Decided | A third of the founding stake at any count, the founder's allocation for building the chain and the veto in the launch window, which public stake dissolves from the first delegation; all of it from the community pool like any seat. The founder's gentx self-delegates one seat and the rest is delegated across the other seats in the first blocks, so no node holds the third. Assembly accepts seven to thirteen seats: under seven the founding stake falls under 40M, over thirteen the founder's seat passes 3% of supply. The founder's 15,000,000 NOAH grant for the work before launch and 10,000,000 NOAH compensation stream for the work after it are not seat figures: seat assembly writes them as contributor awards beside the seat, approved by every gentx rather than voted (§10). |
| Seat float | 50,000 NOAH, liquid | Decided | Unvested coins cannot pay fees, so the float carries the week before seat income starts: gas at a few hundredths of a NOAH a transaction, proposal deposits of 1,000 and 5,000 refunded unless vetoed, and a committee account's funding, under 10,000 in a busy week. Income then carries everything, and a larger float only adds sellable supply on day one. Beside that income it is the only liquid NOAH a founder holds until the seat vests, and conversion is closed at launch, so the floats play no bootstrap role. |
| Per-seat reward target | 0.025 NOAH a block, 131,400 a year, split 70/30 between the validator and oracle lanes | Decided | Sized to fund one highly available operation with the price sidecar, on-call, and the governance and committee work, about 15,000 USD a month at the opening price. Both lanes pay by bonded stake, so the founder's larger seat takes a third of the pot: at ten seats each other seat earns about 99,000 a year, about 11,300 USD a month, and the founder's about 421,000, and the economic committee's corridor, up to twice the launch targets, is the margin if operators run short. The targets are floors: once tax exceeds them the oracle lane takes every residual, so the mature split is set by revenue, not by these numbers (§4). |
| `auth.params.tx_sig_limit` | `10` | Decided | The ante counts every key of every signer, a multisig's whole membership rather than its threshold, so a committee holds at most ten keys, fewer when another account signs as fee payer. The SDK default is seven (§13). |
| `auth.accounts` | empty in the artefact | Assembly | One continuous vesting account per seat, its window four to eight years after genesis time, holding the grant as its original vesting plus the float; the gentx self-delegates one seat. Committees are appointed after launch. |

After launch a validator registers with create-validator, bonding NOAH it holds at a commission at or above the
floor; no proposal is involved and nothing is drawn from the pool. The reward targets do not follow the set: they are
the founding seats' shares at assembly, and raising them as validators join is a governance policy choice by
`MsgUpdatePolicy` (§4), which the [disbursement plan](DISBURSEMENT_PLAN.md#2-validators) proposes
how to use.

## 4. Treasury

Params (governance-owned, `x/treasury/types/params.go`):

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `reference_denom` | `axdr` | Decided | The reference unit for the tax cap and gas pricing, independent of Market's pool unit; it needs a feed, not registry membership. |
| `reference_tax_cap` | 100 XDR, `100000000000000000000` | Decided | Tax is the smaller of rate times amount and the cap, so the cap sets the crossover: at 0.5% every transfer up to 20,000 XDR pays proportionally and larger ones pay a flat 100 XDR. The chain mints nothing, so after the subsidy tax and gas are the validators' pay and the cap cannot fall far; above 100 the extra comes only from movers able to leave the rail through a contract ledger or an exchange ([economic design §5.2](../economics/ECONOMIC_DESIGN.md#52-taxable-denominations-and-operations)). Revisit once conversion opens, from the share of each currency held by contracts. Lowering the cap later refuses nothing; raising it refuses in-flight large transfers. |
| `transfer_tax_rate` | `0.005` | Decided | At the top of Terra Classic's operating band, under the 2% spread floor a swap round trip would otherwise beat, and the safe direction to start from since cuts never refuse in-flight transfers. Revenue starts only once conversion has created currency supply. |
| `reward_funding_window` | `100800` | Decided | One chain week of settlement observations; the first payment lands at block 100,800, which the floats cover. |
| `exposure_refresh_period_blocks` | `600` | Default | Hourly recompute of the multiplier. |
| `volatility_decay` | `0.9975` | Default | Per-sample retention: a half-life of 277 samples, 23 hours at five-minute sampling: a regime, not an event. |
| `volatility_sample_seconds` | `300` | Default | Each return spans at least five minutes, so per-block oracle jitter is never a sample of its own: one basis point of jitter reads as about 4.6% annualised volatility rather than 32%. |
| `flow_decay` | `0.99885` | Default | Flow half-life of 603 blocks, an hour: the one signal an actor can manufacture, so it forgets fast. |
| `multiplier_cap` | `2` | Decided | The defensive ratios sum to 0.5, so full retention is reached at 2; above that only the committee bounds widen. Inert until the weights are set. |
| `multiplier_max_step` | `0.1` | Decided | With cap 2, a climb from one to the cap takes ten sustained hours. Inert until the weights are set. |
| `min_base_gas_price` | `100000000000` axdr per gas | Default | Terra Classic's posted 0.1 usdr per gas carried to eighteen decimals: 0.02 XDR per 200k-gas transfer. |
| `base_fee_target_utilisation` | `0.5` | Default | 50M gas a block, roughly 450 sends before the price moves. |
| `base_fee_adjustment_rate` | `0.025` | Default | Full blocks double the price in 28 blocks and raise it tenfold in nine minutes; empty blocks unwind at the same rate to the floor. |

Economic policy (the committee's levers once appointed, §13):

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `validator_block_reward_target` | `0` in the artefact; 0.0175 NOAH a block per seat, `17500000000000000`, added by each founding seat | Assembly | Every founding seat adds its share at assembly, so the launch target is the seat count times the share; entry after launch grants nothing, and the target then moves only by `MsgUpdatePolicy`, inside the committee corridor (§13) or by governance beyond it. Met from gas first, then tax above the oracle floor, then subsidy. |
| `oracle_block_reward_target` | `0` in the artefact; 0.0075 NOAH a block per seat, `7500000000000000`, added by each founding seat | Assembly | Protected first out of tax. The oracle lane is the only performance-weighted income, paid by band-gated accuracy, and the lane every tax residual flows to, so 30% of the floor keeps a half-accuracy operator 15% poorer without deferring most of a seat's income behind the oracle payout window (§8). |
| `redemption_buffer_target_ratio` | `0.30` | Decided | Each redemption pays 30% from inventory and mints 70%, and that share holds through a run. |
| `strategic_reserve_target_ratio` | `0.15` | Decided | With the Buffer, a 45% coverage ceiling when everything is committed, 22.5% after NOAH halves. |
| `insurance_target_ratio` | `0.05` | Decided | No loss history; a tail margin so the fund builds at all. |
| `liability_ratio_weight`, `volatility_weight`, `flow_weight` | `0` | Decided | Zero keeps the multiplier at one and the targets unscaled; set from live data after observability's calibration window. |

The ratios are the defensive set: their sum is the retention share of growth, so half of every expansion's NOAH is
retained and half burned. All three funds hold NOAH against a NOAH-valued liability, so a ratio halves when NOAH
does, and a lean set would leave single-digit coverage in the run it exists for. Targets are not custody ceilings and
a lowered target releases nothing, so starting high keeps what is built; starting low can only catch up through
expansions that do not arrive during a run.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `economic_mandate` | empty | Decided | Appointed after the first expansions (§13). |
| `conversion_factors` | `anoah` at `1`, derived height 0 | Decided | Genesis requires a positive NOAH factor so gas is payable before the first `axdr` rate; the first usable reference observation replaces it, and the refresh runs whether or not a currency is registered, so fees follow the first live NOAH price rather than waiting for registration. 1 is NOAH opening at one XDR, the neutral unit the protocol references, 1.371 USD at the bootstrap cross, and every dollar figure in this document assumes it. The sidecar's bootstrap `USD/NOAH` leg, 0.729394602479941648 NOAH per dollar, is the same price; both derive from `pkg/chain/launch.go`, so change the pair there if the opening price changes. |
| `base_gas_price` | the floor | Decided | The controller starts at `min_base_gas_price`. |
| `reward_funding`, `exposure_state` | canonical empty state | Decided | The first observation initialises the window from the param; the multiplier starts at one. |

The zero-revenue runway is `subsidy_pool / ((validator_target + oracle_target) × 5,256,000)` years, a display
estimate: fees and tax lengthen it, and nothing refills the pool but permissionless `anoah` deposits.

## 5. Market

Ark's pool is Terra's constant-product virtual pool, so the values align with Terra's original regime, not the 2022
expansions the whitepaper records as the accelerant. A single swap from a rested pool pays the floor up to about 2% of
the depth, and sustained one-way flow at the floor is about 1% of the depth a day with a one-day recovery.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `conversion_policy.base_pool` | 100,000 XDR, `100000000000000000000000` | Decided | Conversion opens by vote with no NOAH market history behind it: a swap worth about 2,000 pool units pays the floor and about 1,000 a day flows at it, enough for members converting real amounts and small enough that a pushed thin price mints little at the wrong rate. Depth is raised by the Market committee inside its corridor and by governance beyond it, sized by the [depth rule](COMMITTEE_RUNBOOK.md#sizing-depth) from what liquid NOAH is expected to convert, never by shortening recovery and never automatically. |
| `conversion_policy.min_stability_spread` | `0.02` | Default | Terra Classic's default. The transfer tax may not exceed it. |
| `conversion_policy.pool_recovery_period` | `14400` | Default | A day, Terra Classic's default; each block returns delta divided by period toward zero. |
| `ark_pool_delta` | `0` | Decided | The launch pool starts balanced, in the pool unit. |
| `params.default_tobin_tax` | `0.0025` | Decided | Terra's default; parity with the Oracle-side rates it replaced ([local design](../../x/market/README.md#conversion-policy)). |
| `tobin_tax_overrides` | none | Decided | Terra's one override was for an illiquid asset Ark does not launch. |
| `conversion_mandate` | empty | Decided | Governance moves policy until there is a reason to delegate (§13). |

## 6. Claims

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `params.claim_cancellation_period` | `604800` | Decided | One week of block time, so the veto window fits a governance deposit period and vote whatever the block pace; positive by validation, capped at a year. |
| `claims_mandate` | empty | Decided | Appointed when Insurance has a charter naming the perils it covers (§13). Governance files claims itself meanwhile. |
| `claims`, `claims_allowance_used`, `insurance_reserved`, `next_claim_id` | none, `0`, none, `1` | Decided | Zero accounting at launch. |

## 7. Reserve

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `mandate` | empty | Decided | Governance alone moves Reserve NOAH through the fixed Reserve-to-Buffer message; the committee is appointed when a deployment is approved (§13). |
| `recognition_policy` | empty | Decided | NOAH-only custody counts at par; an eligibility entry arrives with the first attested external asset. |

## 8. Oracle

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `reference_denom` | `axdr` | Decided | The protocol reference unit (`x/oracle/README.md` §1.3). |
| `feeds.denoms` | `aaud`, `acad`, `acny`, `aeur`, `agbp`, `ajpy`, `akrw`, `amxn`, `asgd`, `ausd`, `axdr` | Decided | Registration needs the asset's feed Active, so the ten currencies [registered after launch](GOVERNANCE_RUNBOOK.md#opening-conversion) print from genesis; `axdr` is the reference and is never registered. |
| `params.vote_threshold` | `0.666…7` | Default | More than two thirds of power must vote, above Terra's half. |
| `params.reward_band` | `0.02` | Default | Terra Classic's default. |
| `params.reward_window` | `100800` | Default | Weekly reward settlement. |
| `params.reward_distribution_window` | `403200` | Decided | Settlement pays out `balance × reward_window / reward_distribution_window` each week, a quarter of the pool at four weeks. The subsidy tops the pool up to its target every funding window, so smoothing only spreads tax windfalls above it: four weeks pays 77% of the target in the first quarter and 94% in the first year, where a quarter paid 40% and 77% and Terra's year 12% and 38%. Revisit with the exposure weights once tax is measured. `accounting` carries the same value. |
| `params.attendance_window` | `100800` | Default | Weekly attendance grading. With the 5% floor, a dark validator recovers by attending the last twentieth of the window. |
| `params.min_attendance_per_window` | `0.05` | Default | Deliberately lenient; the reward lane, not jailing, is the accuracy incentive. Zero switches jailing off; at most `functioning_block_threshold`, so no window can jail the whole set. |
| `params.functioning_block_threshold` | `0.5` | Default | A block grades attendance only once a majority of power priced it. Floored at a half. |
| `params.participation_threshold` | `0.2` | Default | A report must price a fifth of the targets to count. Capped at a half. |
| `params.max_exchange_rate_age` | `60` | Default | Seconds: ten blocks. Also the age past which proposers leave transactions out of a block, so floored at 30 seconds, well above the five-second gap between blocks, and capped at seven days. |
| `accounting.reward_window_start_height`, `accounting.attendance_window_start_height` | `1` | Decided | Both windows open at the first block, the default `NewAccounting` writes, so each reward settlement lands on a Treasury funding block, after the funding in the same EndBlock. Zero names a block that never runs: the first windows would end a block early, before Treasury's first funding, and the settlement would find the account empty and clear the week's weights unpaid. Oracle's InitGenesis refuses a start after the genesis block, which it reads as height one though InitChain runs at height zero. |
| `exchange_rates`, `attendance_records`, `reward_weights` | empty | Decided | State the chain builds. |

## 9. Asset registry

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `assets` | none; the ten currencies are registered after launch, once NOAH has a market price | Decided | Registering an asset is what creates a liability and admits it to conversion, and Market mints only an `ACTIVE` ask, so an empty registry closes conversion while the feeds, the placeholder NOAH leg, and the seeded gas factor keep the oracle lane and fees working ([opening conversion](GOVERNANCE_RUNBOOK.md#opening-conversion)). |
| asset `description` | "An Ark currency tracking the Australian dollar." and so on | Decided | Named per currency from the table in `pkg/chain/metadata.go`. Stored metadata is checked against the derivation at every import, so a table entry is added only before its denomination is registered and never changed after; an unnamed denomination reads "An Ark currency." |
| asset `name`, `symbol` | `ArkAUD`, `arkAUD` and so on | Decided | Derived from the denomination. |
| `params.settlement_activation_delay` | `604800` | Decided | The window in which governance can cancel a mistaken settlement plan before its terms bind. One week of block time, the Claims cancellation period, so a cancel fits a full deposit period and vote with three days left to notice the mistake, whatever the block pace. The code default moved with it. |
| `emergency_mandate` | empty | Decided | Appointed in the first governance cycle (§13). |

## 10. Cosmos SDK modules

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `staking.params.bond_denom` | `anoah` | Decided | The native token stakes (`app/config.go`). |
| `staking.params.unbonding_time` | 21 days | Default | Confirmed; evidence age matches it (§2). |
| `staking.params.max_validators` | `100` | Default | Confirmed. The seat grant times the cap is what the set holds when full (§3). |
| `staking.params.min_commission_rate` | `0.05` | Decided | A zero floor invites a commission race that an operator with other income wins; seats registering below it are refused. |
| `staking.params` (7 entries, 10,000 historical entries) | SDK defaults | Default | |
| `distribution.params.community_tax` | `0` | Decided | Nothing skims validator income; the community-pool ledger receives only rounding residue on top of its seed. |
| `distribution.params` (proposer rewards 0, withdraw address enabled) | SDK defaults | Default | |
| `distribution.fee_pool.community_pool` | 103,000,000 NOAH less the seats | Decided | Equal to the Distribution module balance (§3). |
| `gov.params.min_deposit`, `expedited_min_deposit` | 1,000 NOAH, 5,000 NOAH | Decided | About 1,400 USD at the opening price, the range established chains settle in: deposits refund unless vetoed, so the deposit is capital locked for four days, not a fee. With the 10% initial ratio, 100 NOAH lists a proposal. `app/config.go` carries the same default. |
| `gov.params.quorum`, `threshold`, `expedited_threshold` | `0.5`, `0.667`, `0.75` | Decided | The founding set is equal seats and the founder's third, and the pool it guards holds most of the supply: half the bonded stake must vote and two thirds of votes must agree. The SDK requires the expedited threshold above the regular one. |
| `gov.params.veto_threshold` | `0.334` | Default | A third of the set can block. |
| `gov.params` (2 d voting, 1 d expedited, 2 d deposit period, veto deposits burned) | SDK defaults | Default | Every governance window is sized on them: the claims veto and settlement cancel windows (a week each), the two-week first-period floor that holds new members, and the tax-raise notice. Lengthening is safe while deposit period plus voting period stays at four days or less; past that, move the two week-long windows in the same proposal. |
| `gov.constitution` | empty | Default | |
| `slashing.params` | window 10,000, 5% signed, 600 s jail, 0.01% downtime slash, 5% double-sign | Decided | The SDK default of a 100-block window at 50% jailed and slashed 1% after five minutes offline. This is the Cosmos Hub and Terra Classic production set: about 16 hours of window, and oracle attendance jailing is the sharper operational incentive. |
| `auth.params` | SDK defaults except `tx_sig_limit` 10 | Decided | See §3. |
| `bank.denom_metadata` | NOAH: base `anoah`, display `noah`, 18 decimals | Decided | The native and staking unit. |
| `bank.params` | `default_send_enabled: true`, no per-denom entries | Default | The fund rules are send restrictions in code, not bank params. |
| `mint` | absent | Decided | No routine issuance: conversion is the only path that mints NOAH. |
| `security` | empty mandate and plan | Decided | Appointed in the first governance cycle (§13). |
| `upgrade`, `evidence`, `feegrant`, `authz`, `vesting`, `genutil` | empty | Default | Gentxs are collected at assembly. |

### Native grants

Both launch artifacts include the [Disbursement module](../../x/disbursement/README.md) with 469,200,000 NOAH in its
member account and 312,800,000 NOAH in its contributor account, nothing open, and no grants, journal entries, or
beneficiary records before assembly. Member terms open at the plan's step 1 amount, 20,000 NOAH, a tenth after a
two-week hold and the balance over thirteen four-week periods; each later step's proposal lowers the amount. One
record's compensation is capped at 1M in the reference unit a year of 26 fortnights, a stream counting at its yearly
rate. The grants mandate is disabled, with no allowances and nothing used, until governance appoints one. The ownership
cap is a parameter, 15,000,000 NOAH, the most ownership one record's grants commit over every grant, checked at award;
it reads neither bonded stake nor the seat. Seat assembly adds one beneficiary record per founding operator, carrying
its seat, the founder's at half the others' sum, a record nothing reads, and the founder's two awards as governance
grants made in the first block: grant 1, 15,000,000 NOAH of ownership at the cap on the ownership schedule, and grant 2,
a 10,000,000 NOAH stream over 260 fortnights valued at one XDR a NOAH, both clocked from `genesis_time`, reserved in the
contributor account without moving it, and journaled; the founder's record commits the grant against the cap. IDs start
at one. Stablecoin compensation requires an active registry asset; the award converts contributor NOAH for what custody
lacks.

## 11. CosmWasm

The contract runtime ships open. The chain has no IBC: a contract's IBC messages, queries, and
entry points are refused, so no contract channel can exist.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `wasm.params` | upload `Everybody`, instantiate `Everybody` | Decided | The contract runtime ships open: the tax seams are in place and the accept list is populated (`docs/economics/ECONOMIC_DESIGN.md` §5.5). |

## 12. Assembly sequence

The artefact is the reviewed base; assembly adds the time, the seats with their beneficiary records and reward
shares, and the founder's two awards, and changes nothing else.

1. **Time.** Set `genesis_time` in the file to a weekday hour, in UTC, when every validator's region is awake and at
   least 48 hours after the file will be published. The seats vest from it, so it comes first; a time changed after
   seating means seating again from the artefact.
2. **Seats.** Run `arkd genesis add-validator-seats --founder <operator-address> <operator-address>...` once against
   the file, naming every genesis validator, seven to thirteen of them, and the founder among them. For each it writes
   a continuous vesting account at the operator with 5,000,000 NOAH vesting from four to eight years after `genesis_time`
   and that grant plus the 50,000 float as its balance, subtracts the same from both the Distribution balance and
   `fee_pool.community_pool`, raises both reward targets by one seat's share, and records the operator's seat on its
   beneficiary record in `disbursement.beneficiaries`, leaving supply and the disbursement pools untouched. The
   founder's account holds half the other seats' sum instead, from the same pool, and the founder's grant and stream
   are written as grants 1 and 2 on the founder's record, reserved in the contributor account (§10). It refuses a count outside the
   bounds, which `--any-count` lifts for a testnet only, up to the fourteen seats either artefact's pool funds
   (`TestArtefactPoolSeatCeiling`), an unset `genesis_time`, a file already seated, since the founding set is seated in
   one run, a founder outside the operators, an operator named twice or already present, a pool that cannot fund
   the seats, a file whose caps refuse the founder's awards, whose reference is not the XDR, or whose contributor
   account cannot back them, and writes nothing unless every seat and every edited module validates. The
   seat policy and the count bounds are fixed in `pkg/chain/launch.go`; `TestLaunchGenesisBootsVestingSeat` boots what
   it writes.
3. **Gentxs.** Each validator generates its gentx against the file with chain ID `ark-1`, self-delegating one seat,
   5,000,000 NOAH, the founder included, with commission at or above 5%. The gentx pays no fee at height zero.
4. **Collect.** Collect the gentxs; collection keeps `genesis_time`.
5. **Validate and publish.** Run `arkd genesis validate`, then `arkd genesis dry-run`, which boots the file in memory
   through InitChain and the first block and lists the bonded set. Collection and `validate` check each gentx alone, so
   a gentx the chain refuses, a commission under 5% among them, would otherwise surface only at `genesis_time`
   (`TestDryRunAssembly`). Publish the file with its SHA-256, and have every validator verify the hash before starting.
   The chain starts itself at `genesis_time` once more than two thirds of the seats are online: 7 of 10, 9 of 13.
6. **Spread.** In the first blocks the founder delegates the rest of the founder's seat across the other validators,
   in roughly equal parts, from the seat's own account; unvested coins can be delegated and the float pays the gas.
   Genesis cannot hold these delegations, since the validators exist only once the gentxs execute, so this is the one
   launch step after the first block. Until it is done the founder's node and vote are one seat of the set; after it
   the founder's vote is a third of the founding stake and no node holds more than about a seat and two fifths,
   1.39 seats at ten ([disbursement plan §2](DISBURSEMENT_PLAN.md#2-validators)).

## 13. Committee appointments

No committee is appointed in genesis. The message path is what observes and records a committee's key shape, the
multisig accounts must exist and hold their own gas first, and appointments follow incident exposure rather than chain
age ([governance runbook §3](GOVERNANCE_RUNBOOK.md#3-committees)). With half the set as quorum and a
one-day expedited vote, the deliberative committees save at most a day or two over governance itself; the two
emergency mandates buy real time.

| Mandate | Single job | Appoint | Shape |
| --- | --- | --- | --- |
| Security | Schedule or cancel its own upgrade. Never funds. | First governance cycle | 3-of-5 off-chain multisig |
| Asset emergency | Suspend an asset; one power, per-term usage tracked | First governance cycle | 3-of-5 off-chain multisig, a distinct account and where the seat count allows a distinct member set |
| Economic policy | Move the eight policy fields inside a corridor | After the first expansions; weights only after the calibration window | 4-of-7 |
| Claims | Submit and cancel claims under a gross term limit | When Insurance has a charter naming its perils | 4-of-7 |
| Conversion | Depth, recovery, and spread corridor, Tobin band | At or after the opening of conversion, with a depth corridor to about ten times the genesis depth so routine raises need no vote | 4-of-7 |
| Reserve | Deploy under an allowance, floor, and destination list | When a deployment is approved | 4-of-7 |
| Grants | Appoint the registrar, suspend members, award compensation under a term allowance | Before the first member tranche | 4-of-7 |
| Registrar | Register members within the open tranche; one power | By the grants committee under its mandate, with the first tranche; cleared with the committee | A single hardware-backed key on the member platform's signing service |

Shape and hygiene: legacy amino threshold multisigs of at most ten keys, within `tx_sig_limit`; dedicated
hardware-backed committee keys, not operator keys; each multisig registered on chain with one transaction before
appointment so the recorded shape is verified K-of-N; committee accounts funded from members' floats, never from
Insurance or the Reserve. Terms: activation at or after execution, expiry a year later, both block time in Unix seconds,
annual reappointment; a Claims appointment must span at least the one-week cancellation period.

Economic committee corridor, when appointed: floor every weight at zero until calibration so the committee cannot
switch the multiplier on; bound the reward targets between half and twice the seat-scaled launch values; bound each
ratio between half and one and a half times the defensive set, with Insurance capped at 0.10.

## 14. Review before launch

`app/genesis_test.go` pins: validity under the CLI's manager; a boot with a validator set and one funded account; a
boot from a seat the seat command's own function granted, whose gentx self-delegates the grant, with the pool reduced
by the grant, supply unchanged, and both targets at one share; the consensus authority and evidence bounds; the staking, slashing, and gov values in §10; the
supply ledger, the disbursement pools with nothing open, and the community pool equalling the Distribution balance;
the economic core of §4, §5, §8, and §9 (reference unit and tax, multiplier cap and step, zero reward targets, the
three ratios and zero weights, the NOAH factor, the conversion policy and default Tobin tax, the oracle reward windows and their start heights,
both one-week delays, and the empty registry); the open contract runtime; the absence of `mint` and of every IBC
module section; and the vote-extension enable height. The rest of those sections, among them the feed list, the
oracle thresholds and rate age, Treasury's decay and base-fee parameters, the Tobin overrides, and the empty mandates,
is confirmed by review.

Confirm by hand at assembly:

- Each seat's original vesting is 5,000,000 NOAH and the founder's half the other seats' sum, its `start_time` and
  `end_time` four and eight years after the file's `genesis_time`, its balance that plus 50,000, and the pool and
  fee-pool entries reduced by exactly the seats' total; supply is still 1,000,000,000 NOAH.
- Both reward targets equal the seat count times the per-seat share, which the seat command guarantees when nothing
  else has edited them.
- The disbursement state holds exactly two grants, both the founder's: grant 1, 15,000,000 NOAH of ownership, and grant
  2, 10,000,000 NOAH over 260 fortnights, both created at height 1 and `genesis_time` by the governance authority, the
  contributor book reserving 25,000,000 NOAH, and the contributor balance still 312,800,000 NOAH.
- `arkd genesis dry-run` passes and lists every validator at one seat, 5,000,000 NOAH, the founder's too; it refuses a
  commission under 5% itself.
- The NOAH factor still reflects the opening price.
- The chain ID and the genesis time are final and the published hash matches.

## 15. Testnet artefact

`app/genesis/testnet.json`, chain ID `ark-testnet-1`, is the launch artefact with time compressed and conversion
open from genesis: the same supply, seats, economics, slashing, oracle thresholds, and open contract runtime.
`TestTestnetGenesisDerivesFromLaunch` applies the list below to the launch file and requires equality module by
module, so the two cannot drift, and the validity and both boot tests run over both files. Seats are granted with the
same command (§12); faucet and test accounts are added with `arkd genesis add-genesis-account`, and a testnet supply
above a billion is acceptable. The Docker localnet and the e2e suites run every chain on this artefact
([localnet](../../contrib/localnet/README.md), [e2e](../../tests/e2e/README.md)).

| Setting | Launch | Testnet | Why |
| --- | --- | --- | --- |
| `chain_id` | `ark-1` | `ark-testnet-1` | |
| `gov.params` voting, expedited, deposit period | 2 d, 1 d, 2 d | 1 h, 30 min, 1 h | A proposal a day, with time to gather a small set's votes. The deposits stay at 1,000 and 5,000 NOAH. |
| `asset.params.settlement_activation_delay` | `604800` | `14400`, 4 h | A correction window that fits a vote plus its deposit period, as Claims. |
| `claims.params.claim_cancellation_period` | `604800` | `14400`, 4 h | A veto window that fits a vote plus its deposit period. |
| `staking.params.unbonding_time` | 21 d | 1 d | An unbonding completes inside a test day. |
| `consensus.evidence` age | `302400`, 21 d | `14400`, 1 d | Equals unbonding, the launch rule. |
| `treasury.params.reward_funding_window` | `100800` | `14400` | Daily settlement, so subsidy and tax flows are observable. |
| `oracle` `reward_window`, `attendance_window`, and their `accounting` copies | `100800` | `14400` | Daily. |
| `oracle` `reward_distribution_window`, and its `accounting` copy | `403200` | `57600` | Four daily windows: each settlement pays a quarter of the pool, as at launch. |
| `asset.assets` | none | the ten launch currencies, `ACTIVE` | Conversion opens from genesis, as the launch chain opens it by vote. |
| `market.conversion_policy.base_pool` | 100,000 XDR | 5,000,000 XDR | A deep pool, so test conversions do not move it far. |

The production slashing set and the one-minute rate age are kept so their real behaviour is exercised.
