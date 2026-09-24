# CoinMarketCap API Provider

The CoinMarketCap API provider fetches aggregated USD quotes from CoinMarketCap's latest quotes endpoint. CoinMarketCap
is an aggregator, not a venue; treat it as a secondary source.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires an API key, sent
in the `X-CMC_PRO_API_KEY` header through endpoint authentication. The default config ships a placeholder key that must
be replaced before the provider can fetch anything.

## Symbol Format

A provider symbol is the CoinMarketCap numeric id of the asset, such as `825` for Tether or `1` for Bitcoin. Every
quote is read in USD. Ark maps chain denoms to these provider symbols through `Markets`; the adapter does not perform
denom conversion itself.

## Request Shape

`Handler.CreateURL` joins the requested ids into the `id` query parameter, so a batch is one request.
`Handler.ParseResponse` reads `data.<id>.quote.USD.price`, decoded as a JSON number and kept as text. A non-zero
`status.error_code` fails every ticker in the batch with the venue's message; missing or malformed values are returned
as unresolved ticker errors.

## Market Checks

```sh
curl -H 'X-CMC_PRO_API_KEY: <key>' 'https://pro-api.coinmarketcap.com/v1/cryptocurrency/map?symbol=USDT'
curl -H 'X-CMC_PRO_API_KEY: <key>' 'https://pro-api.coinmarketcap.com/v2/cryptocurrency/quotes/latest?id=825'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/api/coinmarketcap
```

## Response shape

[response.go](response.go) decodes the per-id data and the request status:

```json
{"data":{"825":{"id":825,"symbol":"USDT","quote":{"USD":{"price":1.0001}}}},"status":{"error_code":0,"error_message":""}}
```
