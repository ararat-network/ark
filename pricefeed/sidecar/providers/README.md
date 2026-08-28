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

## Runtime Boundary

Provider adapters do not own provider lifecycle. They implement the data-handling boundary consumed by the shared
fetchers:

- API adapters implement `api.DataHandler` by constructing request URLs and parsing HTTP responses.
- WebSocket adapters implement `websocket.DataHandler` by constructing subscription messages, handling incoming
  messages, creating heartbeat messages when needed, and returning an independent handler from `Copy()` for each
  connection session.

`base.Provider` owns ticker resolution, response ingestion, cached prices, runtime updates, and the fetch loop. Keep new
provider-specific exchange behavior in `api/<provider>/` or `websocket/<provider>/`, and add only the handler-factory
registration needed in the registry.

## Adding a Provider

1. Add provider-specific config defaults near the provider adapter.
2. Implement the relevant data handler interface.
3. Register the handler factory: in `DefaultRegistry()` for an in-tree provider, or on your own registry (passed via
   `sidecar.WithRegistry` or `runtime.WithProviderRegistry`) for an out-of-tree provider.
4. Add market mappings in the provider `providers.Config` used by callers.
5. Add focused tests for parsing, request/message creation, and the fetcher boundary touched by the adapter.
