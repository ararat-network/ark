# Open Exchange Rates API adapter

[handler.go](handler.go) implements the [API adapter contract](../README.md). [config.go](config.go) owns its registry
name, endpoint, request cadence, freshness defaults, authentication template where needed, and built-in market mappings.
[response.go](response.go) owns response decoding.

The adapter groups `BASE/QUOTE` tickers by base and requests comma-separated `symbols`. It omits `base` for the USD
default and supplies it for other bases. Endpoint authentication sends `Authorization: Token <app_id>` with the scheme
prefix included; replace the template app ID. The adapter uses its local successful-response time for cached results,
even though the response schema includes a provider timestamp.

Prices retain the venue's pair orientation. Shared [fiat helpers](../internal/fiat/fiat.go) normalise symbols and batch
same-base tickers. The [resolver](../../../resolver/README.md) converts observations into feed routes; the adapter does
not translate chain denominations on its own. Missing requested tickers become unresolved results; unrequested rows do
not create additional configured markets.

From the root, run `go test ./pricefeed/sidecar/providers/api/openexchangerates`. Tests cover batching, URL construction, malformed
responses, and missing results without requiring a live external account. Check upstream service support separately before
changing endpoints or credentials; this document records the adapter implementation, not a service-plan guarantee.

[Provider construction](../../README.md) explains registration. [Operations](../../../../../docs/PRICEFEED_OPERATIONS.md)
explains configuring provider credentials and routes.
