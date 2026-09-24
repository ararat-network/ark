# Bybit WebSocket Provider

The Bybit WebSocket provider subscribes to Bybit spot tickers topics and publishes last prices as provider responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Requested symbols are subscribed as `tickers.<symbol>` topics, up to ten per subscribe request, which is Bybit's
limit on args. Symbols use the `BASEQUOTE` form, such as `USDCUSDT`. The handler processes:

- `subscribe` responses, reported as errors when `success` is false
- `ping` responses to the client heartbeat, which the fetcher sends on its ping interval to stay under Bybit's idle
  limit
- ticker updates, which carry no `op`; `data.lastPrice` is the observation

## Session State

The handler keeps a ticker cache per session; `Copy()` resets it for each new connection.

## Market Checks

```sh
curl 'https://api.bybit.com/v5/market/instruments-info?category=spot'
curl 'https://api.bybit.com/v5/market/tickers?category=spot&symbol=USDCUSDT'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/bybit
```

## Wire reference

```json
{"op":"subscribe","args":["tickers.USDCUSDT"]}
{"success":true,"ret_msg":"subscribe","conn_id":"2324d924","req_id":"","op":"subscribe"}
{"req_id":"1716915868145","op":"ping"}
{"topic":"tickers.USDCUSDT","ts":1673853746003,"type":"snapshot","cs":2588407389,"data":{"symbol":"USDCUSDT","lastPrice":"0.9998"}}
```

The complete upstream contracts are [connect](https://bybit-exchange.github.io/docs/v5/ws/connect) and the
[tickers channel](https://bybit-exchange.github.io/docs/v5/websocket/public/ticker).
