# Kraken API Provider

The Kraken API provider fetches spot prices from Kraken's public ticker endpoint.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Symbol Format

Kraken responses are keyed by the full pair name, such as `USDTZUSD` or `XXBTZUSD`, even when the request used the
alternate name (`USDTUSD`). Configure the full name as the provider symbol so the request and the response match. Ark
maps chain denoms to these provider symbols through `Markets`; the adapter does not perform denom conversion itself.

## Request Shape

`Handler.CreateURL` joins the requested pairs into the `pair` query parameter, so a batch is one request.
`Handler.ParseResponse` reads the first element of each pair's `c` (last trade closed) array. A non-empty `error`
array fails every ticker in the batch with the venue's message; missing, malformed, or unexpected values are returned
as unresolved ticker errors.

## Market Checks

```sh
curl https://api.kraken.com/0/public/AssetPairs
curl 'https://api.kraken.com/0/public/Ticker?pair=USDTUSD'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/api/kraken
```

## Response shape

[response.go](response.go) decodes the error list and the per-pair result map:

```json
{"error":[],"result":{"USDTZUSD":{"a":["1.00010000","1","1.000"],"c":["1.00000000","100.5"]}}}
```
