# ABCI oracle integration

This tree integrates oracle vote extensions with the SDK lifecycle. [app/oracle.go](../app/oracle.go) installs these
hooks around the default transaction proposal handlers. The protocol is fixed; [oracle/README.md](oracle/README.md) owns aggregation and participation rules, and
[x/oracle](../x/oracle/README.md) owns feed epochs and persistent accounting.

## Package map

| Package | Responsibility |
| --- | --- |
| [voteextension](voteextension/vote_extension.go) | ExtendVote and VerifyVoteExtension handlers. |
| [proposals](proposals/proposals.go) | Carry and authenticate extended commit info in proposal bytes. |
| [preblock](preblock/preblock.go) | Module preblock delegation, oracle processing, then feed promotion. |
| [oracle](oracle/README.md) | Extract, aggregate, score, and apply reports. |
| [codec](codec/codec.go) | Bounded vote-extension and extended-commit wire encoding. |
| [types](types/interfaces.go) | Narrow keeper/client interfaces and shared boundary errors. |
| [metrics](metrics/), [oracle/metrics](oracle/metrics/) | Hook and oracle instruments. |
| [testutil](testutil/) | Fixtures and generated mocks. |

## Vote-extension handlers

`ExtendVote` loads the target epoch for the requested vote height and asks the cached node-side price client for a
snapshot. It filters to canonical targets, drops individual undecodable rates, validates the resulting partial report,
and encodes its target version. Feed/client/encoding failures are logged and yield an empty extension to preserve oracle
failure isolation; malformed requests and structural failures remain visible errors. An empty report's attendance/reward
treatment is specified in [oracle processing](oracle/README.md#participation-and-functioning-blocks).

`VerifyVoteExtension` accepts empty extensions. A non-empty extension must decode and pass the vote-height target/version
and rate validation. The encoded payload is generated into [voteextension/types](voteextension/types/) from the
[ABCI schema](../proto/ark/abci/v1/vote_extension.proto).

## Proposal handlers

Preblock does not receive the local extended commit directly. `PrepareProposal` validates the previous local extended
commit, encodes it, reserves its protobuf transaction bytes, invokes ordinary transaction selection with the reduced
budget, and prepends the metadata exactly once. The wrapped selector never sees protocol metadata as a user transaction.

`ProcessProposal` checks the injected commit against consensus last-commit information, validator flags, signatures, and
quorum before delegating ordinary transaction validation. It does not rewrite the authenticated commit based on the
quality of individual oracle reports. [Mempool policy](../app/mempool/README.md#admission-service-and-sdk-behaviour) explains ordinary transaction service and
proposal-local lane budgets after this envelope is reserved.

## Preblock order

1. Run the module manager's preblockers first so an upgrade's state/schema changes precede oracle reads.
2. When vote-extension data is available, consume the reports for the previous vote height and apply prices/accounting.
3. Advance due feed transitions and prune removed-feed rates.

Consume-before-promote is required: promotion loses the earlier epoch needed to validate the previous height's reports.
Hook timing/status metrics are recorded in finalisation mode separately from wrapped module preblock latency.
The wrapper owns no asset completion or liability-priming call. Asset transitions execute through messages; Treasury owns
its own BeginBlock refreshes and end-of-block valuation/settlement.

## Verification

From the root, run `go test ./abci/...`. Hook tests cover disabled/enabled boundaries, proposal validation, and the feed
activation ordering; oracle tests cover aggregation separately. For application wiring changes also run relevant `./app`
tests. [x/oracle](../x/oracle/README.md) documents keeper state and [PROCESS_MONITORING.md](../docs/PROCESS_MONITORING.md) interprets metrics.
