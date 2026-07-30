# Oracle Preblock

The preblock package owns the SDK preblock hook for Ark oracle vote-extension data.

`Handler` runs before block transactions execute. It first calls the module manager's preblockers, then, when
vote extensions are enabled, applies oracle prices from the extended commit info injected into the proposal.

## Responsibilities

- Run the wrapped module manager preblock flow first.
- Skip oracle work when vote extensions are disabled.
- Delegate vote extraction, aggregation, scoring, and price writes to `abci/oracle.ProcessVoteExtensions`.
- Advance due feed transitions after processing the previous height's reports.
- Complete asset lifecycle transitions that were waiting on a price for their feed.
- Prime the Treasury liability snapshot once prices and feeds are final.
- Record preblock latency and consensus price metrics in finalize mode.

## Asset Lifecycle Completions

Between feed promotion and liability priming, the handler hands the asset registry the rates this block aggregated.
Assets carrying a requested completion — a `PENDING` activation or a `SUSPENDED` recovery — finish the transition when
their feed is among them. The evidence is deliberately this-block aggregation rather than the freshness window: a
completion rides a rate the fleet produced after governance asked for it, never the tail of `MaxExchangeRateAge`. A
block that aggregated nothing skips the call entirely, and failures are wrapped with `ErrAssetKeeper` and fail the
block. All lifecycle policy lives in `x/asset/keeper`; the boundary here is one method.

## Treasury Liability Priming

After prices and feeds are written, the handler calls Treasury's `PrimeLiabilitySnapshot`. This values the
aggregate stable liability once for the block, or records that valuation is unavailable, so transaction-time treasury
reads are block-position-independent and can be charged a flat gas fee instead of paying for a supply-and-rates scan
that grows with the asset registry. The call is intentionally thin: all valuation logic lives in
`x/treasury/keeper`, and failures are wrapped with `ErrTreasuryKeeper` and fail the block like any other preblock state
error.

Priming runs after the module manager's preblockers, so an upgrade migration that changes stable supply is reflected in
the same block's snapshot. It relies on only Market changing registered-asset supply during a block; module account
mint permissions enforce that, and IBC mints only `ibc/` voucher denoms rather than native ones.

## Boundaries

This package should stay focused on the ABCI lifecycle hook. Oracle policy belongs in `abci/oracle`: decoding proposal
bytes, aggregating rates, choosing the reference denom, writing exchange rates, and updating validator reward and
attendance accounting. Liability valuation policy belongs in `x/treasury/keeper`.
