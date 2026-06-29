# Oracle Proposal Handling

The proposals package wraps SDK proposal handlers so vote-extension data can move from CometBFT extended commits into
the block proposal bytes that preblock can read.

Preblock does not receive the local extended commit directly. When vote extensions are enabled, the proposer must inject
the previous height's extended commit info into the proposal. Other validators then validate that injected data before
accepting the proposal.

## PrepareProposal

`Handler.PrepareProposalHandler` runs before a block proposal is broadcast.

When vote extensions are enabled, it prunes and validates `LocalLastCommit`, encodes the extended commit info, reserves
space in `MaxTxBytes`, calls the wrapped prepare-proposal handler, and injects the encoded extended commit info into the
first transaction slot of the returned proposal.

The wrapped handler remains responsible for normal transaction selection. The oracle wrapper only reserves space and
adds the injected oracle payload.

## ProcessProposal

`Handler.ProcessProposalHandler` runs on validators receiving a proposed block.

When vote extensions are enabled, it verifies that the proposal includes injected extended commit info in the expected
slot, validates that data, and then delegates normal proposal validation to the wrapped process-proposal handler.

## Wrapped Handler Option

`WithRetainOracleDataInWrappedHandler` controls whether the wrapped prepare-proposal handler sees the injected oracle
payload in its input request. The default keeps oracle data out of the wrapped handler and injects it after the wrapped
handler returns.
