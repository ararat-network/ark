# Treasury

Treasury owns economic policy and the calculations behind transfer tax, liability, exposure, capital targets, and reward funding. Market supplies conversion facts; Asset supplies lifecycle membership and pricing views; Claims and Reserve own their custody and report recognised capital.


[State](#stored-state-and-ownership) · [Liability](#liability-and-capital-reads) · [Exposure](#exposure-sampling-and-refresh) · [Factors and fees](#conversion-factors-and-dynamic-fees) · [Reward funding](#reward-funding-and-genesis) · [Development](#development)

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/liability.go](keeper/liability.go) | Liability valuation. |
| [keeper/exposure.go](keeper/exposure.go) | Exposure samples and refresh. |
| [keeper/capital.go](keeper/capital.go) | Fund targets and capital requirements. |
| [keeper/settlement.go](keeper/settlement.go) | Conversion allocation and settlement. |
| [keeper/tax.go](keeper/tax.go) | Transfer-tax calculation. |
| [keeper/conversion_factors.go](keeper/conversion_factors.go) | Block-refreshed denomination factors. |
| [keeper/gas_pricing.go](keeper/gas_pricing.go) | Base-fee control. |
| [keeper/reward_funding.go](keeper/reward_funding.go) | Reward-funding window settlement. |
| [keeper/reference_denom.go](keeper/reference_denom.go) | Reference-unit rebasing. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`keeper/keeper.go` declares policy, mandate, factor, exposure, and base-gas-price collections. BeginBlock refreshes conversion factors and exposure on its cadence. EndBlock advances reward funding and updates the base gas price. Conversion settlement values liability from final state; there is no preblock liability-priming call. Follow the current ordering in `app/app_config.go` when editing these paths.

## Stored state and ownership

`Params`, `ConversionFactors`, `RewardFunding`, `EconomicMandate`, `EconomicPolicy`, `ExposureState`, and
`BaseGasPrice` are persistent state. Bank owns fund balances; targets and recognised liability are derived rather
than mirrored. Claims and Reserve independently report their recognised capital. Their structurally identical Go
interfaces are explicitly bound to the intended keepers in app depinject configuration.

Treasury's economic mandate carries minimum/maximum policies around the shared envelope. Governance replaces any
structurally valid policy independently of the mandate; the committee replaces the whole lever set only inside its
active term and corridor. Stance belongs in `EconomicPolicy`; controller machinery and domain caps belong in
`Params`. The cross-module authority rules remain in [economic design](../../docs/design/ECONOMIC_DESIGN.md#102-role-separation).

## Liability and capital reads

The asset registry supplies lifecycle membership and pricing verdicts. Treasury has no enrolment set or
stable-versus-commodity classifier. Each member's gross and Reserve-held supply use the same valuation branch, so
netting cannot credit a self-held amount whose gross liability was omitted. The [economic liability partition](../../docs/design/ECONOMIC_DESIGN.md#71-the-liability-partition)
owns the status treatment and gross-versus-net basis rule.

`FundStatus` reports that partition, disclosures, both target families, and the multiplier; incomplete valuation
zeroes targets without hiding the report. Insurance capital comes through Claims' recognised-capital interface,
not a second reservation calculation. `RewardFunding` reads stored accounting without needing current prices.
No combined backing figure is exposed. There is no Treasury Reserve-transfer history: Bank and the authorising
proposal already record those movements.

Retired designs include the liability snapshot and flat 2,000-gas lookup, supply-change invalidators, and
`PricedLiveVersion`/`TaxCapsEpoch` membership epochs. End-of-block conversion settlement has one final-state
valuation consumer; per-block conversion factors have no membership-epoch consumer. A shared capital-accounting
package was also rejected because the removed snapshot supplied its supposed common state and no duplicated
arithmetic remained to extract.

## Exposure sampling and refresh

`SettleConversions` samples reference returns and net redemption flow before its zero-totals return, so idle blocks
decay the flow EMA. BeginBlock refreshes the multiplier on the governed cadence or while `ExposureRefreshPending`
is set. Unusable inputs leave a pending retry; hard state failures fail the block. Every completed refresh emits
`EventExposureRefreshed`, including unchanged values: an equality guard would destroy its heartbeat meaning.
The explicit pending flag distinguishes a stalled refresh from missing observations downstream.

The instrument updates are:

```text
variance = volatility_decay * variance + (1 - volatility_decay) * clamped_return^2
flow     = flow_decay * flow + (1 - flow_decay) * max(0, RedeemedValue - EligiblePrincipal)
LR       = net_liability / circulating_NOAH
sigma    = sqrt(variance * blocks_per_year)
FR       = flow / net_liability
raw      = (1 + liability_weight * LR) * (1 + volatility_weight * sigma) * (1 + flow_weight * FR)
m        = clamp(step_limit(raw, previous_m, max_step), 1, multiplier_cap)
```

Circulating NOAH excludes the raw NOAH balances in the four custody accounts, not their haircut-valued recognised
capital. [exposure.go](keeper/exposure.go) defines unavailable-input handling and fixed-precision operations.
Returns are clamped before variance accumulation. Checked arithmetic and write-time caps bound the instrument;
a representable input whose combined raw stress exceeds the arithmetic range saturates at the multiplier cap.
The [economic model](../../docs/design/ECONOMIC_DESIGN.md#72-targets-and-the-exposure-multiplier) owns the indicator and target
formulas. Requirements use the multiplier; payment denominators and the controller's own liability input do not.
Scaling the draw would double-count a signal already reflected in Buffer retention and make payout depend on exit timing.

Peg deviation is absent because the feeds observe reference series against NOAH, not each issued token's market
price. Vote dispersion was rejected as a substitute: it measures reporting disagreement and is minority-influenceable.
Reference rebasing treats the exposure anchor as a price, using the reciprocal direction from quantities; otherwise
a unit change would look like a market return. [Protocol monitoring](../../docs/operations/PROTOCOL_MONITORING.md) owns monitoring and calibration.

## Conversion factors and dynamic fees

One `ConversionFactors` table serves fee prices and tax caps. BeginBlock refreshes a member with a servable rate,
retains an existing factor through an outage, and seeds a new unpriceable member at one. The reference has an identity
factor; NOAH has no usable row until a reference rate is available. A reference change atomically rebases the table,
cap, base-fee floor, and live price, so a cap update applies uniformly without a delayed per-member refresh.

Storing FX factors rather than final fee prices lets an oracle outage freeze FX while the base-fee congestion
controller continues adjusting. EndBlock consumes the ante's complete transient gas tally, floors the result at
`min_base_gas_price`, and caps it at `MaxBaseGasPrice`. A finite consensus `max_gas` is needed for utilisation-based
adjustment. The controller uses:

```text
target_gas = target_utilisation * consensus_max_gas
delta      = clamp(adjustment_rate * (paid_gas - target_gas) / target_gas, -adjustment_rate, adjustment_rate)
next_price = clamp(current_price * (1 + delta), min_base_gas_price, MaxBaseGasPrice)
```

An unbounded block or zero adjustment rate holds the price before applying its bounds. The signed-fee contract lives
in [economic design](../../docs/design/ECONOMIC_DESIGN.md#85-the-fee-declaration-settlement-charge).
A separate fee-market dependency and refund escrow were rejected: Ark needs its multi-denomination price contract
and standard SDK fee commitment semantics, without maintaining a fork of an incompatible fee module.

`GasPrices` exposes the reference row separately from the denomination-sorted rows; clients must not infer the
reference from list position. `ComputeTax` uses the consensus calculator and distinguishes malformed inputs,
missing caps, unrepresentable totals, and internal failures. [Client fee construction](../../docs/clients/CLIENT_FEES.md)
owns the integration algorithm; [arkd](../../cmd/arkd/README.md#cli-fee-completion) owns its denomination preference.

## Reward funding and genesis

`advanceRewardFunding` owns the countdown, accrual, due-window settlement, and atomic reset in EndBlock.
The fee collector then contains this block's earned fees; Distribution consumes them and any validator allocation
at the next BeginBlock. Oracle funding goes directly to Oracle. The [economic funding contract](../../docs/design/ECONOMIC_DESIGN.md#9-validator-and-oracle-funding)
owns the split, rounding, and depletion behaviour.

Genesis imports the stored state listed above; Bank genesis supplies custody. Treasury validates NOAH-only fund
balances because genesis bypasses runtime send restrictions, and validates its reference unit against Oracle.
Factors may exist for members not yet priced. [Launch genesis](../../docs/governance/GENESIS.md) owns actual settings.

## Development

Run from the repository root:

```sh
go test ./x/treasury/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds shared test helpers and mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/treasury/](../../proto/ark/treasury/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/treasury/v1/tx.proto),
[queries](../../proto/ark/treasury/v1/query.proto), and [events](../../proto/ark/treasury/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Economic policy and custody](../../docs/design/ECONOMIC_DESIGN.md).
- [Client fee construction](../../docs/clients/CLIENT_FEES.md).
- [Protocol-state monitoring](../../docs/operations/PROTOCOL_MONITORING.md).
- [Application wiring](../../app/README.md).
