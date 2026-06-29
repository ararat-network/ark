# Oracle Providers

This package contains Noah oracle price provider construction and provider-specific adapters.

The shared runtime lives in `base/`. The public constructor in `factory.go` selects a transport-specific fetcher from
the provider config, builds the matching provider adapter, and returns a `base.Provider`. Provider packages under
`api/` and `websocket/` should stay focused on exchange-specific request, message, and response handling.

## Package Layout

- `factory.go` wires a `base.Config` to the correct API or WebSocket fetcher.
- `api/` contains HTTP API provider adapters.
- `websocket/` contains WebSocket provider adapters.
- `base/` contains the provider runtime, API fetcher, WebSocket fetcher, metrics, and shared lifecycle logic.
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
provider-specific exchange behavior in `api/<provider>/` or `websocket/<provider>/`, and add only the construction
branch needed in `factory.go`.

## Adding a Provider

1. Add provider-specific config defaults near the provider adapter.
2. Implement the relevant data handler interface.
3. Add a branch in `factory.go` that selects the handler and default fetcher config.
4. Add market mappings in the provider `base.Config` used by callers.
5. Add focused tests for parsing, request/message creation, and the fetcher boundary touched by the adapter.
