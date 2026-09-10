# Insurance claims

This module owns the Insurance custody account, claim records, reservations, and the Claims mandate. Submission reserves funds; settlement pays eligible claims from the EndBlocker. Treasury consumes recognised Insurance capital through the keeper interface and owns the fund target.

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/claims.go](keeper/claims.go) | Claim submission, cancellation, and record processing. |
| [keeper/abci.go](keeper/abci.go) | Due-claim settlement. |
| [keeper/mandate.go](keeper/mandate.go) | Appointment and term allowance. |
| [keeper/send_restriction.go](keeper/send_restriction.go) | Inbound custody restrictions. |
| [keeper/msg_server.go](keeper/msg_server.go) | Governance and committee transaction entry points. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`Claims` retains records; `DueClaims` indexes pending work by closing height and claim ID. `InsuranceReserved` and `ClaimsAllowanceUsed` have different purposes: pending custody commitments and term usage. See `keeper/keeper.go` for their definitions. Application EndBlock ordering places Claims after governance so cancellations enacted there are visible to settlement.

## Claims and reservation lifecycle

Claims owns the `claims_insurance` account, mandate, claim records, and cancellation period; Treasury computes fund
targets and can credit Insurance but cannot debit it. `RecognisedCapital` reports the balance less pending reservations,
so one approved claim cannot cover a second loss. The reservation spans appointments; allowance usage resets with a
new term. Keeping them separate prevents appointment changes from freeing money still promised to pending claims.

Submission records the origin, submitter, recipient, amount, reference, submission/closing heights, and a committee
term only for committee claims. Governance claims have term zero and do not require an active mandate. Both paths
use the current `claim_cancellation_period_blocks`; putting the period in the mandate was rejected because it made
governance claims depend on an enabled committee. Appointments must have a span at least as long as the current
period. Raising the period later does not make param validation depend on mandate state; runtime expiry bounds
remain the backstop.

The due-claim index drives EndBlock settlement without an execute message. Records preserve paid, cancelled, and
failed outcomes plus cancelling authority. [Economic design](../../docs/ECONOMIC_DESIGN.md#66-insurance-claims) owns
fund-flow and authorisation policy. `ClaimsMandate` reports term-scoped appointment/allowance, while `Balance` reports
custody/reservation across all terms. The [query schema](../../proto/ark/claims/v1/query.proto) defines their exact shape.

The module restricts deposits through `pkg/chain.ValidateNoahOnlyDeposit`, with no collector exemption. App Bank
restriction ordering must register every restriction provider. Genesis fixtures cannot seed claims without the Bank
balance needed to back their reservation. Module-account names determine custody addresses and cannot be renamed
on a live chain as a cosmetic refactor.

Claims was separated from Treasury so custody, appointment, reservations, and settlement share one owner rather than
turning fiscal policy into a multi-mandate operator. No claims-adjudication engine or non-NOAH payout is part of this
module: governance and the committee make the claim decision; the chain enforces the resulting bounded commitment.

## Development

Run from the repository root:

```sh
go test ./x/claims/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds shared test helpers and mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/claims/](../../proto/ark/claims/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/claims/v1/tx.proto),
[queries](../../proto/ark/claims/v1/query.proto), and [events](../../proto/ark/claims/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Insurance custody and claims policy](../../docs/ECONOMIC_DESIGN.md).
- [Committee operations](../../docs/ECONOMIC_COMMITTEE_RUNBOOK.md).
- [Application wiring](../../app/README.md).
