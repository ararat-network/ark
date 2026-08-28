# Binance WebSocket Provider

The Binance WebSocket provider subscribes to Binance spot streams and publishes price updates as provider responses.

Current defaults live in `config.go`:

- Provider name: `binance_ws`
- Transport: `websocket`
- Endpoint: `wss://stream.binance.com/stream`
- Default max tickers per connection: `40`
- Default write interval: `300ms`
- Default handshake timeout: `20s`
- API key: not required

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
