# API Providers

API providers fetch prices over HTTP. Each adapter translates Noah provider tickers into a provider-specific request and
translates the HTTP response back into `types.Response`.

The shared API fetcher in `base/api` owns polling, batching, request timing, endpoint selection, response publication,
and API metrics. Provider packages under this directory should only own exchange-specific URL construction and response
parsing.

## Adapter Contract

API adapters implement `api.DataHandler`:

- `CreateURL(endpoint, tickers)` builds the request URL for the selected endpoint and requested tickers.
- `ParseResponse(tickers, response)` returns resolved prices and unresolved errors for exactly the request tickers.

The factory builds the `http.Client`, applies endpoint authentication as request headers, and passes the adapter into the
shared fetcher. Endpoint authentication belongs in config; adapters should not hard-code secrets or environment lookups.

## Supported Providers

- [Binance](./binance/README.md) fetches spot ticker prices from Binance's public REST API.
