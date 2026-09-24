# Kraken WebSocket Provider

The Kraken WebSocket provider subscribes to the Kraken websocket v2 ticker channel and publishes last-trade prices as
provider responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Requested symbols are subscribed to the `ticker` channel; symbols use Kraken's websocket names, such as `USDT/USD` or
`BTC/USD`. Several symbols may share one subscribe request, and Kraken acknowledges each symbol on its own. The
handler tells data pushes apart by `channel` and request responses by `method`:

- the `status` push sent on connect, reported as an error unless `system` is `online`
- `heartbeat` pushes, sent every second on a quiet connection
- `ticker` snapshots and updates, whose `data[].last` is the observation, decoded as a JSON number and kept as text
- `subscribe` responses, reported as errors when `success` is false rather than retried, because the venue answers
  a bad symbol the same way every time
- `pong` responses

The price is the last trade, matching the [Kraken API adapter](../../api/kraken/README.md). Upstream's v1 handler read
the day's volume-weighted average, which can trail the market by hours.

## Session State

The handler keeps a ticker cache per session; `Copy()` resets it for each new connection.

## Market Checks

Use the websocket v2 [instrument channel](https://docs.kraken.com/exchange/api-reference/spot-websocket-v2/instrument)
to discover symbols. Send this request to `wss://ws.kraken.com/v2` and read `data.pairs[].symbol` in its snapshot:

```json
{"method":"subscribe","params":{"channel":"instrument"}}
```

Websocket v2 uses symbols such as `BTC/USD`. The REST `AssetPairs.wsname` field supplies v1 names, such as
`XBT/USD`, which are not interchangeable with v2 symbols.

The REST ticker endpoint can be used to compare last-trade prices:

```sh
curl 'https://api.kraken.com/0/public/Ticker?pair=USDTUSD'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/kraken
```

## Wire reference

```json
{"method":"subscribe","params":{"channel":"ticker","symbol":["USDT/USD"]}}
{"channel":"status","type":"update","data":[{"version":"2.0.10","system":"online","api_version":"v2","connection_id":9471984923908980762}]}
{"method":"subscribe","result":{"channel":"ticker","event_trigger":"trades","snapshot":true,"symbol":"USDT/USD"},"success":true,"time_in":"2026-09-24T03:54:28.829165Z","time_out":"2026-09-24T03:54:28.829217Z"}
{"error":"Currency pair not supported NOPE/USD","method":"subscribe","success":false,"symbol":"NOPE/USD","time_in":"2026-09-24T03:54:32.996743Z","time_out":"2026-09-24T03:54:32.996775Z"}
{"channel":"ticker","type":"snapshot","data":[{"symbol":"USDT/USD","bid":0.99980,"bid_qty":44620.68691883,"ask":0.99981,"ask_qty":1087605.70466858,"last":0.99981,"volume":270638277.50461281,"vwap":0.99983,"low":0.99959,"high":1.00000,"change":0.00005,"change_pct":0.01,"trades":38144,"timestamp":"2026-09-24T03:54:18.054886Z"}]}
{"channel":"heartbeat"}
```

The complete upstream contracts are the [websocket v2 ticker](https://docs.kraken.com/api/docs/websocket-v2/ticker) and
[status](https://docs.kraken.com/api/docs/websocket-v2/status) channels.
