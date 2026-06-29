# Oracle Vote Extensions

The vote-extension package owns Noah's `ExtendVote` and `VerifyVoteExtension` handlers.

Vote extensions carry oracle price reports from each validator into the next height. They are local to each validator
until a proposer includes the previous extended commit info in a block proposal, so the proposal path is what makes one
canonical set of reports available for preblock aggregation.

## ExtendVote

`Handler.ExtendVoteHandler` fetches prices from the oracle client, validates the response, encodes it with
the configured vote-extension codec, and returns the encoded bytes to CometBFT.

Oracle failures are not consensus failures. If the oracle client is unavailable, returns nil prices, returns invalid
prices, or encoding fails, the handler logs the error and returns an empty vote extension. This preserves chain liveness
while still letting later aggregation and metrics account for missing reports.

## VerifyVoteExtension

`Handler.VerifyVoteExtensionHandler` validates vote extensions seen from other validators.

Empty vote extensions are accepted. Non-empty vote extensions must decode successfully and pass oracle vote-extension
validation. Invalid non-empty extensions are rejected because they represent malformed data, not a validator choosing to
submit no oracle prices.

## Boundaries

- `abci/ve/types` defines the encoded oracle vote-extension payload.
- `abci/codec` encodes and decodes vote-extension bytes.
- `abci/oracle` decodes accepted proposal data and applies aggregation rules later in preblock.
