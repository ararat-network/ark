# Oracle Preblock

The preblock package owns the SDK preblock hook for Ark oracle vote-extension data.

`Handler` runs before block transactions execute. It first calls the module manager's preblockers, then, when
vote extensions are enabled, applies oracle prices from the extended commit info injected into the proposal.

## Responsibilities

- Run the wrapped module manager preblock flow first.
- Skip oracle work when vote extensions are disabled.
- Delegate vote extraction, aggregation, scoring, and price writes to `abci/oracle.ProcessVoteExtensions`.
- Advance pending vote targets after processing the previous height's reports.
- Prime the Treasury liability snapshot once prices and vote targets are final.
- Record preblock latency and consensus price metrics in finalize mode.

## Treasury Liability Priming

After prices and vote targets are written, the handler calls Treasury's `PrimeLiabilitySnapshot`. This values the
aggregate stable liability once for the block, or records that valuation is unavailable, so transaction-time treasury
reads are block-position-independent and can be charged a flat gas fee instead of paying for a supply-and-rates scan
that grows with the oracle whitelist. The call is intentionally thin: all valuation logic lives in
`x/treasury/keeper`, and failures are wrapped with `ErrTreasuryKeeper` and fail the block like any other preblock state
error.

Priming runs after the module manager's preblockers, so an upgrade migration that changes stable supply is reflected in
the same block's snapshot. It relies on only Market changing tobin-denom supply during a block; module account mint
permissions enforce that, and IBC mints only `ibc/` voucher denoms rather than native ones.

## Boundaries

This package should stay focused on the ABCI lifecycle hook. Oracle policy belongs in `abci/oracle`: decoding proposal
bytes, aggregating rates, choosing the reference denom, writing exchange rates, and updating validator reward and
attendance accounting. Liability valuation policy belongs in `x/treasury/keeper`.
