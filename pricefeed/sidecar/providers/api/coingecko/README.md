# CoinGecko API Provider

The CoinGecko API provider fetches aggregated prices from CoinGecko's simple price endpoint. CoinGecko is an aggregator,
not a venue; treat it as a secondary source.

[config.go](config.go) owns the provider name, endpoints and transport defaults. The public endpoint needs no API key but
is rate limited, so the default interval is slow. A demo key goes in the `x-cg-demo-api-key` header on the public
endpoint; a pro key goes in the `x-cg-pro-api-key` header on the pro endpoint. Set the header and key through endpoint
authentication:

```toml
[[providers.coingecko_api.api.endpoints]]
url = "https://pro-api.coingecko.com/api/v3/simple/price"
[providers.coingecko_api.api.endpoints.authentication]
api_key = "<key>"
api_key_header = "x-cg-pro-api-key"
```

## Symbol Format

A provider symbol is the CoinGecko coin id, a slash, and the quote currency, such as `tether/usd`. Case does not
matter: the request sends both parts lower-case, as CoinGecko keys them. Ark maps chain denoms to these provider
symbols through `Markets`; the adapter does not perform denom conversion itself.

## Request Shape

`Handler.CreateURL` sends the unique coin ids in `ids` and the unique quote currencies in `vs_currencies`, with
`precision=18`. The endpoint prices every id against every quote, so `Handler.ParseResponse` keeps only the requested
combinations. Prices are decoded as JSON numbers and kept as text so the requested precision survives. Missing values
are returned as unresolved ticker errors.

## Market Checks

```sh
curl https://api.coingecko.com/api/v3/coins/list
curl 'https://api.coingecko.com/api/v3/simple/price?ids=tether&vs_currencies=usd&precision=18'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/api/coingecko
```

## Response shape

[response.go](response.go) decodes a map of coin id to quote currency to price:

```json
{"tether":{"usd":0.999123456789012345},"bitcoin":{"usd":67734.5}}
```
