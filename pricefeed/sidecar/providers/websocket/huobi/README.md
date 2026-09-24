# Huobi WebSocket Provider

The Huobi (HTX) WebSocket provider subscribes to Huobi market ticker topics and publishes last prices as provider
responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Every message Huobi sends is gzip-compressed. The handler inflates each one under a 1 MiB cap before parsing it, so
a compressed frame that expands far beyond a ticker message is refused rather than decoded. Requested symbols are
subscribed as `market.<symbol>.ticker` topics, one per message; symbols are lower-case, such as `usdcusdt`. The
handler processes:

- `ping` messages, sent by the server every five seconds, which are answered with a `pong` carrying the same value;
  a ping also resolves an unchanged result for tickers with a valid price observed in the current connection,
  which the provider's `max_unchanged_age` decides whether to honour
- subscription responses, reported as errors when `status` is not `ok`
- ticker streams on `market.<symbol>.ticker`, whose `tick.lastPrice` is the observation, decoded as a JSON number and
  kept as text

## Session State

The handler keeps requested tickers and the set of tickers with valid price observations per session; `Copy()`
resets both for each new connection. A subscription request or acknowledgement alone does not qualify a ticker
for heartbeat refreshes.

## Market Checks

```sh
curl https://api.huobi.pro/v1/common/symbols
curl 'https://api.huobi.pro/market/detail/merged?symbol=usdcusdt'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/huobi
```

## Wire reference

Shown decompressed:

```json
{"sub":"market.usdcusdt.ticker","id":"usdcusdt"}
{"id":"usdcusdt","status":"ok","subbed":"market.usdcusdt.ticker","ts":1630982370526}
{"ping":1492420473027}
{"pong":1492420473027}
{"ch":"market.usdcusdt.ticker","ts":1630982370526,"tick":{"open":1.0,"high":1.0002,"low":0.9997,"close":0.99985,"lastPrice":0.99985,"lastSize":10}}
```

The complete upstream contract is the [Huobi market ticker channel](https://huobiapi.github.io/docs/spot/v1/en/#market-ticker).
