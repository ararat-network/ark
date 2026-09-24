package providers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	binanceapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/binance"
	bitstampapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/bitstamp"
	coinbaseapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/coinbase"
	coingeckoapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/coingecko"
	coinmarketcapapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/coinmarketcap"
	currencybeaconapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/currencybeacon"
	frankfurterapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	geckoterminalapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/geckoterminal"
	krakenapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/kraken"
	openexchangeratesapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/openexchangerates"
	polymarketapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/polymarket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	binancews "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/binance"
	bitfinexws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/bitfinex"
	bitstampws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/bitstamp"
	bybitws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/bybit"
	coinbasews "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/coinbase"
	cryptodotcomws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/cryptodotcom"
	gatews "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/gate"
	huobiws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/huobi"
	krakenws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/kraken"
	kucoinws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/kucoin"
	mexcws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/mexc"
	okxws "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/okx"
)

func stubAPIHandlerFactory(providers.Config, log.Logger) (api.DataHandler, error) {
	return frankfurterapi.NewHandler(), nil
}

// nopWebSocketHandler satisfies websocket.DataHandler for construction-only
// tests; the binance handler cannot stand in because it rejects foreign names.
type nopWebSocketHandler struct{}

func (nopWebSocketHandler) HandleMessage([]byte) (types.Response, [][]byte, error) {
	return types.Response{}, nil, nil
}

func (nopWebSocketHandler) CreateMessages([]types.Ticker) ([][]byte, error) {
	return nil, nil
}

func (nopWebSocketHandler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
}

func (h nopWebSocketHandler) Copy() websocket.DataHandler {
	return h
}

func stubWebSocketHandlerFactory(providers.Config, log.Logger) (websocket.DataHandler, error) {
	return nopWebSocketHandler{}, nil
}

func customAPIProviderConfig(name string) providers.Config {
	apiCfg := frankfurterapi.DefaultAPIConfig
	apiCfg.Name = name

	return providers.Config{
		Name:          name,
		TransportType: base.API,
		Markets:       types.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		MaxPriceAge:   time.Minute,
		API:           apiCfg,
	}
}

func customWebSocketProviderConfig(name string) providers.Config {
	wsCfg := binancews.DefaultWebSocketConfig
	wsCfg.Name = name

	return providers.Config{
		Name:          name,
		TransportType: base.WebSocket,
		Markets:       types.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		MaxPriceAge:   time.Minute,
		WebSocket:     wsCfg,
	}
}

func TestRegistryRegister(t *testing.T) {
	testCases := []struct {
		name     string
		register func(r *providers.Registry) error
		wantErr  string
	}{
		{
			name: "api provider registers",
			register: func(r *providers.Registry) error {
				return r.RegisterAPI("custom_api", stubAPIHandlerFactory)
			},
		},
		{
			name: "websocket provider registers",
			register: func(r *providers.Registry) error {
				return r.RegisterWebSocket("custom_ws", stubWebSocketHandlerFactory)
			},
		},
		{
			name: "same name registers on both transports",
			register: func(r *providers.Registry) error {
				if err := r.RegisterAPI("custom", stubAPIHandlerFactory); err != nil {
					return err
				}
				return r.RegisterWebSocket("custom", stubWebSocketHandlerFactory)
			},
		},
		{
			name: "empty api name",
			register: func(r *providers.Registry) error {
				return r.RegisterAPI("", stubAPIHandlerFactory)
			},
			wantErr: "provider name cannot be empty",
		},
		{
			name: "empty websocket name",
			register: func(r *providers.Registry) error {
				return r.RegisterWebSocket("", stubWebSocketHandlerFactory)
			},
			wantErr: "provider name cannot be empty",
		},
		{
			name: "nil api factory",
			register: func(r *providers.Registry) error {
				return r.RegisterAPI("custom_api", nil)
			},
			wantErr: "provider handler factory cannot be nil",
		},
		{
			name: "nil websocket factory",
			register: func(r *providers.Registry) error {
				return r.RegisterWebSocket("custom_ws", nil)
			},
			wantErr: "provider handler factory cannot be nil",
		},
		{
			name: "duplicate api name",
			register: func(r *providers.Registry) error {
				if err := r.RegisterAPI("custom_api", stubAPIHandlerFactory); err != nil {
					return err
				}
				return r.RegisterAPI("custom_api", stubAPIHandlerFactory)
			},
			wantErr: "provider already registered: custom_api",
		},
		{
			name: "duplicate websocket name",
			register: func(r *providers.Registry) error {
				if err := r.RegisterWebSocket("custom_ws", stubWebSocketHandlerFactory); err != nil {
					return err
				}
				return r.RegisterWebSocket("custom_ws", stubWebSocketHandlerFactory)
			},
			wantErr: "provider already registered: custom_ws",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.register(providers.NewRegistry())
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRegistryNewProvider(t *testing.T) {
	testCases := []struct {
		name     string
		cfg      providers.Config
		register func(r *providers.Registry) error
		wantErr  string
	}{
		{
			name: "registered api handler builds",
			cfg:  customAPIProviderConfig("custom_api"),
			register: func(r *providers.Registry) error {
				return r.RegisterAPI("custom_api", stubAPIHandlerFactory)
			},
		},
		{
			name: "registered websocket handler builds",
			cfg:  customWebSocketProviderConfig("custom_ws"),
			register: func(r *providers.Registry) error {
				return r.RegisterWebSocket("custom_ws", stubWebSocketHandlerFactory)
			},
		},
		{
			name:    "unregistered api name",
			cfg:     customAPIProviderConfig("custom_api"),
			wantErr: "unrecognised provider name: custom_api",
		},
		{
			name:    "unregistered websocket name",
			cfg:     customWebSocketProviderConfig("custom_ws"),
			wantErr: "unrecognised provider name: custom_ws",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			registry := providers.NewRegistry()
			if tc.register != nil {
				require.NoError(t, tc.register(registry))
			}

			provider, err := registry.NewProvider(tc.cfg, tc.cfg.Markets, log.NewNopLogger())
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.cfg.Name, provider.Name())
		})
	}
}

func TestRegistryNewProviderPassesConfigToFactory(t *testing.T) {
	cfg := customAPIProviderConfig("custom_api")

	registry := providers.NewRegistry()
	var receivedName string
	err := registry.RegisterAPI("custom_api", func(cfg providers.Config, logger log.Logger) (api.DataHandler, error) {
		receivedName = cfg.Name
		return frankfurterapi.NewHandler(), nil
	})
	require.NoError(t, err)

	_, err = registry.NewProvider(cfg, nil, log.NewNopLogger())

	require.NoError(t, err)
	require.Equal(t, "custom_api", receivedName)
}

func TestDefaultRegistryBuildsInTreeProviders(t *testing.T) {
	markets := types.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}}
	apiConfigs := map[string]api.Config{
		binanceapi.Name:           binanceapi.DefaultNonUSAPIConfig,
		bitstampapi.Name:          bitstampapi.DefaultAPIConfig,
		coinbaseapi.Name:          coinbaseapi.DefaultAPIConfig,
		coingeckoapi.Name:         coingeckoapi.DefaultAPIConfig,
		coinmarketcapapi.Name:     coinmarketcapapi.DefaultAPIConfig,
		currencybeaconapi.Name:    currencybeaconapi.DefaultAPIConfig,
		frankfurterapi.Name:       frankfurterapi.DefaultAPIConfig,
		geckoterminalapi.Name:     geckoterminalapi.DefaultETHAPIConfig,
		krakenapi.Name:            krakenapi.DefaultAPIConfig,
		openexchangeratesapi.Name: openexchangeratesapi.DefaultAPIConfig,
		polymarketapi.Name:        polymarketapi.DefaultAPIConfig,
	}
	wsConfigs := map[string]websocket.Config{
		binancews.Name:      binancews.DefaultWebSocketConfig,
		bitfinexws.Name:     bitfinexws.DefaultWebSocketConfig,
		bitstampws.Name:     bitstampws.DefaultWebSocketConfig,
		bybitws.Name:        bybitws.DefaultWebSocketConfig,
		coinbasews.Name:     coinbasews.DefaultWebSocketConfig,
		cryptodotcomws.Name: cryptodotcomws.DefaultWebSocketConfig,
		gatews.Name:         gatews.DefaultWebSocketConfig,
		huobiws.Name:        huobiws.DefaultWebSocketConfig,
		krakenws.Name:       krakenws.DefaultWebSocketConfig,
		kucoinws.Name:       kucoinws.DefaultWebSocketConfig,
		mexcws.Name:         mexcws.DefaultWebSocketConfig,
		okxws.Name:          okxws.DefaultWebSocketConfig,
	}

	for name, apiCfg := range apiConfigs {
		t.Run(name, func(t *testing.T) {
			cfg := providers.Config{
				Name:          name,
				TransportType: base.API,
				Markets:       markets,
				MaxPriceAge:   time.Minute,
				API:           apiCfg,
			}

			provider, err := providers.DefaultRegistry().NewProvider(cfg, cfg.Markets, log.NewNopLogger())

			require.NoError(t, err)
			require.Equal(t, name, provider.Name())
		})
	}

	for name, wsCfg := range wsConfigs {
		t.Run(name, func(t *testing.T) {
			cfg := providers.Config{
				Name:          name,
				TransportType: base.WebSocket,
				Markets:       markets,
				MaxPriceAge:   time.Minute,
				WebSocket:     wsCfg,
			}

			provider, err := providers.DefaultRegistry().NewProvider(cfg, cfg.Markets, log.NewNopLogger())

			require.NoError(t, err)
			require.Equal(t, name, provider.Name())
		})
	}
}

// dialerWebSocketHandler records that the registry asked it for a dial.
type dialerWebSocketHandler struct {
	nopWebSocketHandler
	dialRequested *bool
}

func (h dialerWebSocketHandler) DialFunc(client *http.Client) websocket.DialFunc {
	*h.dialRequested = client != nil
	return nil
}

// TestNewProviderInstallsTheHandlerDial pins the Dialer contract: a handler
// that supplies a dial is asked for it with the fetcher's HTTP client, so a
// venue that needs a connect token gets one on every session.
func TestNewProviderInstallsTheHandlerDial(t *testing.T) {
	dialRequested := false
	registry := providers.NewRegistry()
	require.NoError(t, registry.RegisterWebSocket("custom_ws", func(providers.Config, log.Logger) (websocket.DataHandler, error) {
		return dialerWebSocketHandler{dialRequested: &dialRequested}, nil
	}))

	// A nil dial from the handler is refused by the fetcher, which is what
	// proves the registry installed it rather than the default.
	_, err := registry.NewProvider(customWebSocketProviderConfig("custom_ws"), nil, log.NewNopLogger())

	require.ErrorContains(t, err, "dial function is nil")
	require.True(t, dialRequested)
}

func TestNewProviderAllowsEmptyActiveMarkets(t *testing.T) {
	cfg := providers.Config{
		Name:          frankfurterapi.Name,
		TransportType: base.API,
		Markets:       types.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		MaxPriceAge:   time.Minute,
		API: api.Config{
			Name:      frankfurterapi.Name,
			Timeout:   time.Second,
			Interval:  time.Second,
			Endpoints: []types.Endpoint{{URL: "https://example.invalid/prices"}},
		},
	}

	provider, err := providers.DefaultRegistry().NewProvider(cfg, nil, log.NewNopLogger())

	require.NoError(t, err)
	require.Empty(t, provider.GetTickers())
}
