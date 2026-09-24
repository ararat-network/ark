# Crypto.com WebSocket Provider

The Crypto.com WebSocket provider subscribes to Crypto.com Exchange ticker channels and publishes latest-trade prices
as provider responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Requested instruments are subscribed as `ticker.<instrument>` channels, one per subscribe request with a sequential
id. Instruments use the `BASE_QUOTE` form, such as `USDT_USD`. Crypto.com asks clients to wait one second after
connecting before subscribing, which the default post-connection timeout does. The handler processes:

- `public/heartbeat` messages, sent by the server every 30 seconds, which are answered with
  `public/respond-heartbeat` and the same id; an unanswered heartbeat closes the connection
- `subscribe` messages: an acknowledgement when `result.data` is empty, a ticker update otherwise; `a` is the
  observation and is null when the instrument has not traded

A non-zero `code` on any message is reported as an error.

## Session State

The handler keeps a ticker cache and the next request id per session; `Copy()` resets both for each new connection.

## Market Checks

```sh
curl https://api.crypto.com/exchange/v1/public/get-instruments
curl 'https://api.crypto.com/exchange/v1/public/get-tickers?instrument_name=USDT_USD'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/cryptodotcom
```

## Wire reference

```json
{"id":1,"method":"subscribe","params":{"channels":["ticker.USDT_USD"]}}
{"id":1,"method":"subscribe","code":0}
{"id":1587523073344,"method":"public/heartbeat","code":0}
{"id":1587523073344,"method":"public/respond-heartbeat"}
{"id":-1,"method":"subscribe","code":0,"result":{"instrument_name":"USDT_USD","subscription":"ticker.USDT_USD","channel":"ticker","data":[{"h":"1.0002","l":"0.9998","a":"1.000100","i":"USDT_USD","t":1613580710768}]}}
```

The complete upstream contract is the [Crypto.com Exchange websocket API](https://exchange-docs.crypto.com/exchange/v1/rest-ws/index.html#websocket-root-endpoints).
