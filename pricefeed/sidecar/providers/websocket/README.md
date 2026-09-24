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
- [Bitfinex](./bitfinex/README.md) subscribes to ticker channels and decodes their array frames.
- [Bitstamp](./bitstamp/README.md) subscribes to live trades channels; the client heartbeat keeps the connection open.
- [Bybit](./bybit/README.md) subscribes to spot tickers topics; the client ping keeps the connection open.
- [Coinbase](./coinbase/README.md) subscribes to the ticker and heartbeat channels; a heartbeat naming the last
  match confirms an unchanged price.
- [Crypto.com](./cryptodotcom/README.md) subscribes to ticker channels and answers the server heartbeat.
- [Gate.io](./gate/README.md) subscribes to the spot tickers channel; the client ping keeps the connection open.
- [Huobi](./huobi/README.md) subscribes to market ticker topics, inflating each gzip message under a cap; the
  server ping is answered and refreshes tickers with valid prices observed in the current connection.
- [Kraken](./kraken/README.md) subscribes to the websocket v2 ticker channel.
- [KuCoin](./kucoin/README.md) fetches a connect token before each dial through the `Dialer` contract; the pong
  refreshes tickers with valid prices observed in the current connection.
- [MEXC](./mexc/README.md) subscribes to protobuf-framed mini ticker streams, decoded by a field-pinned wire reader;
  the client ping keeps the connection open.
- [OKX](./okx/README.md) subscribes to the tickers channel; the plain-text ping keeps the connection open.

Heartbeat-driven unchanged results (Coinbase, Huobi, KuCoin) only extend a price when the provider's
`max_unchanged_age` allows it: the [shared provider](../base/README.md#unchanged-results) records the result and the
[sidecar runtime](../../runtime/README.md#freshness) applies the gate. Coinbase's heartbeat names the last trade of
each product; Huobi's ping and KuCoin's pong vouch for the connection, so those two warrant little or no extension.

## Dial Contract

A handler whose venue needs more than a plain dial, such as a per-connection token fetched over HTTPS, implements
`websocket.Dialer`:

- `DialFunc(client)` returns the `websocket.DialFunc` the fetcher dials with; `client` is the redirect-refusing HTTP
  client the fetcher itself uses.

The registry installs it on the fetcher when the handler implements it. The configured endpoint stays the one
dialled; the dial may only add what the venue's protocol requires, such as a query parameter.

See [shared fetcher lifecycle](../base/README.md) and [provider construction/testing](../README.md).
