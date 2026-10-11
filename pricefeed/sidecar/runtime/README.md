# Sidecar runtime

`Runtime` owns the price-fetch loop and reloadable price configuration. It has no public listener; the
[parent service](../README.md) calls it and exposes its snapshots over RPC.

## Flow and ownership

1. [runtime.go](runtime.go) and [config.go](config.go) assemble validated configuration and collaborators.
2. [lifecycle.go](lifecycle.go) starts providers and chain-state polling. `Run(ctx)` is blocking and single-use; it waits
   for owned collaborators to clean up on cancellation or fatal child failure.
3. [feeds.go](feeds.go) reconciles the latest chain feed set. Read failures preserve the current set; a successful empty
   set is a real update. Fallback feeds provide the configured starting set until chain state is available.
4. [prices.go](prices.go) synchronises feeds, reads fresh provider caches, calls the resolver, and commits one snapshot.
   Missing feeds remain absent; [freshness](#freshness) filtering happens before aggregation.
5. [update.go](update.go) serialises replacement with aggregation and lifecycle changes. It builds replacement providers
   and loads client material before changing live state, so preparation errors leave the active runtime intact. A changed
   `update_interval` resets the fetch ticker at once rather than after the old interval elapses.

## Freshness

Each provider's cached result passes two windows before it reaches the resolver. `max_price_age` bounds the age of
`Timestamp`, which every result refreshes. `max_unchanged_age` bounds the age of `LastObserved`, which only a real
price refreshes; it is zero, which allows no extension, or at least `max_price_age`, and validation refuses anything
between. A heartbeat-driven [unchanged result](../providers/base/README.md#unchanged-results) moves the first and not
the second, so the difference between the two windows is how long a heartbeat may extend a price. A provider whose
heartbeat vouches for the connection rather than the subscription warrants a small difference or none.

Both windows run from arrival: a result is stamped when the sidecar receives it, so `max_price_age` bounds how long ago
the venue last reported the price, not when the price last traded. A ticker stream re-sending its last price, a
subscription snapshot, and an API poll are all reports, and fiat references publish daily or hourly, so no venue clock
could serve as the bound for them. On a quiet market a last-price stream therefore keeps an old trade's price fresh;
set `max_price_age` with that in mind. Coinbase's matches are the exception: they carry their trade time, which dates
`LastObserved` (never later than arrival), so the last match Coinbase repeats on subscribe ages from its trade.

`updateMu` serialises aggregation/config/lifecycle transitions. [provider.go](provider.go) holds cancellation and completion
state for owned provider runs, including retained providers whose markets change. Snapshot ownership is explicit: callers
must not mutate maps retained by runtime. Preserve these boundaries when changing config slices or shared maps.

[Chain-state client](../chainstate/README.md), [shared providers](../providers/base/README.md), and
[resolver](../resolver/README.md) document their independent contracts. [Pricefeed operations](../../../docs/operations/PRICEFEED_OPERATIONS.md)
explains what can reload and which process settings require restart.

Run `go test ./pricefeed/sidecar/runtime/...` from the root. Lifecycle, update, feed reconciliation, and snapshot tests use
injected collaborators; `testutil/` provides chain-state mocks. Use a focused race run when changing locks or ownership.
