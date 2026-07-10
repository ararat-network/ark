# ABCI Oracle Flow

This tree contains Ark's ABCI integration for oracle vote extensions. The flow is fixed protocol code, not a
pluggable strategy layer: validators extend votes with oracle prices, proposers inject the previous extended commit into
block proposals, and the preblock hook aggregates those reports before transactions execute.

## Package Layout

- `ve/` owns `ExtendVote` and `VerifyVoteExtension` handlers.
- `proposals/` owns `PrepareProposal` and `ProcessProposal` wrappers that carry extended commit info through proposal
  bytes.
- `preblock/` owns the SDK preblock hook that calls module preblockers, applies oracle prices, syncs Tobin tax, and
  records preblock metrics.
- `oracle/` owns vote extraction, aggregation, validator scoring, price application, and oracle-specific encoding rules.
- `codec/` owns vote-extension and extended-commit codecs, including optional compression wrappers.
- `metrics/` and `oracle/metrics/` own package-level ABCI and oracle metrics.
- `types/` contains the narrow keeper/client interfaces and shared ABCI error types.
- `testutil/` contains reusable ABCI test fixtures and generated mocks.

## Block Flow

1. `ve.Handler.ExtendVoteHandler` asks the oracle client for prices and encodes them into the local validator vote
   extension. If the oracle is unavailable, the handler returns an empty extension to preserve liveness.
2. `ve.Handler.VerifyVoteExtensionHandler` accepts empty extensions and rejects non-empty extensions that cannot be
   decoded or do not pass oracle vote-extension validation.
3. `proposals.Handler.PrepareProposalHandler` injects the previous height's extended commit info into the proposal when
   vote extensions are enabled.
4. `proposals.Handler.ProcessProposalHandler` validates the injected extended commit info before accepting the proposal.
5. `preblock.Handler` decodes the injected commit info, aggregates oracle votes, writes exchange rates, updates
   validator accounting, and syncs Tobin tax before block transactions run.

## Ownership Rules

Keep lifecycle hooks thin. `ve`, `proposals`, and `preblock` should own ABCI request/response handling and delegate
oracle-specific decoding, aggregation, scoring, and write policy to `oracle/`.
