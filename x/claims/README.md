# Insurance claims

This module owns the Insurance custody account, claim records, reservations, and the Claims mandate. Submission funds and reserves a claim, converting what a stablecoin claim's custody lacks; settlement pays eligible claims from the EndBlocker. Treasury consumes recognised Insurance capital through the keeper interface and owns the fund target.

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/claims.go](keeper/claims.go) | Claim submission, cancellation, and record processing. |
| [keeper/funding.go](keeper/funding.go) | Stablecoin funding through Market and the committee charge. |
| [keeper/reference_denom.go](keeper/reference_denom.go) | Reference rebase of the limit, charges, and usage. |
| [keeper/abci.go](keeper/abci.go) | Due-claim settlement. |
| [keeper/mandate.go](keeper/mandate.go) | Committee authorisation: signer, term, and window. |
| [keeper/send_restriction.go](keeper/send_restriction.go) | Inbound custody restrictions. |
| [keeper/msg_server.go](keeper/msg_server.go) | Governance and committee transaction entry points. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`NextClaimID` issues gap-free IDs; `Claims` retains records; `DueClaims` indexes pending work by closing time and claim ID. `InsuranceReserved` and `ClaimsAllowanceUsed` have different purposes: pending custody commitments by denomination and term usage in the reference unit. See `keeper/keeper.go` for their definitions. Application EndBlock ordering places Claims after governance so cancellations enacted there are visible to settlement, and after Market, which is why a claim converts when it is submitted rather than when it pays. Market is wired by hand in `app.go` (`SetMarketKeeper`), since Treasury injects Claims and Market injects Treasury.

## Claims and reservation lifecycle

Claims owns the `claims_insurance` account, mandate, claim records, and cancellation period; Treasury computes fund
targets and can credit Insurance but cannot debit it. `RecognisedCapital` reports the NOAH balance less NOAH
reservations, so one approved claim cannot cover a second loss; registry paper in custody counts for nothing, since Ark
paper is a liability until burned. The reservation spans appointments; allowance usage resets with a
new term. Keeping them separate prevents appointment changes from freeing money still promised to pending claims.

Submission records the origin, submitter, recipient, amount, reference, submission height and time, closing time, and a committee
term and charge only for committee claims. The amount is NOAH or an active registry asset. A recipient that is the Insurance module account or that Bank blocks is refused at
submission. The reference is a nonempty pointer to the off-chain case record, at most 2,048 bytes
(`chain.MaxReferenceBytes`). Governance claims have term zero and do not require an active mandate. Both paths
use the current `claim_cancellation_period`, in seconds of block time like the governance vote it must fit;
putting the period in the mandate was rejected because it made
governance claims depend on an enabled committee. Appointments must have a span at least as long as the current
period, measured from the later of activation and the appointing block. Raising the period later does not make param validation depend on mandate state; runtime expiry bounds
remain the backstop.

The due-claim index drives EndBlock settlement without an execute message: payment needs no signer because nothing about
it is discretionary, and the recipient of a covered loss is the party least able to send a transaction. A block settles
at most 1,000 due claims (`MaxSettlementsPerBlock`), oldest first; the rest stay pending and due for the next block,
already past the reach of cancellation, which ends at the closing time whenever settlement runs. Records preserve paid,
cancelled, and failed outcomes plus cancelling authority. [Economic
design](../../docs/economics/ECONOMIC_DESIGN.md#46-insurance-claims) owns fund-flow and authorisation policy.
`ClaimsMandate` reports term-scoped appointment/allowance, while `Balance` reports custody/reservation across all terms.
The [query schema](../../proto/ark/claims/v1/query.proto) defines their exact shape.

The module restricts deposits through `pkg/chain.ValidateRegisteredDeposit`: positive NOAH or registry members from any
sender, with no collector exemption. A deposit in a registry member waits uncommitted for the next claim in it. App Bank
restriction ordering must register every restriction provider. Genesis fixtures cannot seed claims without the Bank
balance needed to back their reservation. Module-account names determine custody addresses and cannot be renamed
on a live chain as a cosmetic refactor.

Claims is separate from Treasury so custody, appointment, reservations, and settlement share one owner rather than
turning fiscal policy into a multi-mandate operator. No claims-adjudication engine is part of this module: governance
and the committee make the claim decision; the chain enforces the resulting bounded commitment.

## Stablecoin funding and the committee limit

A stablecoin claim funds itself when it is submitted. It draws uncommitted paper in Insurance custody first, then buys
the rest through Market's ordinary buy with uncommitted Insurance NOAH, inside the submitting message, offering at most
the claim's `max_offer`; Market's buy sizes the smallest sufficient offer, and its surplus of a few units stays
uncommitted. Market's `buy` query sizes the cap for a prospective claim; quoting the whole amount is always enough,
since custody only lowers what converts. A NOAH claim carries no `max_offer`. Every other check runs before the buy, so a refused claim never
trades. Settlement only sends from custody in the claim's denomination, so a claim pays even if its asset is suspended
after submission. Cancellation never reaches Market: the paper a cancelled or failed claim bought stays uncommitted and
funds the next claim in its denomination before anything converts. Governance claims convert inside their proposal as
it executes, at governance's EndBlock, ahead of Market's block settlement.

`committee_claim_limit` is in the reference unit. A committee claim charges it with what the claim drew: its custody and
the NOAH it converted, each valued at the fresh rates Market quoted with and rounded up, written on the record as
`charge`. A claim is checked against the limit at face value before the buy, which can only raise it, and charged what
it drew after; the spread counts because cancelling a claim cannot undo its buy. A committee claim, NOAH included,
needs a fresh rate for its denomination and the reference, so through an oracle outage only governance submits.
Governance claims charge nothing and read no rate. A reference re-point (Oracle's `MsgSetReferenceDenom`) rebases the
limit and every committee claim's charge, rounded up, re-derives the live term's usage from the charges, and raises the
limit to the usage where the per-claim ceilings outran it.

## Development

Run from the repository root:

```sh
go test ./x/claims/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds the generated keeper mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/claims/](../../proto/ark/claims/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/claims/v1/tx.proto),
[queries](../../proto/ark/claims/v1/query.proto), and [events](../../proto/ark/claims/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

Errors are registered with their gRPC codes, so a query returns the keeper's error and the code follows it: a
missing claim is `NotFound`, on the transaction path as on the query path. Every other failure in the module is a
plain error, since nothing branches on it. Handlers classify only the request itself.
On a transaction path, wrap with `errorsmod.Wrapf` when the cause may carry a code: the SDK finds a transaction's
ABCI code only through its own wrapper, so a standard-library wrap keeps the text but reports internal.

## Related documents

- [Insurance custody and claims policy](../../docs/economics/ECONOMIC_DESIGN.md).
- [Committee operations](../../docs/governance/COMMITTEE_RUNBOOK.md#7-claims).
- [Application wiring](../../app/README.md).

## Settlement failure and custody invariants

Settlement rechecks the recipient, coverage in the claim's denomination, and record under the current binary before its
first write. An unparseable or blocked recipient, insufficient custody, an invalid record, or a payment Bank refuses,
such as a recipient whose own restriction admits NOAH alone, ends the claim as failed and releases its reservation. Bank applies its send restrictions before it moves a coin, so a refused payment
leaves nothing written. Failure is terminal: immutable recipient problems cannot heal within the same binary, and a
custody shortfall indicates broken reservation accounting; a fresh claim naming a payable recipient is the remedy for
the first. The failure path does not revalidate a record whose invalidity may be why it was called. Failure details
stay in node logs so error formatting is not part of the event contract. Store and event errors still propagate as
block errors.

Recognised capital requires `reserved <= balance` in NOAH, and every denomination's reservation holds the same bound.
Submission and genesis enforce coverage; payment releases exactly what it sends; cancellation and failure release
without sending. Insurance has no burner permission or other
spending owner. The checked invariant prevents corrupt reservations from silently becoming a larger Treasury
funding gap. Recipient payability is deliberately not an import condition: settlement can resolve a blocked recipient
without making genesis depend on the binary's blocked-address set. Claims are permanent: IDs run from one without a
gap and the sequence sits one past them, so an export that lost a claim cannot import.
