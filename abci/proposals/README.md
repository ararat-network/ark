# Oracle Proposal Handling

The proposals package wraps SDK proposal handlers so vote-extension data can move from CometBFT extended commits into
the block proposal bytes that preblock can read.

Preblock does not receive the local extended commit directly. When vote extensions are enabled, the proposer must inject
the previous height's extended commit info into the proposal. Other validators then validate that injected data before
accepting the proposal.

## PrepareProposal

`Handler.PrepareProposalHandler` runs before a block proposal is broadcast.

When vote extensions are enabled, it validates `LocalLastCommit` without modifying it, encodes the authenticated
extended commit info, reserves space in `MaxTxBytes`, calls the wrapped prepare-proposal handler, and injects the encoded
extended commit info into the first transaction slot of the returned proposal.

The wrapped handler remains responsible for normal transaction selection. The oracle wrapper always keeps protocol
metadata out of the wrapped request, reserves its protobuf-encoded size from `MaxTxBytes`, and prepends it exactly once
to the wrapped response.

## ProcessProposal

`Handler.ProcessProposalHandler` runs on validators receiving a proposed block.

When vote extensions are enabled, it verifies that the proposal includes injected extended commit info in the expected
slot, requires every validator's commit flag to match the consensus last commit, verifies vote-extension signatures and
quorum, and then delegates normal proposal validation to the wrapped process-proposal handler. Oracle payload validity is
classified later by the oracle consumer rather than by proposer-side mutation.
