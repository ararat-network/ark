# Coinbase API Provider

The Coinbase API provider fetches spot prices from the Coinbase v2 prices endpoint.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Symbol Format

Coinbase products use the `BASE-QUOTE` form, such as `USDT-USD` or `BTC-USD`. Ark maps chain denoms to these provider
symbols through `Markets`; the adapter does not perform denom conversion itself.

## Request Shape

The spot endpoint prices one product per request, so `Handler.BatchTickers` puts each ticker in its own request
whatever the configured batch size, and `Handler.CreateURL` appends `/<product>/spot` to the configured endpoint.
`Handler.ParseResponse` reads `data.amount` and requires `data.currency` to be the product's quote, so a response for
the wrong product is refused. The public v2 API allows 10,000 requests per hour per IP; the default
interval polls each product once a second, so size the market set accordingly.

## Market Checks

```sh
curl https://api.exchange.coinbase.com/products
curl https://api.coinbase.com/v2/prices/USDT-USD/spot
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/api/coinbase
```

## Response shape

[response.go](response.go) decodes the `data` envelope; the amount is a decimal string:

```json
{"data":{"amount":"1.0001","base":"USDT","currency":"USD"}}
```
