# Launch Genesis

- Status: policy settled, economic values pending (P1, P4); last updated 2026-09-06
- Artefact: `app/genesis/genesis.json`, data rather than a generator. `app/genesis_test.go` is its build check: it
  validates the file with the manager `arkd genesis validate` uses, boots a chain from it, and pins the launch policy.
- Decisions: the `Dnn` references below are entries in [economic decisions](../design/ECONOMIC_DECISIONS.md); the
  register stays the log, this document records what each setting is and why.

Every table reads Setting, Value, Status, Why. Status is one of **Decided** (a register row or design record fixed it),
**Default** (the module or SDK default, carrying its own rationale, to confirm at the launch review), **Pending** (a P1
or P4 launch value not yet chosen), or **Fix** (the artefact is wrong today).

Block counts assume the chain's six-second block: 600 blocks an hour, 14,400 a day, 100,800 a week, 5,256,000 a year
(`pkg/chain/blocks.go`).

For launch coordinators reviewing the curated artefact. Resolve pending values, run the final validation checklist,
and distribute one agreed genesis. This document is not a specification of every `arkd init` default.

## Contents

- [1. Rules the artefact follows](#1-rules-the-artefact-follows)
- [2. Chain and consensus](#2-chain-and-consensus)
- [3. Accounts, permissions, and supply](#3-accounts-permissions-and-supply)
- [4. Treasury](#4-treasury)
- [5. Market](#5-market)
- [6. Claims](#6-claims)
- [7. Reserve](#7-reserve)
- [8. Oracle](#8-oracle)
- [9. Asset registry](#9-asset-registry)
- [10. Cosmos SDK modules](#10-cosmos-sdk-modules)
- [11. IBC, interchain accounts, and CosmWasm](#11-ibc-interchain-accounts-and-cosmwasm)
- [12. Open decisions](#12-open-decisions)
- [13. Review before launch](#13-review-before-launch)

## 1. Rules the artefact follows

Ark launches from a clean genesis (D20). No legacy state, wire compatibility, or historical module-account balance
survives, so the artefact and the code obey these rules rather than a migration:

- No module registers a state migrator for launch; Treasury's consensus version is 1. No frozen legacy types,
  converters, reserved tags, store-prefix shims, or upgrade handler exist, and any that appear are removed.
- `x/mint` is absent from configuration and genesis: no store, query service, module account, or module-version entry
  (D1).
- The subsidy pool, Redemption Buffer, strategic Reserve, and Insurance seeds are bank balances, never minted by a
  module's InitGenesis (D9, [genesis ownership](GENESIS.md)).
- Distribution's `community_tax` is zero in every generated genesis because the distribution module basic is overridden
  in application code (`app/genesis.go`, D13); the artefact inherits it rather than patching it in.
- The four custody accounts are unblocked for inbound sends and each module's send restriction admits only positive
  `anoah` (D26). Treasury's InitGenesis runs after Bank's and rejects any fund balance holding a non-NOAH coin, closing
  the one credit path that never passes the runtime restriction.
- Bank supply must equal user balances plus every module allocation; bank genesis validation enforces it once balances
  exist.
- A relaunch takes a new chain ID and continues heights: `arkd export` refuses a zero-height export on purpose
  (`CLAUDE.md`, Export & Relaunch).

Export and import must preserve Treasury params, tax caps, the Claims mandate, allowance used, the Insurance
reservation, every claim with its status, all fund balances, total supplies, the denomination-bearing `BasePool`, the
labelled `ArkPoolDelta`, and every governance proposal that authorised a Reserve-to-Buffer commitment or a claim. Only
the launch genesis starts with a zero `ArkPoolDelta`.

## 2. Chain and consensus

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `chain_id` | `ark-1` | Pending | Placeholder. Choose the launch identifier; a later relaunch takes a new one. |
| `genesis_time` | `0001-01-01T00:00:00Z` | Pending | Set at launch. |
| `initial_height` | `1` | Decided | A fresh chain starts at one; continuation exports start at the next height. |
| `consensus.block.max_gas` | `100000000` | Decided | The base-fee controller measures block utilisation against this budget; without a finite `max_gas` its guard held the price at the floor forever ([local design](../../x/treasury/README.md#conversion-factors-and-dynamic-fees)). |
| `consensus.block.max_bytes` | `22020096` | Default | SDK default. |
| `consensus.abci.vote_extensions_enable_height` | `1` | Decided | Oracle rates arrive through vote extensions; `abci/voteextension.VoteExtensionsAvailable` treats zero as disabled, so no rate would ever be applied. `arkd testnet` and the upgrade rehearsal both set one. Pinned by `TestLaunchGenesisEnablesVoteExtensions`. |
| `consensus.evidence` | 100,000 blocks, 48 h, 1 MiB | Default | SDK defaults. |
| `consensus.validator.pub_key_types` | `ed25519` | Default | SDK default. |
| `consensus.version.app` | `0` | Default | SDK default. |

## 3. Accounts, permissions, and supply

Module accounts are code (`app/app_config.go`), listed here because the genesis review confirms them:

| Account | Permissions | Inbound user sends | Why |
| --- | --- | --- | --- |
| `market` | Minter, Burner | Blocked | The sole minter; mints and burns only in conversion settlement (D2). |
| `treasury_subsidy_pool` | none | `anoah` only | Balance-constrained subsidy pool; permissionless NOAH deposits are the only refill (D9, D25). |
| `treasury_redemption_buffer` | none | `anoah` only | Coverage-based redemption inventory (D4, D26). |
| `strategic_reserve` | Burner | `anoah` and registry members | Reserve custody; the burn authority is split between committee and governance (D61). External symbols are refused at the restriction, permanently (D70, amended 2026-09-06). |
| `claims_insurance` | none | `anoah` only | Insurance custody, paid only through recorded claims (D15, D26). |
| `transfer_tax_collector` | none | Blocked | Holds the funding window's transfer tax until settlement routes it; its only outflows are Treasury settlement. The one exempt pair is its send into `strategic_reserve`, which carries derecognised tax into inert custody (D26). |
| `oracle` | none | Blocked | Oracle reward pool. |
| `fee_collector` | none | Blocked | Gas fees before Distribution. |
| `transfer`, `wasm`, staking pools, `gov` | as the SDK and Wasmd require | Blocked | Upstream module accounts; transfer holds Minter and Burner for vouchers (D45). |

The blocked list replaces the SDK default rather than augmenting it, so every blocked account above is named
explicitly and the four custody accounts are deliberately left out.

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `bank.balances`, `bank.supply` | empty | Pending (P1) | The subsidy pool seed, the three fund seeds, and every other allocation. The subsidy seed sizes the zero-revenue runway (§12). |
| `bank.denom_metadata` | NOAH: base `anoah`, display `noah`, 18 decimals | Decided | The native and staking unit; eighteen decimals as every Ark denomination. |
| `bank.params` | `default_send_enabled: true`, no per-denom entries | Default | The fund rules are send restrictions in code, not bank params. |
| `auth.accounts` | empty | Pending (P1, P4) | Committee accounts must exist as base accounts with their own NOAH for gas, never funded from Insurance or Reserve. |
| `auth.params` | SDK defaults | Default | |

## 4. Treasury

Params (governance-owned, `x/treasury/types/params.go`):

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `reference_denom` | `axdr` | Decided | The reference unit for the tax cap and gas pricing, independent of Market's pool unit (D21); it needs a feed, not registry membership. |
| `reference_tax_cap` | `1` | Pending (P1) | Placeholder. One base unit caps every taxed input at one base unit; zero is uncapped; the real figure is chosen with the rate. Per-denomination caps derive from it through the conversion-factor table (`docs/design/ECONOMIC_DESIGN.md` §8.4). |
| `transfer_tax_rate` | `0` | Pending (P1) | No tax collected while zero, so the Oracle target is met from subsidy alone. A rate is governance's because it is part of the fee every wallet signs (D80) and must stay at or below the 2% spread floor (D81). |
| `reward_funding_window` | `100800` | Decided | One chain week of settlement observations (D34). Bounded at 2^32 so the accrual cannot overflow. |
| `exposure_refresh_period_blocks` | `600` | Default | Hourly recompute of the multiplier; a Terra-speed run unfolds over days. |
| `volatility_decay` | `0.99995` | Default | Variance half-life of about a day: a regime, not an event. |
| `flow_decay` | `0.99885` | Default | Flow half-life of about an hour: the one signal an actor can manufacture, so it forgets fast. |
| `multiplier_cap` | `4` | Default | With launch-scale ratios the scaled targets stay below the whole liability, so overflow burn still fires. |
| `multiplier_max_step` | `0.25` | Default | A climb from one to the cap takes at least twelve sustained hours. |
| `min_base_gas_price` | `100000000000` axdr per gas | Default | Terra Classic's posted XDR gas price carried to eighteen decimals: 0.02 XDR per 200k-gas transfer, the band Terra ran in production. |
| `base_fee_target_utilisation` | `0.5` | Default | Half-full blocks, EIP-1559's midpoint. |
| `base_fee_adjustment_rate` | `0.025` | Default | At most 2.5% per block; sustained full blocks double the price in about 28 blocks. Deliberately slow, to observe before tightening. |

Economic policy (the committee's levers, all zero):

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `validator_block_reward_target` | `0` | Pending (P1) | Minimum aggregate validator funding per block in base-unit NOAH; met from fees, then tax above the Oracle floor, then subsidy (D10, D11). |
| `oracle_block_reward_target` | `0` | Pending (P1) | Minimum aggregate Oracle funding per block; protected first out of tax (D11). |
| `redemption_buffer_target_ratio` | `0` | Pending (P1) | See §12 for what each ratio sets. Zero means every expansion overflow-burns and no fund ever builds. |
| `strategic_reserve_target_ratio` | `0` | Pending (P1) | |
| `insurance_target_ratio` | `0` | Pending (P1) | |
| `liability_ratio_weight`, `volatility_weight`, `flow_weight` | `0` | Decided | Zero at launch keeps the multiplier at one and the targets unscaled (D72); weights are set from live data after observability's calibration window. |

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `economic_mandate` | empty | Pending (P4) | The committee address and shape, its half-open activation window, and the minimum and maximum policies it may move between (D36). Empty disables delegation. |
| `tax_caps` | derived | Decided | A zero reference cap derives a complete explicit-zero uncapped map without Oracle prices ([genesis ownership](GENESIS.md)). |
| `reward_funding` | canonical empty state | Decided | The first observation initialises the window from the param (D34). |

## 5. Market

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `conversion_policy.base_pool` | `1000000000000000000000000 axdr` (1,000,000 XDR) | Default | Terra Classic's launch depth carried to eighteen decimals. The denomination is decided (D22); the amount is a P1 judgement about conversion depth. |
| `conversion_policy.min_stability_spread` | `0.02` | Default | Terra Classic's default. The transfer tax may not exceed it (D81). |
| `conversion_policy.pool_recovery_period` | `14400` | Default | A day, Terra Classic's default; each block returns delta divided by period toward zero. |
| `ark_pool_delta` | `0` | Decided | The launch pool starts balanced, in the pool unit. |
| `params.default_tobin_tax` | `0.0025` | Decided | Parity with the Oracle-side rates it replaced ([local design](../../x/market/README.md#conversion-policy)). |
| `tobin_tax_overrides` | none | Decided | The one override the design note anticipated was for `amnt`, which is not a launch asset. |
| `conversion_mandate` | empty | Pending (P4) | A committee may move policy inside a governance corridor and a Tobin band; optional at launch. |

## 6. Claims

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `params.claim_cancellation_period_blocks` | `100800` | Decided | One week, so the veto window fits a governance voting cycle; positive by validation, capped at a year (D30, D38, moved into `x/claims` by D54). |
| `claims_mandate` | empty | Pending (P4) | The Claims committee multisig, its activation and expiry heights, and the fixed gross `committee_claim_limit` for the term. |
| `claims`, `claims_allowance_used`, `insurance_reserved`, `next_claim_id` | none, `0`, `0`, `1` | Decided | Zero accounting at launch ([genesis ownership](GENESIS.md)). |

## 7. Reserve

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `mandate` | empty | Decided | At launch governance alone moves Reserve NOAH, through the fixed Reserve-to-Buffer message; the committee is appointed when a deployment is approved (`docs/governance/GOVERNANCE_OPERATIONS.md` §5). |
| `recognition_policy` | empty | Decided | NOAH-only custody counts at par; an eligibility entry arrives with the first attested external asset (D58, D69). |
| `params` | empty | Default | The module has no launch parameters. |

## 8. Oracle

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `reference_denom` | `axdr` | Decided | The protocol reference unit (`x/oracle/README.md` §1.3). |
| `feeds.denoms` | the ten registry denominations plus `axdr` | Decided | Every registered asset is priced by the feed its denomination keys; `axdr` is priced without being listed, since a feed creates no liability. |
| `params.vote_threshold` | `0.666…7` | Default | More than two thirds of power must vote, above Terra's half. |
| `params.reward_band` | `0.02` | Default | Terra Classic's default. |
| `params.reward_window` | `100800` | Default | Weekly reward settlement. |
| `params.reward_distribution_window` | `5256000` | Default | A year, Terra Classic's default; must be at least the reward window. |
| `params.attendance_window` | `100800` | Default | Weekly attendance grading. With the 5% floor, a dark validator recovers by attending the last twentieth of the window, most of a week: the defence against correlated jailing when a target change outpaces sidecar rollouts. |
| `params.min_attendance_per_window` | `0.05` | Default | Deliberately lenient, for the reason above. Zero switches jailing off. |
| `params.functioning_block_threshold` | `0.5` | Default | A block grades attendance only once a majority of power priced it, so a dark coalition must itself be a majority to switch grading off. Floored at a half. |
| `params.participation_threshold` | `0.2` | Default | A report must price a fifth of the targets to count as participation: low enough for a partial provider gap, high enough that one hardcoded rate is not an oracle. Capped at a half. |
| `params.max_exchange_rate_age` | `60s` | Default | Every freshness check measures against it; capped at seven days because a value that never elapses removes the gate rather than loosening it. |
| `exchange_rates`, `attendance_records`, `reward_weights` | empty | Decided | State the chain builds. |

## 9. Asset registry

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `assets` | `aaud`, `acad`, `acny`, `aeur`, `agbp`, `ajpy`, `akrw`, `amxn`, `asgd`, `ausd`, all `ACTIVE`, version 1, 18 decimals | Decided | The launch stablecoin set is the denomination set in `pkg/chain/denom.go`. Registering an asset is what creates a liability and admits it to conversion. |
| asset `description` | "A native asset of Ark Icarus." | Fix | Placeholder text; confirm the wording before launch. |
| `params.settlement_activation_delay_blocks` | `14400` | Default | A day for governance to cancel a mistaken settlement plan before its terms bind; a parameter because the voting period can move without the module knowing. |

## 10. Cosmos SDK modules

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `staking.params.bond_denom` | `anoah` | Decided | The native token stakes (`app/config.go`). |
| `staking.params` (unbonding 21 d, 100 validators, 7 entries, 10,000 historical entries, 0 min commission) | SDK defaults | Default | Unreviewed; confirm the validator count and unbonding time at the launch review. |
| `distribution.params.community_tax` | `0` | Decided | Nothing skims validator income; the community-pool ledger stays but receives only rounding residue (D13, D14). |
| `distribution.params` (proposer rewards 0, withdraw address enabled) | SDK defaults | Default | |
| `gov.params.min_deposit` | 10 NOAH | Decided | `app/config.go`; expedited is five times it, the SDK ratio. |
| `gov.params.min_initial_deposit_ratio` | `0.1` | Decided | Matches the Cosmos Hub. |
| `gov.params` (2 d voting, 1 d expedited, 2 d deposit period, quorum 33.4%, threshold 50%, veto 33.4%, veto deposits burned) | SDK defaults | Default | Unreviewed; a two-day voting period also bounds how fast an emergency parameter change lands. |
| `slashing.params` (100-block window, 50% signed, 10 min jail, 5% double-sign, 1% downtime) | SDK defaults | Default | Unreviewed; a 100-block window is ten minutes at this block time. |
| `mint` | absent | Decided | D1. |
| `security` | empty mandate and plan | Decided | The security committee is appointed after launch ([local design](../../x/security/README.md#powers-and-rationale)). |
| `upgrade`, `evidence`, `feegrant`, `authz`, `vesting`, `genutil` | empty | Default | Gentxs are collected at launch. |

## 11. IBC, interchain accounts, and CosmWasm

IBC ships installed and shut behind one switch, an empty allowed-client list; the contract runtime ships open (decided
2026-09-06). Opening IBC is a governance sequence: admit `07-tendermint`, configure rate limits for every route (D46),
then the transfer flags. The activation tests are complete (`docs/design/ECONOMIC_DESIGN.md` §11).

| Setting | Value | Status | Why |
| --- | --- | --- | --- |
| `ibc.client_genesis.params.allowed_clients` | `[]` | Decided | No client type at launch. Every IBC surface starts with a client, so this one switch holds channels, ICS-20, ICA, GMP, and contract channels shut; governance admits `07-tendermint` when it opens the hub. `09-localhost` and `08-wasm` stay unavailable (D45, D50, amended 2026-09-06). |
| `ibc.connection_genesis.params.max_expected_time_per_block` | `30s` | Default | ibc-go default. |
| `transfer.params.send_enabled`, `receive_enabled` | `false`, `false` | Decided | Off until the tax, recipient-restriction, and rate-limit gates are live on a real route (D45, `docs/design/ECONOMIC_DESIGN.md` §11.2). |
| `interchainaccounts.controller_genesis_state.params.controller_enabled` | `false` | Decided | Launched disabled (`docs/design/ECONOMIC_DESIGN.md` §11.2). |
| `interchainaccounts.host_genesis_state.params` | `host_enabled: false`, `allow_messages: []` | Decided | Activation names explicit type URLs, never the wildcard (`docs/design/ECONOMIC_DESIGN.md` §11.2). |
| `ratelimit` | no limits, hourly epoch | Default | Limits are governance-set per denomination and channel before a route opens (D46). |
| `packetfowardmiddleware` | no in-flight packets | Default | Classic-only forwarding (D46). |
| `wasm.params` | upload `Everybody`, instantiate `Everybody` | Decided | The contract runtime ships open (2026-09-06): the tax seams are in place, the accept list is populated, and the empty client allowlist leaves contract channels unopenable (`docs/design/ECONOMIC_DESIGN.md` §11.3). |
| `08-wasm.contracts` | empty | Decided | No light-client checksums at launch (D50). |
| `gmp.ics27_accounts` | empty | Decided | Derived accounts are created on first use. |
| `07-tendermint` | no state | Decided | No client exists, and none can until governance admits the type. |

## 12. Open decisions

The economic values (P1) and the roles (P4) are the only launch settings not yet chosen. They are entered into the
artefact, validated and booted by its tests, and reviewed against §13.

| Value | Where it goes | Required decision |
| --- | --- | --- |
| Transfer tax rate | `treasury.params.transfer_tax_rate` | Fixed launch percentage, at or below the 2% spread floor (D81). |
| Reference tax cap | `treasury.params.reference_tax_cap` | Launch amount in `axdr`; zero is uncapped. |
| Validator block reward target | `treasury.economic_policy` | Minimum aggregate validator funding per block, base-unit NOAH. |
| Oracle block reward target | `treasury.economic_policy` | Minimum aggregate Oracle funding per block, base-unit NOAH. |
| Three fund target ratios | `treasury.economic_policy` | See the table below. |
| Exposure weights | `treasury.economic_policy` | Zero at launch (D72); set from live data. |
| Subsidy pool seed | `bank.balances` for `treasury_subsidy_pool` | Sizes the zero-revenue runway; later `anoah` deposits extend it. |
| Buffer, Reserve, Insurance seeds | `bank.balances` | Existing NOAH assigned to each fund; the Buffer seed is redemption coverage from block one. |
| Base pool amount | `market.conversion_policy.base_pool` | Launch conversion depth in `axdr`; the denomination is decided. |
| Economic-policy committee | `treasury.economic_mandate` | Threshold-multisig address and shape, activation and expiry heights, minimum and maximum policies. |
| Claims committee | `claims.claims_mandate` | Multisig address and shape, activation and expiry heights, fixed gross claim limit. |
| Conversion committee | `market.conversion_mandate` | Optional at launch: policy corridor and Tobin band. |
| Committee fee funding | `auth.accounts`, `bank.balances` | Each committee's own NOAH for gas, or a separately approved narrow fee grant; never automatic fund spend. |
| Chain ID and genesis time | top level | |

What each ratio sets, and what to choose it from. Under NOAH-only custody every fund's exposure has liability as its
only base, so these ratios are the exposure model. [Future changes](../direction/FUTURE_CHANGES.md#4-capital-and-protocol-follow-ups)
records the trigger for revisiting a separate per-fund model:

| Ratio | What it literally sets | Choose it from |
| --- | --- | --- |
| Redemption Buffer target ratio | Steady-state coverage share: each redemption draws `min(1, B/L)` of its NOAH from inventory and mints the rest (D4), and that share holds through a run (D73) | The dilution accepted per unit redeemed; a 10% ratio mints 90% of every redemption's NOAH |
| Strategic Reserve target ratio | Coverage headroom governance can add by committing to the Buffer, plus deployable surplus | The crisis coverage ceiling: Buffer plus Reserve ratios is the highest coverage share reachable by committing everything |
| Insurance target ratio | Expected covered loss over a horizon plus a tail margin, as a fraction of liability | The perils the claims committee is chartered for and a judgement on their frequency and size; there is no loss history |
| Exposure weights | How far all three targets rise under stress: liability ratio, reference volatility, net redemption flow | Observability's calibration window, from live data |

The zero-revenue runway, once both reward targets are positive:

```text
worst_case_coverage_blocks = subsidy_pool_noah / (validator_block_reward_target + oracle_block_reward_target)
```

A conservative display estimate, not consensus state: fees and tax lengthen it, and nothing refills the pool but
permissionless `anoah` deposits. Stablecoin tax cannot become NOAH subsidy without a conversion, and no automatic
conversion exists.

The Claims committee controls only typed claim submission and cancellation inside its window and term. Its balance is
for gas; deposits or expansion allocations to Insurance raise coverage but grant it no custody or send authority.
Governance may replace or disable it at any time, submits under the same period, and may cancel any pending claim before
its closing height. There is no launch Reserve-withdrawal floor, deployment cap, or price trigger to configure: each
Reserve-to-Buffer proposal states its own amount and minimum remaining balance.

## 13. Review before launch

`app/genesis_test.go` pins, today: validity under the CLI's manager; a boot with a validator set and one funded
account; zero `community_tax`; the NOAH metadata; the empty allowed-client list, both ICS-20 flags, both ICA sides,
the empty host allowlist, and the open contract runtime, read back from the keepers after the first block; the absence
of `mint`; no 08-wasm checksums; and the vote-extension enable height.

Confirm by hand, once the P1 and P4 values are in:

- Module-account permissions match §3 and the blocked list names every blocked account.
- Every fund balance is `anoah` only, and supply equals the sum of balances (bank validation).
- Each committee account exists, is distinct from every other role and from the governance authority, and holds its
  own gas.
- The transfer tax rate is at or below the spread floor and the reference cap is a real figure or zero.
- The subsidy runway implied by the seed and the two targets is intended.
- The asset descriptions, the chain ID, and the genesis time are final.
