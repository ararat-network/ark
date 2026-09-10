# CurrencyBeacon API adapter

[handler.go](handler.go) implements the [API adapter contract](../README.md). [config.go](config.go) owns its registry
name, endpoint, request cadence, freshness defaults, authentication template where needed, and built-in market mappings.
[response.go](response.go) owns response decoding.

The adapter groups `BASE/QUOTE` tickers by base, sets `base` and comma-separated `symbols` query parameters, and maps
returned rates back to the requested tickers. Endpoint authentication sends `Authorization: Bearer <key>`; the value
includes the scheme prefix. The template key must be replaced before the provider can fetch useful data.

Prices retain the venue's pair orientation. Shared [fiat helpers](../internal/fiat/fiat.go) normalise symbols and batch
same-base tickers. The [resolver](../../../resolver/README.md) converts observations into feed routes; the adapter does
not translate chain denominations on its own. Missing requested tickers become unresolved results; unrequested rows do
not create additional configured markets.

From the root, run `go test ./pricefeed/sidecar/providers/api/currencybeacon`. Tests cover batching, URL construction, malformed
responses, and missing results without requiring a live external account. Check upstream service support separately before
changing endpoints or credentials; this document records the adapter implementation, not a service-plan guarantee.

[Provider construction](../../README.md) explains registration. [Operations](../../../../../docs/PRICEFEED_OPERATIONS.md)
explains configuring provider credentials and routes.
