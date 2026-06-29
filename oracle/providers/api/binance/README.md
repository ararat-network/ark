# Binance API Provider

The Binance API provider fetches spot prices from Binance's public ticker price endpoint.

Current defaults live in `config.go`:

- Provider name: `binance_api`
- Transport: `api`
- Endpoint template: `https://api.binance.com/api/v3/ticker/price?symbols=%s%s%s`
- Default polling interval: `750ms`
- Default request timeout: `3s`
- API key: not required

## Symbol Format

Binance spot symbols use the `BASEQUOTE` form, such as `BTCUSDT` or `ETHUSDT`. Noah maps chain denoms to these provider
symbols through `base.Config.Markets`; the adapter does not perform denom conversion itself.

## Request Shape

`Handler.CreateURL` batches requested symbols into Binance's `symbols` query parameter. A request for `BTCUSDT` and
`ETHUSDT` becomes a single ticker-price request containing both symbols.

`Handler.ParseResponse` only accepts prices for the requested tickers. Missing, malformed, or unexpected values are
returned as unresolved ticker errors so the shared provider runtime can record the outcome without confusing missing data
with transport failure.

## Market Checks

Useful Binance endpoints when checking market support:

```sh
curl https://api.binance.com/api/v3/exchangeInfo
curl 'https://api.binance.com/api/v3/ticker/price?symbol=BTCUSDT'
```
