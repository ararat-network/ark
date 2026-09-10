# Oracle Providers

This package contains Ark oracle price provider construction and provider-specific adapters.

The shared runtime lives in `base/`. Construction goes through a `Registry` in `registry.go`: `NewProvider` validates a
`providers.Config`, looks up the handler factory registered for the provider name, builds the configured API or
WebSocket fetcher around it, and returns a `base.Provider`. `DefaultRegistry()` carries the in-tree providers; custom
sidecar binaries register additional handler factories on their own registry before starting the runtime. Provider
packages under `api/` and `websocket/` should stay focused on exchange-specific request, message, and response handling.

## Package Layout

- `config.go` defines the provider-level config, including provider identity, markets, and transport-specific config.
- `registry.go` maps provider names to handler factories and wires a `providers.Config` to the correct API or WebSocket
  fetcher.
- `api/` contains HTTP API provider adapters.
- `websocket/` contains WebSocket provider adapters.
- `base/` contains the provider runtime, transport fetcher interfaces, metrics, and shared lifecycle logic.
- `base/api/` and `base/websocket/` contain the shared API and WebSocket fetcher implementations.
- `types/` contains shared provider values such as tickers, endpoints, markets, responses, and errors.
- `providertest/` contains helpers for tests that need runnable provider instances.

## Runtime boundary

[base/README.md](base/README.md) owns provider/cache/fetcher lifecycle. [API adapters](api/README.md) and
[WebSocket adapters](websocket/README.md) own their distinct extension contracts. Keep exchange-specific requests,
messages, and parsing in adapters, and lifecycle machinery in shared fetchers.

## Adding a Provider

1. Add provider-specific config defaults near the provider adapter.
2. Implement the relevant data handler interface.
3. Register the handler factory: in `DefaultRegistry()` for an in-tree provider, or on your own registry (passed via
   `sidecar.WithRegistry` or `runtime.WithProviderRegistry`) for an out-of-tree provider.
4. Add market mappings in the provider `providers.Config` used by callers.
5. Add focused tests for parsing, request/message creation, and the fetcher boundary touched by the adapter.

## Testing

Use focused parser/request tests for an adapter and the [shared fetcher tests](base/README.md) for lifecycle changes.
From the root, run `go test ./pricefeed/sidecar/providers/...` for the whole provider tree.

[providertest](providertest/provider.go) samples a real provider lifecycle built by the caller. For example, inside a
Go test with a configured registry entry:

```go
registry := providers.DefaultRegistry()
results, err := providertest.Run(ctx, func(context.Context) (*base.Provider, error) {
    return registry.NewProvider(cfg, cfg.Markets, logger)
}, providertest.DefaultProviderTestConfig())
```

Here `cfg` is a validated provider configuration and `logger` is the caller-supplied logger; construction is defined in [registry.go](registry.go).
`RunProvider` runs `provider.Run(ctx)`, samples its copied prices, then cancels and waits for cleanup. Test builders and
stub fetchers can supply fake providers without registering a production fake exchange. Network sampling is a manual
integration check; deterministic unit tests should inject responses or local transports.

## Related policy

[Pricefeed operations](../../../docs/operations/PRICEFEED_OPERATIONS.md) owns endpoint security, TLS, configuration, and releases.
[The sidecar guide](../README.md) maps service ownership. [Process monitoring](../../../docs/operations/PROCESS_MONITORING.md) owns metric interpretation.
