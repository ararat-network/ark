# Shared provider runtime and fetchers

[Provider](provider.go) runs a fetcher over provider-specific tickers and exposes pair-keyed cached results.
The [provider registry](../README.md) constructs it; [sidecar runtime](../../runtime/README.md) owns its run and cancellation.

| File / directory | Responsibility |
| --- | --- |
| [provider.go](provider.go), [fetcher.go](fetcher.go) | Provider run lifecycle and the fetcher boundary. |
| [receive.go](receive.go) | Response ingestion and cached observations. |
| [update.go](update.go) | Market/ticker changes and retained cache state. |
| [api/](api/) | Polling, handler-defined batching, endpoint selection, timing, and publication. |
| [websocket/](websocket/) | Dial/reconnect, sharding, subscriptions, heartbeats, I/O timeouts, and publication. |
| [metrics/](metrics/) | Shared provider instruments; transports own their child metrics. |
| [types.go](types.go), [options.go](options.go) | `TransportType` and construction options. |

`Run(ctx)` is the blocking provider lifecycle; the owner starts the goroutine, cancels it, and waits for return.
`GetPrices` returns copied results for configured pairs. Runtime, provider, and fetcher ownership are distinct: changing
market mappings must preserve the configured identity and the intended retained-price semantics while retargeting transport work.

## Failed fetches

A fetch that fails publishes its tickers as unresolved, counted by error code in `ark.pricefeed.provider.responses`,
and is retried: an API batch on its next interval, a WebSocket shard after its reconnect delay. That holds for failures
before anything is sent too, such as a URL the adapter cannot build, a request that would leave its configured origin,
or subscription messages the adapter cannot construct. A failed API ticker batching ends that provider's run. A panic in
any provider stops the whole sidecar: the [runtime](../../runtime/README.md) cancels every provider and the process
exits, since a provider in an unknown state is not one to keep pricing from, so run the sidecar under a supervisor that
restarts it.

A failed request moves the API batch to its next configured endpoint, and every WebSocket reconnect dials the next one,
wrapping round; a single endpoint is simply retried.

Some venues refuse a whole API request over one symbol they do not list: Binance answers HTTP 400, Kraken an unknown
pair error. When a batch of several tickers is refused outright (an unknown pair, or HTTP 400, 404 or 422 for every
ticker), the fetcher asks for each ticker alone once, sets aside those refused alone, and requests the rest together.
A set-aside ticker is published unresolved as an unknown pair every interval without being requested, retried alone
every five minutes, and rejoins the batch once answered. A batch refused alone as well as together is not one symbol's
doing, so nothing is set aside, and isolation waits five minutes before it runs again.

## Unchanged results

A venue heartbeat that vouches for a subscription reports an unchanged result: no price, only a timestamp. [receive.go](receive.go)
applies it to the cached result by moving `Timestamp` and leaving `LastObserved` alone, and drops it when no price is
cached yet; a result neither unchanged nor priced is dropped too. A dropped result is counted under its reason, not as a
success. The heartbeat says the price still holds, not that it was seen again, and the two timestamps are what let
the [sidecar runtime](../../runtime/README.md#freshness) bound how long a heartbeat may carry a price.

Results apply in arrival order. Each ticker has one producer delivering in order, and timestamps are local wall-clock
times, so refusing a result stamped before the cached one would only ever fire after the clock stepped back, freezing
the cache while the runtime read the frozen price's future stamp as fresh. A priced result's `LastObserved` is never
later than its `Timestamp`; one left unset is taken as observed on arrival.

API and WebSocket fetchers consume adapter contracts documented in [API providers](../api/README.md) and
[WebSocket providers](../websocket/README.md). Adapters own exchange requests/messages and parsing; they do not start a
second polling or reconnect loop. Endpoint transport/security policy is in [pricefeed operations](../../../../docs/operations/PRICEFEED_OPERATIONS.md).

Run `go test ./pricefeed/sidecar/providers/base/...` from the root. Use injected fetchers/handlers and local TLS servers
for deterministic tests. Child `testutil/` packages contain generated mocks. For end-to-end provider lifecycle sampling,
use the harness described in the [provider testing section](../README.md#testing).
