# Binance WebSocket Provider

The Binance WebSocket provider subscribes to Binance spot streams and publishes price updates as provider responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

For each requested ticker, the handler subscribes to Binance stream names derived from the provider symbol. The current
handler processes:

- aggregate trade stream messages
- ticker stream messages
- subscription acknowledgement messages

Both price-bearing stream types are parsed into the same Ark `types.Response` shape. Subscription failures can produce
follow-up subscription messages, which the shared WebSocket fetcher writes back to the same connection.

## Session State

The Binance handler keeps per-session state:

- a ticker cache for symbols currently known to the connection
- subscription request IDs mapped to the instruments in each request
- the next subscription request ID

`Copy()` resets that state for each new connection session. This keeps reconnects and sharded connections independent.

## Market Checks

Useful Binance endpoints when checking market support:

```sh
curl https://api.binance.com/api/v3/exchangeInfo
curl 'https://api.binance.com/api/v3/ticker/price?symbol=BTCUSDT'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/binance
```

## Wire reference

Subscription messages correlate a per-session sequential `id` with an acknowledgement whose `result` is null on
success. Combined streams wrap the observation in `stream` and `data`:

```json
{"method":"SUBSCRIBE","params":["btcusdt@aggTrade"],"id":1}
{"result":null,"id":1}
{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","s":"BTCUSDT","p":"67734.00000000"}}
```

The last example shows only the fields needed to identify a price. The complete upstream contracts are
[subscription messages](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams#live-subscribingunsubscribing-to-streams),
[aggregate trades](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams#aggregate-trade-streams),
and [individual tickers](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams#individual-symbol-ticker-streams).
The Go structs in [messages.go](messages.go) define the fields this adapter decodes.
