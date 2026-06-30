package providers

import (
	"fmt"
	"net/http"

	"cosmossdk.io/log/v2"

	binanceapi "noah/oracle/providers/api/binance"
	"noah/oracle/providers/base"
	"noah/oracle/providers/base/api"
	"noah/oracle/providers/base/websocket"
	binancews "noah/oracle/providers/websocket/binance"
)

// NewProvider builds the provider runtime and the transport-specific fetcher
// selected by cfg.Type.
func NewProvider(cfg Config, logger log.Logger, denoms []string) (*base.Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var (
		fetcher base.Fetcher
		err     error
	)
	switch cfg.Type {
	case base.API:
		fetcher, err = buildAPIFetcher(cfg, logger)

	case base.WebSocket:
		fetcher, err = buildWebSocketFetcher(cfg, logger)
	default:
		return nil, fmt.Errorf("invalid provider type: %s", cfg.Type)
	}
	if err != nil {
		return nil, err
	}
	provider, err := base.NewProvider(
		cfg.Name,
		cfg.Type,
		cfg.Markets,
		fetcher,
		base.WithLogger(logger),
		base.WithDenoms(denoms),
	)
	if err != nil {
		return nil, err
	}

	return provider, nil
}

// buildAPIFetcher selects the provider-specific API handler and wraps it in the
// shared API fetcher runtime.
func buildAPIFetcher(cfg Config, logger log.Logger) (*api.Fetcher, error) {
	var dataHandler api.DataHandler
	switch cfg.Name {
	case binanceapi.Name:
		dataHandler = binanceapi.NewHandler()
	default:
		return nil, fmt.Errorf("unrecognised provider name: %s", cfg.Name)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
		Timeout: cfg.API.Timeout,
	}

	headers := make(map[string]string)
	// Endpoint authentication is stored in config; the API fetcher consumes it
	// as request headers.
	if auth := cfg.API.Endpoints[0].Authentication; auth.Enabled() {
		headers[auth.APIKeyHeader] = auth.APIKey
	}

	fetcher, err := api.NewFetcher(
		cfg.API,
		client,
		dataHandler,
		api.WithHTTPHeaders(headers),
		api.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return fetcher, nil
}

// buildWebSocketFetcher selects the provider-specific websocket data handler
// and wraps it in the shared websocket fetcher runtime.
func buildWebSocketFetcher(cfg Config, logger log.Logger) (*websocket.Fetcher, error) {
	var dataHandler websocket.DataHandler
	var err error
	switch cfg.Name {
	case binancews.Name:
		dataHandler, err = binancews.NewHandler(logger, cfg.WebSocket)
	default:
		return nil, fmt.Errorf("unrecognised provider name: %s", cfg.Name)
	}
	if err != nil {
		return nil, err
	}

	fetcher, err := websocket.NewFetcher(
		cfg.WebSocket,
		dataHandler,
		websocket.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return fetcher, nil
}
