# MEXC WebSocket Provider

The MEXC WebSocket provider subscribes to MEXC spot mini ticker streams and publishes deal prices as provider
responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.

## Stream Shape

Requested symbols are subscribed as `spot@public.miniTicker.v3.api.pb@<SYMBOL>@UTC+8` streams, one per subscription
request; the symbol is upper-cased whatever the configured case. MEXC allows 30 subscriptions per connection, which
the default ticker-per-connection limit stays under, and the handler refuses a larger shard on its own.

Acknowledgements are JSON objects; data arrives as binary protobuf frames, and the handler tells them apart by the
first byte. It processes:

- subscription echoes, which repeat the stream name in `msg`
- `PONG` answers to the client ping, which the fetcher sends on its ping interval to stay under MEXC's 30 second
  idle limit
- refusals, which arrive with a zero `code` and a `msg` that names no stream, reported with the venue's text
- `PushDataV3ApiWrapper` frames on a mini ticker channel, whose `publicMiniTicker.price` is the observation

[wire.go](wire.go) reads the frames with `protowire`, pinning the five field numbers it needs against MEXC's
published schema and skipping every other field, so a field MEXC adds does not break it. The frame is bounded by
the shared fetcher's read limit before it is decoded.

## Session State

The handler keeps a ticker cache per session; `Copy()` resets it for each new connection.

## Market Checks

```sh
curl https://api.mexc.com/api/v3/exchangeInfo
curl 'https://api.mexc.com/api/v3/ticker/price?symbol=USDCUSDT'
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/mexc
```

## Wire reference

```json
{"method":"SUBSCRIPTION","params":["spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8"]}
{"id":0,"code":0,"msg":"spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8"}
{"id":0,"code":0,"msg":"Not Subscribed successfully! [spot@public.miniTicker.v3.api.pb@NOPEUSDT@UTC+8].  Reason： Blocked! "}
{"method":"PING"}
{"id":0,"code":0,"msg":"PONG"}
```

A data frame is a `PushDataV3ApiWrapper` message: field 1 `channel`, field 3 `symbol`, field 6 `sendTime`, and
field 309 `publicMiniTicker`, a `PublicMiniTickerV3Api` whose field 1 is `symbol` and field 2 is `price` as a
decimal string. The schema is [mexcdevelop/websocket-proto](https://github.com/mexcdevelop/websocket-proto); the
stream contract is the [MEXC websocket market streams](https://mexcdevelop.github.io/apidocs/spot_v3_en/#websocket-market-streams).
