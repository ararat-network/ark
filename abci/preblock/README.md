# Oracle Preblock

The preblock package owns the SDK preblock hook for Noah oracle vote-extension data.

`Handler` runs before block transactions execute. It first calls the module manager's preblockers, then, when
vote extensions are enabled, applies oracle prices from the extended commit info injected into the proposal.

## Responsibilities

- Run the wrapped module manager preblock flow first.
- Skip oracle work when vote extensions are disabled.
- Delegate vote extraction, aggregation, scoring, and price writes to `abci/oracle.PriceApplier`.
- Sync Tobin tax from the vote targets used for the block.
- Record preblock latency, price, and validator-report metrics in finalize mode.

## Boundaries

This package should stay focused on the ABCI lifecycle hook. Oracle policy belongs in `abci/oracle`: decoding proposal
bytes, aggregating rates, choosing the reference denom, writing exchange rates, and updating validator score/miss
accounting.
