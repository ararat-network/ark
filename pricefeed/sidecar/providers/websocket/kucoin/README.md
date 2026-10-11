# KuCoin WebSocket Provider

The KuCoin WebSocket provider subscribes to the KuCoin spot ticker topic and publishes last-trade prices as provider
responses.

[config.go](config.go) owns the provider name `kucoin_ws`, endpoint and transport defaults. This adapter requires no API key.

## Connect Token

KuCoin refuses a dial that carries no connect token, so the handler implements the shared fetcher's `Dialer`
contract: [dial.go](dial.go) requests a public token with an empty POST to the fixed bullet-public endpoint, then
dials the configured endpoint with the token in its `token` query parameter, where KuCoin's protocol reads it. The
token is a short-lived public value, not a credential. The token response also names an instance server and ping
timings; the adapter keeps the configured endpoint and the static timings in config (a 10 s ping under the 18 s KuCoin
advertises, a 20 s read deadline that outlasts a ping round trip, 300 ms between writes under its 100 messages per 10 s,
and 25 tickers a connection under its 300 subscriptions), so the endpoint stays an operator decision and the read
deadline stays a config value. A dial error is returned with the token redacted, raw and query-escaped,
because the URL it names would otherwise reach the logs. The token request and the dial share the handshake timeout.
Upstream's handler followed the token response's instance server; this one keeps the configured endpoint.

## Stream Shape

Requested symbols are subscribed under the `/market/ticker:` topic, comma-joined per request up to the batch limit,
with a sequential request id. Symbols use the `BASE-QUOTE` form, such as `USDC-USDT`. The handler processes:

- the `welcome` message sent on connect
- `pong` answers to the client ping, which the fetcher sends on its ping interval; a pong also resolves an unchanged
  result for tickers whose latest report in the current connection was a valid price, which the provider's
  `max_unchanged_age` decides whether to honour
- `ack` acknowledgements of subscriptions
- `error` notices, logged as a warning with the venue's code
- `message` data on the `trade.ticker` subject, whose `data.price` is the observation; a `sequence` at or below the
  last seen for the symbol is refused so a late or repeated message cannot roll a price back, until three arrive in a
  row, which reads as KuCoin restarting the sequence and takes the new one

## Session State

The handler keeps requested tickers, the set of tickers whose latest report was a valid price, per-symbol sequence
numbers and refusal runs, and the next request id per session; `Copy()` resets them for each new connection. A subscription request or
acknowledgement alone does not qualify a ticker for heartbeat refreshes.

## Market Checks

```sh
curl https://api.kucoin.com/api/v2/symbols
curl 'https://api.kucoin.com/api/v1/market/orderbook/level1?symbol=USDC-USDT'
curl -X POST https://api.kucoin.com/api/v1/bullet-public
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/kucoin
```

## Wire reference

```json
{"code":"200000","data":{"token":"...","instanceServers":[{"endpoint":"wss://ws-api-spot.kucoin.com/","protocol":"websocket","pingInterval":18000,"pingTimeout":10000}]}}
{"id":"hQvf8jkno","type":"welcome"}
{"id":1,"type":"subscribe","topic":"/market/ticker:USDC-USDT","privateChannel":false,"response":true}
{"id":"1","type":"ack"}
{"id":"1545910590801","type":"ping"}
{"id":"1545910590801","type":"pong"}
{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"1545896668986","price":"0.9998","size":"0.011","bestAsk":"0.9999","bestBid":"0.9997","time":1704873323416}}
```

The complete upstream contracts are the [public token](https://www.kucoin.com/docs/websocket/basic-info/apply-connect-token/public-token-no-authentication-required-)
and the [ticker channel](https://www.kucoin.com/docs/websocket/spot-trading/public-channels/ticker).
