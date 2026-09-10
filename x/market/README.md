# Market conversion

Market implements conversion between NOAH and registered assets, and between eligible registered assets. It owns quotation, the virtual-pool state, Tobin overrides, and the conversion mandate. Treasury owns allocation of conversion proceeds and fund targets; Market accumulates conversion facts and calls Treasury at EndBlock.

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/swap.go](keeper/swap.go) | Swap quotation and entry points. |
| [keeper/conversion.go](keeper/conversion.go) | Conversion execution. |
| [keeper/accumulator.go](keeper/accumulator.go) | Block-local conversion accumulation. |
| [keeper/settle.go](keeper/settle.go) | Holder redemption against an active settlement plan. |
| [keeper/abci.go](keeper/abci.go) | Block conversion settlement, returned burn, and pool replenishment. |
| [keeper/tobin.go](keeper/tobin.go) | Tobin overrides. |
| [keeper/reference_denom.go](keeper/reference_denom.go) | Reference-unit rebasing. |
| [keeper/mandate.go](keeper/mandate.go) | Conversion mandate authorisation. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`Params`, `ArkPoolDelta`, `TobinTaxOverrides`, `ConversionPolicy`, and `ConversionMandate` are persistent collections. The transient store carries block-local conversion state. Market leads the application EndBlock sequence so later governance and claims work observes settled funds. Native conversion minting belongs here; IBC voucher minting is a separate transfer-module responsibility.

## Conversion policy

Tobin policy belongs here: a default plus sparse per-denomination overrides. The effective cross-asset spread uses
the larger of the offer and ask rates. An override annotates a registered asset; it never creates membership.
Setting requires a registered asset, while removing requires only the override. Retirement leaves an inert override
rather than making Asset write Market state. A new denomination cannot inherit a retired denomination's history.

Ordinary conversion accepts `ACTIVE` or `ISSUANCE_HALTED` offers and only `ACTIVE` asks. Status gates precede rate
reads, and issuance rechecks status at minting. Settlement redemption is a separate holder-signed `MsgSettle`:
`SUSPENDED`, an activated plan, asset-to-NOAH only. It uses the plan's committed rate, never the virtual pool or an
ordinary reverse-issuance path. Economic flows and rounding are specified in [economic design](../../docs/design/ECONOMIC_DESIGN.md#6-conversion-flows).

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

Conversions record `EligiblePrincipal`, `RedemptionOutput`, and `RedeemedValue` in transient state, all in NOAH at
each conversion's own quoted rate. Checked additions and the output-versus-retired-value bound fail the transaction
that violates them. Cross-asset conversions record no NOAH flow: they burn the whole offer and mint only the post-spread output.
NOAH expansion retains the whole offer for settlement; redemption burns the offered asset and immediately mints
the post-spread NOAH output. Only fund allocation and the compensating burn wait for settlement.

Market's EndBlocker sends one `ConversionTotals` to Treasury, burns the returned amount from Market custody, then
replenishes its virtual pool. The call needs no rate set: conversions already recorded valued facts. It runs for idle
blocks too, so Treasury can sample zero flow. [Application ordering](../../app/README.md#block-lifecycle) keeps this
before governance lifecycle changes and later fund movements.

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
[queries](../../proto/ark/market/v1/query.proto), and [events](../../proto/ark/market/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Conversion and settlement policy](../../docs/design/ECONOMIC_DESIGN.md).
- [Asset eligibility](../asset/README.md).
- [Application wiring](../../app/README.md).

## Local quote and admission contracts

A swap minimum is either the empty coin or a positive ask-denomination amount. A denominated zero is rejected.
The floor is checked before any settlement writes. A quote that loses its entire payout to spread or integer
truncation is refused; very small virtual-pool offers can reach a spread of exactly one.

NOAH-pair spread is the constant-product shortfall from a frictionless fill, bounded below by the policy floor.
A tiny trade can remain at that floor regardless of depth, which is why committee bounds include the floor alongside
depth and recovery. Transfer-tax policy must respect the reachable corridor floor, not just its current value.

A stranded corridor becomes usable again if the reference returns to its denomination while the original term and
window remain valid. Genesis therefore accepts mismatched units. The Tobin band applies to the shared override map,
including governance-written entries; only governance can remove an override to resume tracking the default.
