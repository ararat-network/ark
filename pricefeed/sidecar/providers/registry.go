package providers

import (
	"errors"
	"fmt"
	"net/http"

	"cosmossdk.io/log/v2"

	binanceapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/binance"
	currencybeaconapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/currencybeacon"
	frankfurterapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	openexchangeratesapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/openexchangerates"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	binancews "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/binance"
)

// APIHandlerFactory builds a provider's API data handler.
type APIHandlerFactory func(cfg Config, logger log.Logger) (api.DataHandler, error)

// WebSocketHandlerFactory builds a provider's websocket data handler.
type WebSocketHandlerFactory func(cfg Config, logger log.Logger) (websocket.DataHandler, error)

// Registry maps provider names to handler factories, one namespace per
// transport. Finish registration before handing the registry to a runtime:
// lookups are unsynchronised and recur on every config reload.
type Registry struct {
	apiFactories map[string]APIHandlerFactory
	wsFactories  map[string]WebSocketHandlerFactory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		apiFactories: make(map[string]APIHandlerFactory),
		wsFactories:  make(map[string]WebSocketHandlerFactory),
	}
}

// DefaultRegistry returns a registry of the in-tree providers.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	// In-tree names are distinct non-empty constants; registration cannot fail.
	_ = r.RegisterAPI(binanceapi.Name, func(Config, log.Logger) (api.DataHandler, error) {
		return binanceapi.NewHandler(), nil
	})
	_ = r.RegisterAPI(currencybeaconapi.Name, func(Config, log.Logger) (api.DataHandler, error) {
		return currencybeaconapi.NewHandler(), nil
	})
	_ = r.RegisterAPI(frankfurterapi.Name, func(Config, log.Logger) (api.DataHandler, error) {
		return frankfurterapi.NewHandler(), nil
	})
	_ = r.RegisterAPI(openexchangeratesapi.Name, func(Config, log.Logger) (api.DataHandler, error) {
		return openexchangeratesapi.NewHandler(), nil
	})
	_ = r.RegisterWebSocket(binancews.Name, func(cfg Config, logger log.Logger) (websocket.DataHandler, error) {
		return binancews.NewHandler(logger, cfg.WebSocket)
	})

	return r
}

// RegisterAPI registers the handler factory for an API provider name.
func (r *Registry) RegisterAPI(name string, factory APIHandlerFactory) error {
	if name == "" {
		return errors.New("provider name cannot be empty")
	}
	if factory == nil {
		return errors.New("provider handler factory cannot be nil")
	}
	if _, ok := r.apiFactories[name]; ok {
		return fmt.Errorf("provider already registered: %s", name)
	}
	r.apiFactories[name] = factory

	return nil
}

// RegisterWebSocket registers the handler factory for a websocket provider name.
func (r *Registry) RegisterWebSocket(name string, factory WebSocketHandlerFactory) error {
	if name == "" {
		return errors.New("provider name cannot be empty")
	}
	if factory == nil {
		return errors.New("provider handler factory cannot be nil")
	}
	if _, ok := r.wsFactories[name]; ok {
		return fmt.Errorf("provider already registered: %s", name)
	}
	r.wsFactories[name] = factory

	return nil
}

// NewProvider builds the provider runtime and the transport-specific fetcher
// selected by cfg.TransportType, using the handler factory registered for
// cfg.Name.
func (r *Registry) NewProvider(cfg Config, markets types.Markets, logger log.Logger) (*base.Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var (
		fetcher base.Fetcher
		err     error
	)
	switch cfg.TransportType {
	case base.API:
		fetcher, err = r.buildAPIFetcher(cfg, logger)

	case base.WebSocket:
		fetcher, err = r.buildWebSocketFetcher(cfg, logger)
	default:
		return nil, fmt.Errorf("invalid provider transport type: %s", cfg.TransportType)
	}
	if err != nil {
		return nil, err
	}
	provider, err := base.NewProvider(
		cfg.Name,
		cfg.TransportType,
		markets,
		fetcher,
		base.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return provider, nil
}

// buildAPIFetcher builds the registered API handler and wraps it in the shared
// API fetcher runtime.
func (r *Registry) buildAPIFetcher(cfg Config, logger log.Logger) (*api.Fetcher, error) {
	factory, ok := r.apiFactories[cfg.Name]
	if !ok {
		return nil, fmt.Errorf("unrecognised provider name: %s", cfg.Name)
	}
	dataHandler, err := factory(cfg, logger)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}

	fetcher, err := api.NewFetcher(
		cfg.API,
		client,
		dataHandler,
		api.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return fetcher, nil
}

// buildWebSocketFetcher builds the registered websocket handler and wraps it in
// the shared websocket fetcher runtime.
func (r *Registry) buildWebSocketFetcher(cfg Config, logger log.Logger) (*websocket.Fetcher, error) {
	factory, ok := r.wsFactories[cfg.Name]
	if !ok {
		return nil, fmt.Errorf("unrecognised provider name: %s", cfg.Name)
	}
	dataHandler, err := factory(cfg, logger)
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
