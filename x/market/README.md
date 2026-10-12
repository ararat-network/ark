# Market conversion

Market implements conversion between NOAH and registered assets, and between eligible registered assets. It owns quotation, the virtual-pool state, Tobin overrides, and the conversion mandate. Treasury owns allocation of conversion proceeds and fund targets; Market accumulates conversion facts and calls Treasury at EndBlock.

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/price.go](keeper/price.go) | A pair's snapshot, the forward quote, and its inverse. |
| [keeper/orders.go](keeper/orders.go) | Sell, Buy, and Settle entry points and their bounds. |
| [keeper/ledger.go](keeper/ledger.go) | Booking a quote through the module account. |
| [keeper/conversion.go](keeper/conversion.go) | Policy application shared by governance and committee updates. |
| [keeper/accumulator.go](keeper/accumulator.go) | Block-local conversion accumulation. |
| [keeper/abci.go](keeper/abci.go) | Block conversion settlement, returned burn, and pool replenishment. |
| [keeper/tobin.go](keeper/tobin.go) | Tobin overrides. |
| [keeper/reference_denom.go](keeper/reference_denom.go) | Reference-unit rebasing. |
| [keeper/mandate.go](keeper/mandate.go) | Conversion mandate authorisation. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`Params`, `ArkPoolDelta`, `TobinTaxOverrides`, `ConversionPolicy`, and `ConversionMandate` are persistent collections. The transient store carries block-local conversion state. Market settles after governance has executed the block's proposals and before every later EndBlocker that moves funds, so a proposal's conversions settle in their own block and claims work observes settled funds. Native conversion minting belongs here.

## Conversion policy

Tobin policy belongs here: a default plus sparse per-denomination overrides. The effective cross-asset spread uses
the larger of the offer and ask rates. An override annotates a registered asset; it never creates membership.
Setting requires a registered asset, while removing requires only the override. Retirement leaves an inert override
rather than making Asset write Market state. A new denomination cannot inherit a retired denomination's history.

Ordinary conversion accepts `ACTIVE` or `ISSUANCE_HALTED` offers and only `ACTIVE` asks. Status gates precede rate
reads. Settlement redemption is a separate holder-signed `MsgSettle`:
`SUSPENDED`, an activated plan, asset-to-NOAH only. It uses the plan's committed rate, never the virtual pool or an
ordinary reverse-issuance path. Economic flows and rounding are specified in [economic design](../../docs/economics/ECONOMIC_DESIGN.md#3-conversion).

## Conversion mandate

`ConversionPolicy` is separate from `Params`, so replacing governance params cannot overwrite a committee's policy
change from a stale draft. `ConversionMandate` contains the shared envelope, minimum and maximum policies in one
pool denomination, and a Tobin cap. Governance updates policy without the corridor; committee updates require the
exact signer, term, active window, and bounds on every field. Both use one apply path, proportionally rescale the
pool delta, validate effective pools, and emit `EventPoolUpdated`.

The corridor covers depth, recovery period, and the spread floor. Depth alone cannot throttle tiny conversions whose
constant-product spread remains below the floor. The Tobin power is a band from the default to the mandate cap;
a raise-only ratchet was rejected because a committee could not undo its own overshoot. Zero cap delegates no Tobin
power; governance retains below-default overrides and removal. Policy stays with the module owning the pool, without
a Treasury-to-Market write dependency.

Reference changes rebase the pool and delta together, preserving spread pressure. Policy updates cannot change the
unit. The mandate corridor is not rebased silently: its old denomination closes committee policy updates until
reappointment; the dimensionless Tobin cap survives. The [committee runbook](../../docs/governance/ECONOMIC_COMMITTEE_RUNBOOK.md#reference-change-and-conversion-mandate)
owns the ordered reappointment procedure.

## Block settlement and failure boundaries

Conversions record `GrossOffer`, `EligiblePrincipal`, `RedemptionOutput`, and `RedeemedValue` in transient state, all
in NOAH at each conversion's own quoted rate; `GrossOffer` is what Treasury's waterfall places, `EligiblePrincipal`
what liability grew by. Checked additions, the bound that minted value cannot exceed the gross offer, and the
output-versus-retired-value bound fail the transaction that violates them. Cross-asset conversions record no NOAH flow: they burn the whole offer and mint only the post-spread output.
NOAH expansion retains the whole offer for settlement; redemption burns the offered asset and immediately mints
the post-spread NOAH output. Only fund allocation and the compensating burn wait for settlement.

Market's EndBlocker sends one `ConversionTotals` to Treasury, burns the returned amount from Market custody, then
replenishes its virtual pool. The call needs no rate set: conversions already recorded valued facts. It runs for idle
blocks too, so Treasury can sample zero flow. [Application ordering](../../app/README.md#block-lifecycle) keeps this
after governance's proposal execution and before later fund movements.

The previous swap-time settlement needed a cached liability snapshot, flat-metered reads, priming, supply-change
notifications, and cross-module invalidators. End-of-block valuation removed those maintenance obligations and their
silent stale-cache failure mode. It also keeps settlement's variable recognition cost out of individual swap gas.
Incomplete valuation parks principal and discloses it; store, send, or arithmetic errors propagate from EndBlock.
Those are block failures, so write-time domain bounds remain the liveness defence.

## Development

Run from the repository root:

```sh
go test ./x/market/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds shared test helpers and mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/market/](../../proto/ark/market/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/market/v1/tx.proto),
[queries](../../proto/ark/market/v1/query.proto), and [events](../../proto/ark/market/v1/event.proto). `MsgSell` and `MsgBuy`
are the two orders and both settle as one swap: a sell states the offer and takes what the quote returns, a buy states
the ask and pays the least offer that returns it, within a required maximum. Neither carries a recipient: a payment
in another denomination is a buy and a Bank send in one transaction, taxed on the send. `Query/Sell` quotes a sell;
`Query/Buy` inverts that quote in closed form and confirms it forward; Disbursement funds a stablecoin award and Claims
a stablecoin claim with a buy.

Errors are registered with their gRPC codes, so a quote returns the keeper's error and the code follows it: a
malformed order is `InvalidArgument`, an ineligible asset or a crossed floor or cap is `FailedPrecondition`,
arithmetic the pools cannot represent is `OutOfRange`, and a missing rate or asset is `NotFound` from the module
that owns the record. Handlers classify only the request itself.
On a transaction path, wrap with `errorsmod.Wrapf` when the cause may carry a code: the SDK finds a transaction's
ABCI code only through its own wrapper, so a standard-library wrap keeps the text but reports internal.
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Conversion and settlement policy](../../docs/economics/ECONOMIC_DESIGN.md).
- [Asset eligibility](../asset/README.md).
- [Application wiring](../../app/README.md).

## Local quote and admission contracts

A swap minimum is either the empty coin or a positive ask-denomination amount. A denominated zero is rejected.
The floor is checked before any settlement writes. A quote that loses its entire payout to spread or integer
truncation is refused; very small virtual-pool offers can reach a spread of exactly one.

NOAH-pair spread is the constant-product shortfall from a frictionless fill, bounded below by the policy floor.
A tiny trade can remain at that floor regardless of depth, which is why committee bounds include the floor alongside
depth and recovery. Transfer-tax policy must respect the reachable corridor floor, not just its current value.

From a rested pool a swap of `x` pool units pays `x / (depth + x)`, which meets a 2% floor at about a fiftieth of the
depth. Each block returns one part in the recovery period of the imbalance, so steady one-way flow holds the
imbalance at one period's flow. The marginal spread is about twice the imbalance over the depth, so flow stays at the
floor while one period's worth is within half the floor times the depth. The
[committee runbook](../../docs/governance/ECONOMIC_COMMITTEE_RUNBOOK.md#conversion-depth) sizes depth from these two
figures.

A stranded corridor becomes usable again if the reference returns to its denomination while the original term and
window remain valid. Genesis therefore accepts mismatched units. The Tobin band applies to the shared override map,
including governance-written entries; only governance can remove an override to resume tracking the default.
