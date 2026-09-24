# Coinbase WebSocket Provider

The Coinbase WebSocket provider subscribes to the Coinbase Exchange ticker and heartbeat channels and publishes match
prices as provider responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Requested products are subscribed to the `ticker` and `heartbeat` channels in one subscribe message per product.
Products use the `BASE-QUOTE` form, such as `USDT-USD`. The subscribe message must be sent within five seconds of
connecting. The handler processes:

- `subscriptions` listings of the channels held
- `error` notices, reported with the venue's reason
- `ticker` matches, whose `price` is the observation; the `trade_id` is recorded for the heartbeats that follow
- `heartbeat` messages, sent every second per product; one whose `last_trade_id` matches the last match seen resolves
  an unchanged result, which the provider's `max_unchanged_age` decides whether to honour, and one naming another
  trade means a match was missed

Every message carries a `sequence`; a message older than the last seen for its product is refused so a late match
cannot roll a price back.

## Session State

The handler keeps a ticker cache plus per-product sequence and trade id maps; `Copy()` resets them for each new
connection.

## Market Checks

```sh
curl https://api.exchange.coinbase.com/products
curl https://api.exchange.coinbase.com/products/USDT-USD/ticker
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/coinbase
```

## Wire reference

```json
{"type":"subscribe","product_ids":["USDT-USD"],"channels":["ticker","heartbeat"]}
{"type":"subscriptions","channels":[{"name":"ticker","product_ids":["USDT-USD"]},{"name":"heartbeat","product_ids":["USDT-USD"]}]}
{"type":"ticker","sequence":37475248783,"product_id":"USDT-USD","price":"1.0001","trade_id":370843401,"time":"2022-10-19T23:28:22.061769Z"}
{"type":"heartbeat","sequence":90,"last_trade_id":370843401,"product_id":"USDT-USD","time":"2014-11-07T08:19:28.464459Z"}
```

The complete upstream contracts are the [websocket overview](https://docs.cdp.coinbase.com/exchange/docs/websocket-overview),
the [ticker channel](https://docs.cdp.coinbase.com/exchange/docs/websocket-channels#ticker-channel), and the
[heartbeat channel](https://docs.cdp.coinbase.com/exchange/docs/websocket-channels#heartbeat-channel).
