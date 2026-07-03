# Provider Testing

`providertest` contains integration-test helpers for running real Noah
providers and sampling their cached prices.

This package does not own provider construction. Production assembly should live
in `oracle/sidecar/providers`, and tests should pass that construction in as a
`Builder`.

```go
results, err := providertest.Run(ctx, func(ctx context.Context) (*base.Provider, error) {
	return providers.NewProvider(...)
}, providertest.DefaultProviderTestConfig())
```

Use this package when a test needs to exercise the provider lifecycle through
`Start`, `GetPrices`, and `Stop`. Unit tests for fetcher internals still belong
under `oracle/sidecar/providers/base/...`.

Noah does not currently need a `providers/volatile` package. Connect uses its
volatile provider as a fake API provider registered in production factory
tables; Noah can use test builders or stub fetchers for that role unless a real
configured fake provider is needed for manual runs.
