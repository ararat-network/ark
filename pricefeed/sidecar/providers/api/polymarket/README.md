# Polymarket API Provider

The Polymarket API provider fetches outcome-token prices from the Polymarket CLOB markets endpoint. A price is the
market's probability for that outcome, between 0 and 1, quoted in USDC.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Symbol Format

A provider symbol names the market and the outcome token: the market's condition id, a slash, and the token id, such
as `0x08f5...0925/95128...0759`. Both ids come from the markets endpoint. There are no built-in market mappings; every
market is specific to a question. Ark maps chain denoms to these provider symbols through `Markets`; the adapter does
not perform denom conversion itself.

## Request Shape

The markets endpoint describes one market per request, so `Handler.BatchTickers` puts each ticker in its own request
whatever the configured batch size, and `Handler.CreateURL` appends the condition id to the configured endpoint.
`Handler.ParseResponse` finds the requested token in `tokens` and reads its `price`, decoded as a JSON number and
kept as text. A zero price is floored to `0.0001`, because the resolver drops non-positive prices and a settled
outcome should stay observable. A token absent from the response is no response; a token without a price, or with a
malformed one, is an unresolved ticker error.

## Market Checks

```sh
curl https://clob.polymarket.com/markets/0x08f5fe8d0d29c08a96f0bc3dfb52f50e0caf470d94d133d95d38fa6c847e0925
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/api/polymarket
```

## Response shape

[response.go](response.go) decodes the market's `tokens` array; other market fields are ignored:

```json
{"condition_id":"0x08f5...0925","tokens":[{"token_id":"95128...0759","outcome":"Yes","price":0.735},{"token_id":"50107...7986","outcome":"No","price":0.265}]}
```
