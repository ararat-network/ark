# OKX WebSocket Provider

The OKX WebSocket provider subscribes to the OKX tickers channel and publishes last prices as provider responses.

[config.go](config.go) owns the provider name, endpoints and transport defaults. This adapter requires no API key.
An AWS-hosted endpoint and a demo endpoint are listed for operators near those regions.

## Stream Shape

Requested instruments are subscribed to the `tickers` channel, up to 25 per subscribe request, spaced by the write
interval to stay under OKX's rate of three messages per second. Instruments use the `BASE-QUOTE` form, such as
`USDC-USDT`. The handler processes:

- the plain-text `pong` answer to the plain-text `ping` the fetcher sends on its ping interval; OKX closes a
  connection idle for 30 seconds
- `subscribe` confirmations
- `channel-conn-count` notices, sent after each subscription, which are logged
- `channel-conn-count-error` notices, reported as errors: the channel's connection limit is exceeded, and the
  fetcher reconnects when OKX closes the connection
- `error` notices, reported with the venue's code rather than retried, because OKX echoes the bad request every
  time and a retry would loop
- ticker pushes, which carry no `event`; each instrument's `last` is the observation

## Session State

The handler keeps a ticker cache per session; `Copy()` resets it for each new connection.

## Market Checks

```sh
curl 'https://www.okx.com/api/v5/public/instruments?instType=SPOT'
curl 'https://www.okx.com/api/v5/market/ticker?instId=USDC-USDT'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/okx
```

## Wire reference

```json
{"op":"subscribe","args":[{"channel":"tickers","instId":"USDC-USDT"}]}
{"event":"subscribe","arg":{"channel":"tickers","instId":"USDC-USDT"},"connId":"a4d3ae55"}
{"event":"channel-conn-count","channel":"tickers","connCount":"1","connId":"a4d3ae55"}
ping
pong
{"arg":{"channel":"tickers","instId":"USDC-USDT"},"data":[{"instType":"SPOT","instId":"USDC-USDT","last":"0.9998","lastSz":"0.1","askPx":"0.9999","bidPx":"0.9997","ts":"1597026383085"}]}
```

The complete upstream contracts are the [websocket overview](https://www.okx.com/docs-v5/en/#overview-websocket-overview)
and the [tickers channel](https://www.okx.com/docs-v5/en/#order-book-trading-market-data-ws-tickers-channel).
