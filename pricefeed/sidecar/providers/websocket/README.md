# WebSocket Providers

WebSocket providers maintain streaming subscriptions for provider tickers. Each adapter translates requested tickers into
provider-specific subscription messages and translates incoming provider messages back into `types.Response`.

The shared WebSocket fetcher in `base/websocket` owns dial, reconnect, sharding, subscription writes, read/write
timeouts, heartbeat scheduling, response publication, and WebSocket metrics. Provider packages under this directory
should only own exchange-specific protocol messages and message parsing.

## Adapter Contract

WebSocket adapters implement `websocket.DataHandler`:

- `CreateMessages(tickers)` builds the subscription messages for one connection.
- `HandleMessage(message)` parses a provider message and may return a price response plus optional follow-up messages.
- `HeartBeatMessages()` builds provider-level heartbeat messages when the provider protocol requires them.
- `Copy()` returns independent per-connection handler state.

`Copy()` is important because the shared fetcher can reconnect and shard tickers across connections. Any state tied to a
session, such as subscription IDs or symbol caches, belongs in the copied handler.

## Supported Providers

- [Binance](./binance/README.md) subscribes to spot aggregate trade and ticker streams.

See [shared fetcher lifecycle](../base/README.md) and [provider construction/testing](../README.md).
