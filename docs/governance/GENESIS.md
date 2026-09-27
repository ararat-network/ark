# Launch Genesis

- Status: decided 2026-09-11. The artefact is complete except for what assembly adds: the validator seats, the
  seat-scaled reward targets, and the genesis time.
- Artefact: `app/genesis/genesis.json`, data rather than a generator. `app/genesis_test.go` is its build check: it
  validates the file with the manager `arkd genesis validate` uses, boots a chain from it two ways, and pins every
  decided value.
- Decisions: the `Dnn` references below are entries in [economic decisions](../design/ECONOMIC_DECISIONS.md); the
  register stays the log, this document records what each setting is and why.

Every table reads Setting, Value, Status, Why. Status is one of **Decided** (the launch review or a register row fixed
it), **Default** (the module or SDK default, confirmed at the launch review), or **Assembly** (set when the final file
is built, §12).

Block counts assume the chain's six-second block: 600 blocks an hour, 14,400 a day, 100,800 a week, 5,256,000 a year
(`pkg/chain/blocks.go`, and the 5 s commit timeout in `cmd/arkd/cmd/config.go`). Dollar figures assume the 1 USD
opening price the artefact's NOAH factor encodes (§4).

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
- [11. IBC, interchain accounts, and CosmWasm](#11-ibc-interchain-accounts-and-cosmwasm)
- [12. Assembly sequence](#12-assembly-sequence)
- [13. Committee appointments](#13-committee-appointments)
- [14. Review before launch](#14-review-before-launch)
- [15. Testnet artefact](#15-testnet-artefact)

## 1. Rules the artefact follows

Ark launches from a clean genesis (D20). No legacy state, wire compatibility, or historical module-account balance
survives, so the artefact and the code obey these rules rather than a migration:

- No module registers a state migrator for launch; Treasury's consensus version is 1. No frozen legacy types,
  converters, reserved tags, store-prefix shims, or upgrade handler exist, and any that appear are removed.
- `x/mint` is absent from configuration and genesis: no store, query service, module account, or module-version entry
  (D1). The launch supply is the native supply for the life of the chain, changed only by conversion and authorised
  Reserve burns.
- The subsidy pool, Redemption Buffer, strategic Reserve, Insurance, disbursement pools, and community pool seeds are bank
  balances, never minted by a module's InitGenesis (D9). The community pool's fee-pool entry equals the Distribution module balance,
  which Distribution's InitGenesis requires.
- Distribution's `community_tax` is zero because the distribution module basic is overridden in application code
  (`app/genesis.go`, D13); the artefact inherits it rather than patching it in.
- The four custody accounts are unblocked for inbound sends and each module's send restriction admits only positive
  `anoah` (D26). Treasury's InitGenesis runs after Bank's and rejects any fund balance holding a non-NOAH coin.
- Bank supply equals the sum of balances at every step: the artefact carries the disbursement pools and the community
  pool before seats, and assembly moves each seat's grant out of the community pool rather than adding supply.
- A relaunch takes a new chain ID and continues heights: `arkd export` refuses a zero-height export on purpose
  (`CLAUDE.md`, Export & Relaunch).

Export and import must preserve Treasury params, tax caps, the Claims mandate, allowance used, the Insurance
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
| `market` | Minter, Burner | Blocked | The sole minter; mints and burns only in conversion settlement (D2). |
| `treasury_subsidy_pool` | none | `anoah` only | Balance-constrained subsidy pool; permissionless NOAH deposits are the only refill (D9, D25). |
| `treasury_redemption_buffer` | none | `anoah` only | Coverage-based redemption inventory (D4, D26). |
| `strategic_reserve` | Burner | `anoah` and registry members | Reserve custody; the burn authority is split between committee and governance (D61). External symbols are refused at the restriction, permanently (D70). |
| `claims_insurance` | none | `anoah` only | Insurance custody, paid only through recorded claims (D15, D26). |
| `transfer_tax_collector` | none | Blocked | Holds the funding window's transfer tax until settlement routes it. The one exempt pair is its send into `strategic_reserve` (D26). |
| `oracle` | none | Blocked | Oracle reward pool. |
| `fee_collector` | none | Blocked | Gas fees before Distribution. |
| `distribution` | none | Blocked | The community pool. Governance spends it by `MsgCommunityPoolSpend`; nothing else moves it. |
| `disbursement` | none | open | The member and contributor pools from genesis, fully funded grants, and compensation; governance opens tranches and nothing else draws the pools (D87, D88). |
| `gov` | Burner | open | Holds deposits, and is the signer of every proposal-executed message. |
| `transfer`, `wasm`, staking pools | as the SDK and Wasmd require | Blocked | Upstream module accounts; transfer holds Minter and Burner for vouchers (D45). |

The blocked list replaces the SDK default rather than augmenting it, so every blocked account above is named
explicitly and the four protocol-fund custody accounts, `disbursement`, and `gov` are deliberately left out.

### Supply ledger

Total supply is 1,000,000,000 NOAH, `1000000000000000000000000000` in base units. There are no investors, backers, or
public allocation at launch: the protocol funds are seeded, the member and contributor pools are held by the
Disbursement module, the validator seats are granted, and the remainder is the community pool, which governance spends
by vote (D88).

| Balance | NOAH | Share | Status | Why |
| --- | --- | --- | --- | --- |
| `treasury_subsidy_pool` | 100,000,000 | 10% | Decided | The payroll fund for the life of the chain: at the per-seat income below it runs 50 years for fifteen seats and 7.5 years for a hundred, before any gas or tax. Drawn by settlement; governance may raise the targets or return idle balance to the community pool through `MsgReturnSubsidy` (D83). |
| `strategic_reserve` | 50,000,000 | 5% | Decided | Governance's intervention capacity through the first growth phase; commitments to the Buffer and surplus burns are its only exits. |
| `treasury_redemption_buffer` | 10,000,000 | 1% | Decided | Full coverage on the first 10M NOAH of liability, so early redemptions pay from inventory rather than minting. |
| `claims_insurance` | 5,000,000 | 0.5% | Decided | The only claims capacity until the waterfall reaches Insurance, which is last in line. |
| `disbursement` member pool | 439,200,000 | 43.92% | Decided | 60% of the distribution. Tranches open by vote; the pool has no exit, so it pays members or nobody (D88). |
| `disbursement` contributor pool | 292,800,000 | 29.28% | Decided | 40% of the distribution: ownership awards, compensation, and conversion orders. Governance may return it to the community pool (D88). |
| `distribution` (community pool) | 103,000,000 less the seats | 10.3% | Decided | 50,000,000 after ten seats: governance's discretionary fund, and the backstop behind the Reserve and subsidy pool. Assembly moves 5,300,000 NOAH per seat out of it (§12). |

Under the defensive target ratios in §4, fund targets are zero until conversion creates liability, so the seeds are
what give the funds capacity before the first expansion.

### Validator seats

The ten founding validators hold the same stake, granted from the community pool, so at launch governance is one seat
one vote. Entry after launch is open: a validator bonds NOAH it holds, and no proposal grants a seat (D84). Until
distributed NOAH is bonded the founders hold every vote, a proof-of-authority trust model on proof-of-stake
machinery; the [threat model](../design/THREAT_MODEL.md#27-governance-and-committees) records it.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| Seat grant | 5,000,000 NOAH, vesting from four to ten years after genesis | Decided | A continuous vesting account whose start is four years after genesis and whose end is ten: deferred pay for a decade of running the chain. Unvested coins can be delegated, vote, and be slashed, never transferred, so equal power is structural through the launch years and the floats stay the only liquid NOAH a founder holds until year four. A double-sign takes 250,000 NOAH, two years of income; downtime takes 500. The founding seats are the only granted stake, 5% of supply across ten; later validators bond NOAH they hold. |
| Seat float | 300,000 NOAH, liquid | Decided | Unvested coins cannot pay fees. Operations need little; the floats are the only liquid NOAH at launch and so the only NOAH that can be converted into the currencies, about 4.5M across fifteen seats. |
| Per-seat income | 0.025 NOAH a block, 131,400 a year, split 70/30 between the validator and oracle lanes | Decided | Funds a highly available operation with the price sidecar, on-call, and the governance and committee work, at about 10,000 USD a month. The targets are floors: once tax exceeds them the oracle lane takes every residual, so the mature split is set by revenue, not by these numbers (§4). |
| `auth.params.tx_sig_limit` | `7` | Decided | The ante counts every member key of a multisig, not its threshold, so committees hold at most seven keys (§13). |
| `auth.accounts` | empty in the artefact | Assembly | One continuous vesting account per seat, its window four to ten years after genesis time, holding the grant as its original vesting plus the float; the gentx self-delegates the grant. Committees are appointed after launch. |

After launch a validator registers with create-validator, bonding NOAH it holds at a commission at or above the
floor; no proposal is involved and nothing is drawn from the pool. The reward targets do not follow the set: they are
the founding seats' shares at assembly, and raising them as validators join is a governance policy choice by
`MsgUpdatePolicy` (§4), which the [disbursement plan](DISBURSEMENT_PLAN.md#6-if-the-public-does-not-stake) proposes
how to use.

## 4. Treasury

Params (governance-owned, `x/treasury/types/params.go`):

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `reference_denom` | `axdr` | Decided | The reference unit for the tax cap and gas pricing, independent of Market's pool unit (D21); it needs a feed, not registry membership. |
| `reference_tax_cap` | 1,000 XDR, `1000000000000000000000` | Decided | Tax is the smaller of rate times amount and the cap, so the cap sets the crossover: at 0.5% every transfer up to 200,000 XDR pays proportionally and larger ones pay a flat 1,000 XDR. Terra's 1 SDR cap left most value untaxed. Lowering the cap later refuses nothing; raising it refuses in-flight large transfers (D80). |
| `transfer_tax_rate` | `0.005` | Decided | At the top of Terra Classic's operating band, under the 2% spread floor a swap round trip would otherwise beat (D81), and the safe direction to start from since cuts never refuse in-flight transfers (D80). Revenue starts only once conversion has created currency supply. |
| `reward_funding_window` | `100800` | Decided | One chain week of settlement observations (D34); the first payment lands at block 100,800, which the floats cover. |
| `exposure_refresh_period_blocks` | `600` | Default | Hourly recompute of the multiplier. |
| `volatility_decay` | `0.99995` | Default | Variance half-life of 13,863 blocks, 23 hours: a regime, not an event. |
| `flow_decay` | `0.99885` | Default | Flow half-life of 603 blocks, an hour: the one signal an actor can manufacture, so it forgets fast. |
| `multiplier_cap` | `2` | Decided | The defensive ratios sum to 0.5, so full retention is reached at 2; above that only the committee bounds widen. Inert until the weights are set. |
| `multiplier_max_step` | `0.1` | Decided | With cap 2, a climb from one to the cap takes ten sustained hours. Inert until the weights are set. |
| `min_base_gas_price` | `100000000000` axdr per gas | Default | Terra Classic's posted 0.1 usdr per gas carried to eighteen decimals: 0.02 XDR per 200k-gas transfer. |
| `base_fee_target_utilisation` | `0.5` | Default | 50M gas a block, roughly 450 sends before the price moves. |
| `base_fee_adjustment_rate` | `0.025` | Default | Full blocks double the price in 28 blocks and raise it tenfold in nine minutes; empty blocks unwind at the same rate to the floor. |

Economic policy (the committee's levers once appointed, §13):

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `validator_block_reward_target` | `0` in the artefact; 0.0175 NOAH a block per seat, `17500000000000000`, added by each founding seat | Assembly | Every founding seat adds its share at assembly, so the launch target is the seat count times the share; entry after launch grants nothing, and the target then moves only by `MsgUpdatePolicy`, inside the committee corridor (§13) or by governance beyond it (D84). Met from gas first, then tax above the oracle floor, then subsidy (D10, D11). |
| `oracle_block_reward_target` | `0` in the artefact; 0.0075 NOAH a block per seat, `7500000000000000`, added by each founding seat | Assembly | Protected first out of tax (D11). The oracle lane is the only performance-weighted income, paid by band-gated accuracy, and the lane every tax residual flows to, so 30% of the floor keeps a half-accuracy operator 15% poorer without deferring most of a seat's income behind the oracle payout window (§8). |
| `redemption_buffer_target_ratio` | `0.30` | Decided | Each redemption pays 30% from inventory and mints 70% (D4), and that share holds through a run (D73). |
| `strategic_reserve_target_ratio` | `0.15` | Decided | With the Buffer, a 45% coverage ceiling when everything is committed, 22.5% after NOAH halves. |
| `insurance_target_ratio` | `0.05` | Decided | No loss history; a tail margin so the fund builds at all. |
| `liability_ratio_weight`, `volatility_weight`, `flow_weight` | `0` | Decided | Zero keeps the multiplier at one and the targets unscaled (D72); set from live data after observability's calibration window. |

The ratios are the defensive set: their sum is the retention share of growth, so half of every expansion's NOAH is
retained and half burned. All three funds hold NOAH against a NOAH-valued liability, so a ratio halves when NOAH
does, and a lean set would leave single-digit coverage in the run it exists for. Targets are not custody ceilings and
a lowered target releases nothing, so starting high keeps what is built; starting low can only catch up through
expansions that do not arrive during a run.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `economic_mandate` | empty | Decided | Appointed after the first expansions (§13). |
| `conversion_factors` | `anoah` at `1.371`, derived height 0 | Decided | Genesis requires a positive NOAH factor so gas is payable before the first `axdr` rate; the first usable reference observation replaces it. 1.371 is NOAH opening at 1 USD with XDR at 1.371 USD, and every dollar figure in this document assumes it. Recompute as USD per XDR over USD per NOAH, rounded up, if the opening price changes. |
| `base_gas_price` | the floor | Decided | The controller starts at `min_base_gas_price`. |
| `tax_caps` | derived | Decided | Derived from the reference cap through the conversion-factor table at the first refresh. |
| `reward_funding`, `exposure_state` | canonical empty state | Decided | The first observation initialises the window from the param (D34); the multiplier starts at one. |

The zero-revenue runway is `subsidy_pool / ((validator_target + oracle_target) × 5,256,000)` years, a display
estimate: fees and tax lengthen it, and nothing refills the pool but permissionless `anoah` deposits.

## 5. Market

Ark's pool is Terra's constant-product virtual pool, so the values align with Terra's original regime, not the 2022
expansions the whitepaper records as the accelerant. A single swap from a rested pool pays the floor up to about 2% of
the depth, and sustained one-way flow at the floor is about 1% of the depth a day with a one-day recovery.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `conversion_policy.base_pool` | 5,000,000 XDR, `5000000000000000000000000` | Decided | Sized to the bootstrap: the seat floats can mint about a million XDR of currencies a month at the floor, while a 10% run against a 10M liability still pays a 31% marginal spread. Depth is revisited by governance as liability grows, never by shortening recovery. |
| `conversion_policy.min_stability_spread` | `0.02` | Default | Terra Classic's default. The transfer tax may not exceed it (D81). |
| `conversion_policy.pool_recovery_period` | `14400` | Default | A day, Terra Classic's default; each block returns delta divided by period toward zero. |
| `ark_pool_delta` | `0` | Decided | The launch pool starts balanced, in the pool unit. |
| `params.default_tobin_tax` | `0.0025` | Decided | Terra's default; parity with the Oracle-side rates it replaced ([local design](../../x/market/README.md#conversion-policy)). |
| `tobin_tax_overrides` | none | Decided | Terra's one override was for an illiquid asset Ark does not launch. |
| `conversion_mandate` | empty | Decided | Governance moves policy until there is a reason to delegate (§13). |

## 6. Claims

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `params.claim_cancellation_period_blocks` | `100800` | Decided | One week, so the veto window fits a governance voting cycle; positive by validation, capped at a year (D30, D38, D54). |
| `claims_mandate` | empty | Decided | Appointed when Insurance has a charter naming the perils it covers (§13). Governance files claims itself meanwhile. |
| `claims`, `claims_allowance_used`, `insurance_reserved`, `next_claim_id` | none, `0`, `0`, `1` | Decided | Zero accounting at launch. |

## 7. Reserve

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `mandate` | empty | Decided | Governance alone moves Reserve NOAH through the fixed Reserve-to-Buffer message; the committee is appointed when a deployment is approved (§13). |
| `recognition_policy` | empty | Decided | NOAH-only custody counts at par; an eligibility entry arrives with the first attested external asset (D58, D69). |
| `params` | empty | Default | The module has no launch parameters. |

## 8. Oracle

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `reference_denom` | `axdr` | Decided | The protocol reference unit (`x/oracle/README.md` §1.3). |
| `feeds.denoms` | the ten registry denominations plus `axdr` | Decided | Every registered asset is priced by the feed its denomination keys; `axdr` is priced without being listed. |
| `params.vote_threshold` | `0.666…7` | Default | More than two thirds of power must vote, above Terra's half. |
| `params.reward_band` | `0.02` | Default | Terra Classic's default. |
| `params.reward_window` | `100800` | Default | Weekly reward settlement. |
| `params.reward_distribution_window` | `1310400` | Decided | Settlement pays out `balance × reward_window / reward_distribution_window` each week. A year, Terra's value, smoothed volatile seigniorage and would defer 62% of the subsidy era's oracle income past its first year; a quarter delivers 77% within the year and still smooths the mature tax inflow. `accounting` carries the same value. |
| `params.attendance_window` | `100800` | Default | Weekly attendance grading. With the 5% floor, a dark validator recovers by attending the last twentieth of the window. |
| `params.min_attendance_per_window` | `0.05` | Default | Deliberately lenient; the reward lane, not jailing, is the accuracy incentive. Zero switches jailing off. |
| `params.functioning_block_threshold` | `0.5` | Default | A block grades attendance only once a majority of power priced it. Floored at a half. |
| `params.participation_threshold` | `0.2` | Default | A report must price a fifth of the targets to count. Capped at a half. |
| `params.max_exchange_rate_age` | `60s` | Default | Ten blocks; capped at seven days. |
| `exchange_rates`, `attendance_records`, `reward_weights` | empty | Decided | State the chain builds. |

## 9. Asset registry

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `assets` | `aaud`, `acad`, `acny`, `aeur`, `agbp`, `ajpy`, `akrw`, `amxn`, `asgd`, `ausd`, all `ACTIVE`, version 1, 18 decimals | Decided | The launch currency set is the denomination set in `pkg/chain/denom.go`. Registering an asset is what creates a liability and admits it to conversion. |
| asset `description` | "An Ark currency tracking the Australian dollar." and so on | Decided | Named per currency from the table in `pkg/chain/metadata.go`. Stored metadata is checked against the derivation at every import, so a table entry is added only before its denomination is registered and never changed after; an unnamed denomination reads "An Ark currency." |
| asset `name`, `symbol` | `ArkAUD`, `arkAUD` and so on | Decided | Derived from the denomination. |
| `params.settlement_activation_delay_blocks` | `43200` | Decided | The window in which governance can cancel a mistaken settlement plan before its terms bind. Three days outlasts the two-day vote with a day's margin; the SDK-era default of a day equalled the expedited period and fell inside a normal one. The code default moved with it. |
| `emergency_mandate` | empty | Decided | Appointed in the first governance cycle (§13). |

## 10. Cosmos SDK modules

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `staking.params.bond_denom` | `anoah` | Decided | The native token stakes (`app/config.go`). |
| `staking.params.unbonding_time` | 21 days | Default | Confirmed; evidence age matches it (§2). |
| `staking.params.max_validators` | `100` | Default | Confirmed. The seat grant times the cap is what the set holds when full (§3). |
| `staking.params.min_commission_rate` | `0.05` | Decided | A zero floor invites a commission race that an operator with other income wins; seats registering below it are refused. |
| `staking.params` (7 entries, 10,000 historical entries) | SDK defaults | Default | Historical entries serve IBC once it opens. |
| `distribution.params.community_tax` | `0` | Decided | Nothing skims validator income; the community-pool ledger receives only rounding residue on top of its seed (D13, D14). |
| `distribution.params` (proposer rewards 0, withdraw address enabled) | SDK defaults | Default | |
| `distribution.fee_pool.community_pool` | 103,000,000 NOAH less the seats | Decided | Equal to the Distribution module balance (§3). |
| `gov.params.min_deposit`, `expedited_min_deposit` | 1,000 NOAH, 5,000 NOAH | Decided | About 1,000 USD, the range established chains settle in: deposits refund unless vetoed, so the deposit is capital locked for four days, not a fee. With the 10% initial ratio, 100 NOAH lists a proposal. `app/config.go` carries the same default. |
| `gov.params.quorum`, `threshold`, `expedited_threshold` | `0.5`, `0.667`, `0.75` | Decided | The founding set is ten equal seats and the pool it guards holds most of the supply: half the bonded stake must vote and two thirds of votes must agree. The SDK requires the expedited threshold above the regular one. |
| `gov.params.veto_threshold` | `0.334` | Default | A third of the set can block. |
| `gov.params` (2 d voting, 1 d expedited, 2 d deposit period, veto deposits burned) | SDK defaults | Default | Confirmed; the claims veto window and the tax-raise notice were sized on them. Lengthen after the calibration window if wanted. |
| `gov.constitution` | empty | Default | |
| `slashing.params` | window 10,000, 5% signed, 600 s jail, 0.01% downtime slash, 5% double-sign | Decided | The SDK default of a 100-block window at 50% jailed and slashed 1% after five minutes offline. This is the Cosmos Hub and Terra Classic production set: about 16 hours of window, and oracle attendance jailing is the sharper operational incentive. |
| `auth.params` | SDK defaults, `tx_sig_limit` 7 | Decided | See §3. |
| `bank.denom_metadata` | NOAH: base `anoah`, display `noah`, 18 decimals | Decided | The native and staking unit. |
| `bank.params` | `default_send_enabled: true`, no per-denom entries | Default | The fund rules are send restrictions in code, not bank params. |
| `mint` | absent | Decided | D1. |
| `security` | empty mandate and plan | Decided | Appointed in the first governance cycle (§13). |
| `upgrade`, `evidence`, `feegrant`, `authz`, `vesting`, `genutil` | empty | Default | Gentxs are collected at assembly. |

### Native grants

Both launch artifacts include the [Disbursement module](../../x/disbursement/README.md) with a 439,200,000 NOAH member pool
and a 292,800,000 NOAH contributor pool in its custody, nothing open, and no grants, journal entries, conversion orders,
or founders before assembly. Member terms are 10,000 NOAH, a tenth immediately and the balance over twelve monthly
periods. Issuance is limited to 250 members per rolling seven days. The grants mandate is disabled, with no
compensation allowance, until governance appoints one; NOAH is the initial compensation denomination. Genesis ownership policy is one fifth of others' bonded stake
and a 60M NOAH ceiling that counts the founding seat. Seat assembly adds one beneficiary record per founding
operator, carrying its immutable seat; the module derives founding stake from these records. IDs start at one.
Stablecoin compensation requires an explicit governance allowlist update and funding in that denomination, which a
conversion order of contributor NOAH can supply (D87, D88).

## 11. IBC, interchain accounts, and CosmWasm

IBC ships installed and shut behind one switch, an empty allowed-client list; the contract runtime ships open (decided
2026-09-06). Opening IBC is a governance sequence: admit `07-tendermint`, configure rate limits for every route (D46),
then the transfer flags. The activation tests are complete (`docs/design/ECONOMIC_DESIGN.md` §11).

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `ibc.client_genesis.params.allowed_clients` | `[]` | Decided | No client type at launch. Every IBC surface starts with a client, so this one switch holds channels, ICS-20, ICA, GMP, and contract channels shut; governance admits `07-tendermint` when it opens the hub (D45, D50). |
| `ibc.connection_genesis.params.max_expected_time_per_block` | `30s` | Default | ibc-go default. |
| `transfer.params.send_enabled`, `receive_enabled` | `false`, `false` | Decided | Off until the tax, recipient-restriction, and rate-limit gates are live on a real route (D45). |
| `interchainaccounts.controller_genesis_state.params.controller_enabled` | `false` | Decided | Launched disabled. |
| `interchainaccounts.host_genesis_state.params` | `host_enabled: false`, `allow_messages: []` | Decided | Activation names explicit type URLs, never the wildcard. |
| `ratelimit` | no limits, hourly epoch | Default | Limits are governance-set per denomination and channel before a route opens (D46). |
| `packetfowardmiddleware` | no in-flight packets | Default | Classic-only forwarding (D46). |
| `wasm.params` | upload `Everybody`, instantiate `Everybody` | Decided | The contract runtime ships open: the tax seams are in place, the accept list is populated, and the empty client allowlist leaves contract channels unopenable (`docs/design/ECONOMIC_DESIGN.md` §11.3). |
| `08-wasm.contracts` | empty | Decided | No light-client checksums at launch (D50). |
| `gmp.ics27_accounts` | empty | Decided | Derived accounts are created on first use. |
| `07-tendermint` | no state | Decided | No client exists, and none can until governance admits the type. |

## 12. Assembly sequence

The artefact is the reviewed base; assembly adds the time and the seats and changes nothing else.

1. **Time.** Set `genesis_time` in the file to a weekday hour, in UTC, when every validator's region is awake and at
   least 48 hours after the file will be published. The seats vest from it, so it comes first; a time changed after
   seating means seating again from the artefact.
2. **Seats.** Run `arkd genesis add-validator-seats <operator-address>...` once against the file, naming every
   genesis validator. For each it writes a continuous vesting account at the operator with 5,000,000 NOAH vesting
   from four to ten years after `genesis_time` and a 5,300,000 NOAH balance, subtracts 5,300,000 NOAH from both the
   Distribution balance and `fee_pool.community_pool`, raises both reward targets by one seat's share, and records the operator's 5M
   seat on its beneficiary record in `disbursement.beneficiaries`, leaving supply and the disbursement pools untouched. It refuses an unset `genesis_time`, an operator already present, and a pool that cannot fund
   the seats, and writes nothing unless every seat and every edited module validates. The seat policy is fixed in
   `pkg/chain/launch.go`; `TestLaunchGenesisBootsVestingSeat` boots what it writes.
3. **Gentxs.** Each validator generates its gentx against the file with chain ID `ark-1`, self-delegating the
   5,000,000 NOAH grant with commission at or above 5%. The gentx pays no fee at height zero.
4. **Collect.** Collect the gentxs; collection keeps `genesis_time`.
5. **Validate and publish.** Run `arkd genesis validate`, publish the file with its SHA-256, and have every validator
   verify the hash before starting. The chain starts itself at `genesis_time` once more than two thirds of the seats
   are online: 7 of 10, 11 of 15.

## 13. Committee appointments

No committee is appointed in genesis. The message path is what observes and records a committee's key shape, the
multisig accounts must exist and hold their own gas first, and appointments follow incident exposure rather than chain
age ([governance operations §6](GOVERNANCE_OPERATIONS.md#6-committee-appointments)). With half the set as quorum and a
one-day expedited vote, the deliberative committees save at most a day or two over governance itself; the two
emergency mandates buy real time.

| Mandate | Single job | Appoint | Shape |
| --- | --- | --- | --- |
| Security | Schedule or cancel its own upgrade, recover an IBC client. Never funds. | First governance cycle | 3-of-5 off-chain multisig |
| Asset emergency | Suspend an asset; one power, per-term usage tracked | First governance cycle | 3-of-5 off-chain multisig, a distinct account and where the seat count allows a distinct member set |
| Economic policy | Move the eight policy fields inside a corridor | After the first expansions; weights only after the calibration window | 4-of-7 |
| Claims | Submit and cancel claims under a gross term limit | When Insurance has a charter naming its perils | 4-of-7 |
| Conversion | Depth, recovery, and spread corridor, Tobin band | When there is a reason to delegate | 4-of-7 |
| Reserve | Deploy under an allowance, floor, and destination list | When a deployment is approved | 4-of-7 |

Shape and hygiene: legacy amino threshold multisigs of at most seven keys, within `tx_sig_limit`; dedicated
hardware-backed committee keys, not operator keys; each multisig registered on chain with one transaction before
appointment so the recorded shape is verified K-of-N; committee accounts funded from members' floats, never from
Insurance or the Reserve. Terms: activation a few blocks after execution, expiry one chain year, annual reappointment;
a Claims appointment must span at least the one-week cancellation period.

Economic committee corridor, when appointed: floor every weight at zero until calibration so the committee cannot
switch the multiplier on; bound the reward targets between half and twice the seat-scaled launch values; bound each
ratio between half and one and a half times the defensive set, with Insurance capped at 0.10.

## 14. Review before launch

`app/genesis_test.go` pins: validity under the CLI's manager; a boot with a validator set and one funded account; a
boot from a seat the seat command's own function granted, whose gentx self-delegates the grant, with the pool reduced
by the grant, supply unchanged, and both targets at one share; the consensus authority and evidence bounds; the staking, slashing, and gov values in §10; the
supply ledger, the unopened disbursement pools, and the community pool equalling the Distribution balance; every Treasury, Market, Oracle, and Asset
value in §4, §5, §8, and §9; the empty allowed-client list, both ICS-20 flags, both ICA sides, the empty host
allowlist, and the open contract runtime, read back from the keepers after the first block; the absence of `mint`; no
08-wasm checksums; and the vote-extension enable height.

Confirm by hand at assembly:

- Each seat's original vesting is 5,000,000 NOAH, its `start_time` and `end_time` four and ten years after the file's
  `genesis_time`, its balance 5,300,000, and the pool and fee-pool entries are reduced by exactly the seats' total;
  supply is still 1,000,000,000 NOAH.
- Both reward targets equal the seat count times the per-seat share, which the seat command guarantees when nothing
  else has edited them.
- Every gentx self-delegates the whole grant at a commission of at least 5%.
- The NOAH factor still reflects the opening price.
- The chain ID and the genesis time are final and the published hash matches.

## 15. Testnet artefact

`app/genesis/testnet.json`, chain ID `ark-testnet-1`, is the launch artefact with time compressed and nothing else
changed: the same supply, seats, economics, slashing, oracle thresholds, shut hub, and open contract runtime.
`TestTestnetGenesisDerivesFromLaunch` applies the list below to the launch file and requires equality module by
module, so the two cannot drift, and the validity and both boot tests run over both files. Seats are granted with the
same command (§12); faucet and test accounts are added with `arkd genesis add-genesis-account`, and a testnet supply
above a billion is acceptable. The Docker localnet and the e2e suites run every chain on this artefact
([localnet](../../contrib/localnet/README.md), [e2e](../../tests/e2e/README.md)).

| Setting | Launch | Testnet | Why |
| --- | --- | --- | --- |
| `chain_id` | `ark-1` | `ark-testnet-1` | |
| `gov.params` voting, expedited, deposit period | 2 d, 1 d, 2 d | 1 h, 30 min, 1 h | A proposal a day, with time to gather a small set's votes. The deposits stay at 1,000 and 5,000 NOAH. |
| `asset.params.settlement_activation_delay_blocks` | `43200` | `1200`, 2 h | Outlasts the vote, the launch rule. |
| `claims.params.claim_cancellation_period_blocks` | `100800` | `2400`, 4 h | A veto window that fits a vote plus its deposit period. |
| `staking.params.unbonding_time` | 21 d | 1 d | An unbonding completes inside a test day. |
| `consensus.evidence` age | `302400`, 21 d | `14400`, 1 d | Equals unbonding, the launch rule. |
| `treasury.params.reward_funding_window` | `100800` | `14400` | Daily settlement, so subsidy and tax flows are observable. |
| `oracle` `reward_window`, `attendance_window`, and their `accounting` copies | `100800` | `14400` | Daily. |
| `oracle` `reward_distribution_window`, and its `accounting` copy | `1310400` | `100800` | Keeps the payout smoothing at a seventh a window. |

The production slashing set and the one-minute rate age are kept so their real behaviour is exercised.
