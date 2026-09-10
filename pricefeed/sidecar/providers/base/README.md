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

`Run(ctx)` is the blocking provider lifecycle; the owner starts the goroutine, cancels it, and waits for return.
`GetPrices` returns copied results for configured pairs. Runtime, provider, and fetcher ownership are distinct: changing
market mappings must preserve the configured identity and the intended retained-price semantics while retargeting transport work.

API and WebSocket fetchers consume adapter contracts documented in [API providers](../api/README.md) and
[WebSocket providers](../websocket/README.md). Adapters own exchange requests/messages and parsing; they do not start a
second polling or reconnect loop. Endpoint transport/security policy is in [pricefeed operations](../../../../docs/PRICEFEED_OPERATIONS.md).

Run `go test ./pricefeed/sidecar/providers/base/...` from the root. Use injected fetchers/handlers and local TLS servers
for deterministic tests. Child `testutil/` packages contain generated mocks. For end-to-end provider lifecycle sampling,
use the harness described in the [provider testing section](../README.md#testing).
