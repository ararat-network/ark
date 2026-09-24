# Gate.io WebSocket Provider

The Gate.io WebSocket provider subscribes to the Gate.io spot tickers channel and publishes last prices as provider
responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Requested pairs are subscribed to `spot.tickers` in subscribe requests carrying a sequential id and a Unix `time`.
Pairs use the `BASE_QUOTE` form, such as `USDC_USDT`. The handler processes:

- `subscribe` responses, reported as errors when `error` is set or `result.status` is not `success`
- `spot.pong` answers to the client ping, which the fetcher sends on its ping interval so a quiet market's reads keep
  flowing
- `update` events on `spot.tickers`, whose `result.last` is the observation

## Session State

The handler keeps a ticker cache and the next request id per session; `Copy()` resets both for each new connection.

## Market Checks

```sh
curl https://api.gateio.ws/api/v4/spot/currency_pairs
curl 'https://api.gateio.ws/api/v4/spot/tickers?currency_pair=USDC_USDT'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/gate
```

## Wire reference

```json
{"time":1611541000,"channel":"spot.tickers","event":"subscribe","id":1,"payload":["USDC_USDT"]}
{"time":1611541000,"channel":"spot.tickers","event":"subscribe","error":null,"result":{"status":"success"}}
{"time":1545404023,"channel":"spot.ping"}
{"time":1545404023,"channel":"spot.pong","event":"","result":null}
{"time":1669107766,"channel":"spot.tickers","event":"update","result":{"currency_pair":"USDC_USDT","last":"0.9998","lowest_ask":"0.9999","highest_bid":"0.9997"}}
```

The complete upstream contract is the [Gate.io spot websocket API](https://www.gate.io/docs/developers/apiv4/ws/en/).
