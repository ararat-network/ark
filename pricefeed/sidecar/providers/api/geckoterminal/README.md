# GeckoTerminal API Provider

The GeckoTerminal API provider fetches on-chain token prices in USD from GeckoTerminal's simple token price endpoint.
GeckoTerminal aggregates decentralised-exchange pools; treat it as a secondary source.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key. The
network is part of the endpoint path, so one provider prices one network; the default endpoint is Ethereum mainnet.
Configure another network by changing the endpoint path, for example `.../networks/base/token_price`.

## Symbol Format

A provider symbol is the token's contract address on the configured network, such as
`0xdac17f958d2ee523a2206206994597c13d831ec7` for USDT on Ethereum. Prices are in USD. Ark maps chain denoms to these
provider symbols through `Markets`; the adapter does not perform denom conversion itself.

## Request Shape

`Handler.CreateURL` appends the comma-joined addresses as the last path segment. The endpoint prices up to 30
addresses per request; `Handler.BatchTickers` caps the configured `batch_size` there, so a zero or larger value still
fits. `Handler.ParseResponse` requires `data.type` to be `simple_token_price` and reads `data.attributes.token_prices`.
Missing, malformed, or unexpected values are returned as unresolved ticker errors.

## Market Checks

```sh
curl https://api.geckoterminal.com/api/v2/networks
curl https://api.geckoterminal.com/api/v2/simple/networks/eth/token_price/0xdac17f958d2ee523a2206206994597c13d831ec7
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/api/geckoterminal
```

## Response shape

[response.go](response.go) decodes the typed `data` envelope; prices are decimal strings keyed by address:

```json
{"data":{"id":"1","type":"simple_token_price","attributes":{"token_prices":{"0xdac17f958d2ee523a2206206994597c13d831ec7":"0.999806870339427"}}}}
```
