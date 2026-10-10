# Frankfurter API Provider

The Frankfurter API provider fetches fiat reference rates from Frankfurter's v2 rates endpoint. Rates are reference
observations, not an exchange trade stream; a result carries the local successful-response time, not the source
publication date.

[config.go](config.go) owns the registry name `frankfurter_api`, the endpoint, a one-minute poll, a 150-second
freshness window, and the built-in markets, USD/XDR included. This adapter requires no API key.

## Symbol Format

Tickers are `BASE/QUOTE`, such as `USD/KRW`. Shared [fiat helpers](../internal/fiat/fiat.go) normalise case and group
tickers by base. Prices retain the venue's pair orientation; the [resolver](../../../resolver/README.md) converts
observations into feed routes, so the adapter does not translate chain denominations itself.

## Request Shape

`Handler.CreateURL` sets the `base` and comma-separated `quotes` query parameters for one same-base batch.
`Handler.ParseResponse` reads the returned base/quote/rate rows and maps them back to the requested tickers. Missing
requested tickers become unresolved results; unrequested rows create no markets.

## Market Checks

```sh
curl 'https://api.frankfurter.dev/v2/rates?base=USD&quotes=KRW,EUR'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from
the repository root; they cover batching, URL construction, malformed responses, and missing results without a live
service:

```sh
go test ./pricefeed/sidecar/providers/api/frankfurter
```

Check upstream service support separately before changing endpoints; this document records the adapter implementation,
not a service-plan guarantee. [Operations](../../../../../docs/operations/PRICEFEED_OPERATIONS.md) explains configuring
provider routes.

## Response shape

[response.go](response.go) decodes a list of rate rows:

```json
[{"date":"2026-10-10","base":"USD","quote":"KRW","rate":1385.2},{"date":"2026-10-10","base":"USD","quote":"EUR","rate":0.85}]
```
