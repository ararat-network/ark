# CurrencyBeacon API Provider

The CurrencyBeacon API provider fetches fiat reference rates from CurrencyBeacon's latest-rates endpoint. Rates are
reference observations, not an exchange trade stream; a result carries the local successful-response time.

[config.go](config.go) owns the registry name `currencybeacon_api`, the endpoint, a 15-minute poll inside the free
plan's monthly request allowance, a 35-minute freshness window, the authentication template, and the built-in markets.
Endpoint authentication sends `Authorization: Bearer <key>`, scheme prefix included; replace the template key before
the provider can fetch useful data. USD/XDR is absent from the built-in markets by design: CurrencyBeacon lists no XDR,
so the reference prices through the other two fiat providers.

## Symbol Format

Tickers are `BASE/QUOTE`, such as `USD/KRW`. Shared [fiat helpers](../internal/fiat/fiat.go) normalise case and group
tickers by base. Prices retain the venue's pair orientation; the [resolver](../../../resolver/README.md) converts
observations into feed routes, so the adapter does not translate chain denominations itself.

## Request Shape

`Handler.CreateURL` sets the `base` and comma-separated `symbols` query parameters for one same-base batch.
`Handler.ParseResponse` fails the whole batch when `meta.code` is not 200, then maps `response.rates` back to the
requested tickers. Missing requested tickers become unresolved results; unrequested rows create no markets.

## Market Checks

```sh
curl -H 'Authorization: Bearer <key>' 'https://api.currencybeacon.com/v1/latest?base=USD&symbols=KRW,EUR'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from
the repository root; they cover batching, URL construction, malformed responses, and missing results without a live
account:

```sh
go test ./pricefeed/sidecar/providers/api/currencybeacon
```

Check upstream service support separately before changing endpoints or credentials; this document records the adapter
implementation, not a service-plan guarantee. [Operations](../../../../../docs/operations/PRICEFEED_OPERATIONS.md)
explains configuring provider credentials and routes.

## Response shape

[response.go](response.go) decodes the status envelope and the rates map:

```json
{"meta":{"code":200},"response":{"date":"2026-10-10","base":"USD","rates":{"KRW":1385.2,"EUR":0.85}}}
```
