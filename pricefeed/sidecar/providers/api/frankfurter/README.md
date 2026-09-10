# Frankfurter API adapter

[handler.go](handler.go) implements the [API adapter contract](../README.md). [config.go](config.go) owns its registry
name, endpoint, request cadence, freshness defaults, authentication template where needed, and built-in market mappings.
[response.go](response.go) owns response decoding.

The adapter groups `BASE/QUOTE` tickers by base and sends `base` plus comma-separated `quotes` query parameters.
It parses the returned base/quote/rate rows. It requires no API credential. Rates are reference observations rather than
an exchange trade stream; parser freshness uses the local successful-response timestamp, not the source publication date.

Prices retain the venue's pair orientation. Shared [fiat helpers](../internal/fiat/fiat.go) normalise symbols and batch
same-base tickers. The [resolver](../../../resolver/README.md) converts observations into feed routes; the adapter does
not translate chain denominations on its own. Missing requested tickers become unresolved results; unrequested rows do
not create additional configured markets.

From the root, run `go test ./pricefeed/sidecar/providers/api/frankfurter`. Tests cover batching, URL construction, malformed
responses, and missing results without requiring a live external account. Check upstream service support separately before
changing endpoints or credentials; this document records the adapter implementation, not a service-plan guarantee.

[Provider construction](../../README.md) explains registration. [Operations](../../../../../docs/PRICEFEED_OPERATIONS.md)
explains configuring provider credentials and routes.
