# Bitstamp API Provider

The Bitstamp API provider fetches spot prices from Bitstamp's public ticker endpoint.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Symbol Format

Bitstamp reports each market as `BASE/QUOTE` in the `pair` field, such as `USDT/USD` or `BTC/USD`. Configure that form
as the provider symbol. Ark maps chain denoms to these provider symbols through `Markets`; the adapter does not perform
denom conversion itself.

## Request Shape

The ticker endpoint returns every market in one response, so `Handler.BatchTickers` keeps all tickers in one request
whatever the configured batch size, and `Handler.CreateURL` returns the configured endpoint unchanged.
`Handler.ParseResponse` resolves only the requested pairs from the `last` field. Missing, malformed, or unexpected
values are returned as unresolved ticker errors.

## Market Checks

```sh
curl https://www.bitstamp.net/api/v2/trading-pairs-info/
curl https://www.bitstamp.net/api/v2/ticker/usdtusd/
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/api/bitstamp
```

## Response shape

[response.go](response.go) decodes an array of market objects; prices are decimal strings:

```json
[{"pair":"USDT/USD","last":"1.00010","bid":"1.0000","ask":"1.0002","timestamp":"1706302442"}]
```
