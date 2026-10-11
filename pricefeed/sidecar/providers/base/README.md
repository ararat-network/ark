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
or subscription messages the adapter cannot construct. Only a panic or a failed API ticker batching ends a provider's
run.

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
