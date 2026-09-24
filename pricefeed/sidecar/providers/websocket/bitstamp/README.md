# Bitstamp WebSocket Provider

The Bitstamp WebSocket provider subscribes to Bitstamp live trades channels and publishes each trade's price as a
provider response.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Each requested market gets its own `bts:subscribe` message for `live_trades_<market>`; Bitstamp does not batch
subscriptions. The market is the lower-case symbol, such as `usdtusd`. The handler processes:

- `bts:heartbeat` echoes of the client heartbeat
- `bts:subscription_succeeded` confirmations
- `bts:request_reconnect` warnings, which are logged; the server closes the connection shortly after and the shared
  fetcher reconnects on the failed read
- `bts:error` notices, reported with the venue's code
- `trade` events, whose `price_str` is the observation

The client heartbeat runs on the fetcher's ping interval, which Bitstamp needs to keep an idle connection open.

## Session State

The handler keeps a ticker cache per session; `Copy()` resets it for each new connection.

## Market Checks

```sh
curl https://www.bitstamp.net/api/v2/trading-pairs-info/
curl https://www.bitstamp.net/api/v2/ticker/usdtusd/
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/bitstamp
```

## Wire reference

```json
{"event":"bts:subscribe","data":{"channel":"live_trades_usdtusd"}}
{"event":"bts:subscription_succeeded","channel":"live_trades_usdtusd","data":{}}
{"event":"bts:heartbeat"}
{"event":"trade","channel":"live_trades_usdtusd","data":{"id":317319339,"price":0.9999,"price_str":"0.99990","type":0}}
```

The complete upstream contract is the [Bitstamp WebSocket v2 API](https://www.bitstamp.net/websocket/v2/).
