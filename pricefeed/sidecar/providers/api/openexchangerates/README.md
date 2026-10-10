# Open Exchange Rates API Provider

The Open Exchange Rates API provider fetches fiat reference rates from the latest-rates endpoint. Rates are reference
observations, not an exchange trade stream; a result carries the local successful-response time even though the
response includes a provider timestamp.

[config.go](config.go) owns the registry name `openexchangerates_api`, the endpoint, an hourly poll that matches the
free plan's refresh and stays inside its monthly allowance, a 150-minute freshness window, the authentication template,
and the built-in markets, USD/XDR included. Endpoint authentication sends `Authorization: Token <app_id>`, scheme
prefix included; replace the template app ID before the provider can fetch useful data.

## Symbol Format

Tickers are `BASE/QUOTE`, such as `USD/KRW`. Shared [fiat helpers](../internal/fiat/fiat.go) normalise case and group
tickers by base. Prices retain the venue's pair orientation; the [resolver](../../../resolver/README.md) converts
observations into feed routes, so the adapter does not translate chain denominations itself.

## Request Shape

`Handler.CreateURL` sets comma-separated `symbols` for one same-base batch and `base` only when it is not USD, the
service default. `Handler.ParseResponse` maps `rates` back to the requested tickers. Missing requested tickers become
unresolved results; unrequested rows create no markets.

## Market Checks

```sh
curl -H 'Authorization: Token <app_id>' 'https://openexchangerates.org/api/latest.json?symbols=KRW,EUR'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from
the repository root; they cover batching, URL construction, malformed responses, and missing results without a live
account:

```sh
go test ./pricefeed/sidecar/providers/api/openexchangerates
```

Check upstream service support separately before changing endpoints or credentials; this document records the adapter
implementation, not a service-plan guarantee. [Operations](../../../../../docs/operations/PRICEFEED_OPERATIONS.md)
explains configuring provider credentials and routes.

## Response shape

[response.go](response.go) decodes the base and the rates map:

```json
{"timestamp":1760054400,"base":"USD","rates":{"KRW":1385.2,"EUR":0.85}}
```
