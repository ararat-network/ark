# Security committee

Security implements the bounded committee fast path over standard-module emergency functions: upgrade scheduling/cancellation and IBC client recovery. It constructs upstream messages and dispatches through the app message router with the effective authority. It owns no fund custody.

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/committee.go](keeper/committee.go) | Mandate checks and committee state. |
| [keeper/msg_server.go](keeper/msg_server.go) | Emergency messages and upstream dispatch. |
| [keeper/keeper.go](keeper/keeper.go) | Router, account/upgrade dependencies, and collections. |
| [types/mandate.go](types/mandate.go) | Mandate validation. |
| [types/committee.go](types/committee.go) | Committee message surface. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

The persistent items are `Mandate` and `CommitteePlan`. This module has no params or block hooks; authority windows are checked when the transaction executes. Review application privilege registration when changing the committee message surface, and update the threat model for a new message or trust boundary.

## Powers and rationale

`x/security` gives the chain a security committee with a bounded fast path over the standard-module
emergency surface. Its charter is one line: **code and connectivity repair, never funds.**

- **State.** `SecurityMandate{envelope}` and `CommitteePlan{name, height, term}`. No module account, params,
  hooks, or blockers. An enabled appointment holds every power the module delegates; an empty committee
  disables and still advances the term, so previously prepared committee transactions never become valid
  again.
- **Messages.** Governance `MsgSetSecurityMandate`; committee `MsgCommitteePlanUpgrade`,
  `MsgCommitteeCancelUpgrade`, `MsgCommitteeRecoverClient`, each carrying `expected_term` and authorising
  through `Envelope.Authorise` (exact signer, exact term, active window, in that order). The authority is
  rejected as committee. No-op actions are rejected.
- **Execution by dispatch.** Each handler constructs the exact upstream message (`MsgSoftwareUpgrade`,
  `MsgCancelUpgrade`, `MsgRecoverClient`) with `effectiveAuthority` as signer, resolved exactly as
  `sdk.ValidateAuthority` does (consensus-params authority when set, keeper fallback otherwise), and routes
  it through `baseapp.MessageRouter`, the technique x/gov uses. The target re-validates the authority itself,
  so no denylist exists: the only reachable messages are the three the module builds, and nothing accepts a
  caller-supplied `Any`. Committee messages run inside a normal transaction, so dispatch errors abort
  atomically.
- **Upgrade slot invariant: the committee can never affect a governance-scheduled upgrade.** `x/upgrade`
  holds one plan and scheduling overwrites it, so the committee may schedule only when the slot is empty or
  holds its own plan, and may cancel only its own. `CommitteePlan` is trusted only while it matches the
  pending plan on both name and height; a governance overwrite, governance cancel, or executed upgrade
  leaves a stale record that is cleared opportunistically, never trusted. No minimum lead time: halt-in-hours
  is the point.
- Queries `SecurityMandate` (with `active`) and `CommitteePlan` (with `matches_pending`). Events report
  resulting state. Every dependency comes through the constructor and the module reads only x/upgrade
  before dispatching.

**What was withdrawn and why.** Four freeze powers (per-denom send halt, IBC transfer halt, rate-limit
tightening, ICA host restriction): each froze user funds and defended economics, not the chain; Ark owns
economics at the source (asset suspension, the conversion corridor); a committee convenes in hours and an
exploit exits in minutes, so a freeze traps the holders who stayed, while a standing rate limit set in calm
conditions is what bounds exit velocity and governance keeps it; and a selective freeze can be aimed at
someone quietly while the chain runs, whereas an upgrade halt stops everything, announces itself, and has a
remediation path that does not wait for a proposal. The per-class grant list: the three remaining powers are
one job (scheduling and cancelling are inverses, and without scheduling there is no way to ship the patch),
so a subset is not a narrower committee but one that cannot finish, and appointing the committee is the
decision. Cancel-any: cancel-any plus schedule-own-only was defeated in two transactions; governance keeps
two remedies for a buggy approved upgrade (cancel its own plan, or validators restart past the halt with
`--unsafe-skip-upgrades`), and a committee veto over a voted upgrade is a governance override, not an
emergency power. Committee messages keep `MsgCommittee*` names so they never read like the upstream types
they construct; all amino names fit the 39-character cap.

## Development

Run from the repository root:

```sh
go test ./x/security/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds shared test helpers and mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/security/](../../proto/ark/security/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/security/v1/tx.proto),
[queries](../../proto/ark/security/v1/query.proto), and [events](../../proto/ark/security/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Authority and trust boundaries](../../docs/THREAT_MODEL.md).
- [Submission and upgrade operations](../../docs/EMERGENCY_SUBMISSION_RUNBOOK.md).
- [Application wiring](../../app/README.md).
