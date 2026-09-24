# Bitfinex WebSocket Provider

The Bitfinex WebSocket provider subscribes to Bitfinex ticker channels and publishes last-trade prices as provider
responses.

[config.go](config.go) owns the provider name, endpoint and transport defaults. This adapter requires no API key.
Bitfinex names Tether `UST`, so the USDT market is `USTUSD`.

## Stream Shape

Each requested symbol gets its own subscription message; Bitfinex does not batch subscriptions and allows 30 channels
per connection, which the default ticker-per-connection limit stays under. The handler processes:

- the `info` greeting and other platform notices, which are ignored
- `subscribed` confirmations, which bind a channel id to the requested ticker
- `error` notices, which are reported with the venue's code
- stream frames: `[CHANNEL_ID, "hb"]` heartbeats and `[CHANNEL_ID, [BID, BID_SIZE, ASK, ASK_SIZE, DAILY_CHANGE,
  DAILY_CHANGE_RELATIVE, LAST_PRICE, VOLUME, HIGH, LOW]]` ticker payloads

Frames are decoded as raw JSON elements and the price is read from index 6 as its decimal text. Bitfinex sends more
elements than the ten it documents, so the payload only has to reach the price; a shorter one on a confirmed channel is
reported as an unresolved ticker error, and a frame for a channel id that was never confirmed is refused. The confirmation's `pair` field lacks the `t` prefix and its `symbol` field carries it, so either form of the
configured symbol matches.

## Session State

The handler keeps a ticker cache and the channel-id-to-ticker map per session; `Copy()` resets both for each new
connection.

## Market Checks

```sh
curl 'https://api-pub.bitfinex.com/v2/conf/pub:list:pair:exchange'
curl https://api-pub.bitfinex.com/v2/ticker/tUSTUSD
```

See the [adapter contract](../README.md) and [provider development guide](../../README.md). Run the package tests from the repository root:

```sh
go test ./pricefeed/sidecar/providers/websocket/bitfinex
```

## Wire reference

```json
{"event":"subscribe","channel":"ticker","symbol":"USTUSD"}
{"event":"subscribed","channel":"ticker","chanId":7,"symbol":"tUSTUSD","pair":"USTUSD"}
[7,"hb"]
[7,[0.9998,1000,0.9999,2000,0.0001,0.0001,0.99985,12345.6,1.0002,0.9990]]
```

The complete upstream contract is the [public ticker channel](https://docs.bitfinex.com/reference/ws-public-ticker).
