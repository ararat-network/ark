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
func NewProvider(cfg base.Config, logger log.Logger, opts ...Option) (*base.Provider, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Fetcher config overrides are transport-specific. Rejecting the inactive
	// transport keeps provider construction unambiguous.
	if o.hasAPIConfig {
		if cfg.Type == base.WebSocket {
			return nil, fmt.Errorf("API config override supplied for websocket provider %s", cfg.Name)
		}
		if cfg.Name != o.apiConfig.Name {
			return nil, fmt.Errorf("mismatched provider and fetcher name")
		}
	}
	if o.hasWSConfig {
		if cfg.Type == base.API {
			return nil, fmt.Errorf("websocket config override supplied for API provider %s", cfg.Name)
		}
		if cfg.Name != o.wsConfig.Name {
			return nil, fmt.Errorf("mismatched provider and fetcher name")
		}
	}

	var (
		fetcher base.Fetcher
		err     error
	)
	switch cfg.Type {
	case base.API:
		fetcher, err = buildAPIFetcher(cfg, logger, &o)
	case base.WebSocket:
		fetcher, err = buildWebSocketFetcher(cfg, logger, &o)
	default:
		return nil, fmt.Errorf("invalid provider type: %s", cfg.Type)
	}
	if err != nil {
		return nil, err
	}

	provider, err := base.NewProvider(
		cfg,
		fetcher,
		base.WithLogger(logger),
		base.WithDenoms(o.denoms),
	)
	if err != nil {
		return nil, err
	}

	return provider, nil
}

// buildAPIFetcher selects the provider-specific API handler and wraps it in the
// shared API fetcher runtime.
func buildAPIFetcher(cfg base.Config, logger log.Logger, opt *options) (*api.Fetcher, error) {
	var fetcherCfg api.Config
	var dataHandler api.DataHandler
	switch cfg.Name {
	case binanceapi.Name:
		fetcherCfg = binanceapi.DefaultNonUSAPIConfig
		dataHandler = binanceapi.NewHandler()
	default:
		return nil, fmt.Errorf("unrecognised provider name: %s", cfg.Name)
	}
	if opt.hasAPIConfig {
		fetcherCfg = opt.apiConfig
	}
	if err := fetcherCfg.Validate(); err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
		Timeout: fetcherCfg.Timeout,
	}

	headers := make(map[string]string)
	// Endpoint authentication is stored in config; the API fetcher consumes it
	// as request headers.
	if auth := fetcherCfg.Endpoints[0].Authentication; auth.Enabled() {
		headers[auth.APIKeyHeader] = auth.APIKey
	}

	fetcher, err := api.NewFetcher(
		fetcherCfg,
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
func buildWebSocketFetcher(cfg base.Config, logger log.Logger, opt *options) (*websocket.Fetcher, error) {
	fetcherCfg := opt.wsConfig

	var dataHandler websocket.DataHandler
	var err error
	switch cfg.Name {
	case binancews.Name:
		if !opt.hasWSConfig {
			fetcherCfg = binancews.DefaultWebSocketConfig
		}
		dataHandler, err = binancews.NewHandler(logger, fetcherCfg)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unrecognised provider name: %s", cfg.Name)
	}
	if err := fetcherCfg.Validate(); err != nil {
		return nil, err
	}

	fetcher, err := websocket.NewFetcher(
		fetcherCfg,
		dataHandler,
		websocket.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return fetcher, nil
}

type options struct {
	apiConfig    api.Config
	hasAPIConfig bool

	wsConfig    websocket.Config
	hasWSConfig bool

	denoms []string
}

// Option configures provider construction without changing the base provider
// runtime contract.
type Option func(*options)

// WithDenoms sets the chain denoms that should be resolved through cfg.Markets.
func WithDenoms(denoms []string) Option {
	return func(o *options) {
		o.denoms = append([]string(nil), denoms...)
	}
}

// WithAPIConfig overrides the default API fetcher config for the selected
// provider.
func WithAPIConfig(cfg api.Config) Option {
	return func(o *options) {
		o.apiConfig = cfg
		o.hasAPIConfig = true
	}
}

// WithWebSocketConfig overrides the default websocket fetcher config for the
// selected provider.
func WithWebSocketConfig(cfg websocket.Config) Option {
	return func(o *options) {
		o.wsConfig = cfg
		o.hasWSConfig = true
	}
}
