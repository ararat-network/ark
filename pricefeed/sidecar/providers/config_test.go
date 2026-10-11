package providers_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestConfigEqualComparesIdentityTypeAndTransportConfig(t *testing.T) {
	markets := types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}

	testCases := []struct {
		name string
		a    Config
		b    Config
		want bool
	}{
		{
			name: "matching api config",
			a:    testAPIProviderConfig("unknown", markets),
			b:    testAPIProviderConfig("unknown", markets),
			want: true,
		},
		{
			name: "different provider name",
			a:    testAPIProviderConfig("unknown", markets),
			b:    testAPIProviderConfig("other", markets),
			want: false,
		},
		{
			name: "different transport type",
			a:    testAPIProviderConfig("unknown", markets),
			b:    testWebSocketProviderConfig("test", markets),
			want: false,
		},
		{
			name: "different markets only",
			a:    testAPIProviderConfig("unknown", markets),
			b: func() Config {
				cfg := testAPIProviderConfig("unknown", types.Markets{{Pair: "USDT/KRW", Symbol: "USDTKRW"}})
				return cfg
			}(),
			want: true,
		},
		{
			name: "different max price age only",
			a:    testAPIProviderConfig("unknown", markets),
			b: func() Config {
				cfg := testAPIProviderConfig("unknown", markets)
				cfg.MaxPriceAge += time.Second
				return cfg
			}(),
			want: true,
		},
		{
			name: "different max unchanged age only",
			a:    testAPIProviderConfig("unknown", markets),
			b: func() Config {
				cfg := testAPIProviderConfig("unknown", markets)
				cfg.MaxUnchangedAge = cfg.MaxPriceAge
				return cfg
			}(),
			want: true,
		},
		{
			name: "matching websocket config",
			a:    testWebSocketProviderConfig("unknown", markets),
			b:    testWebSocketProviderConfig("unknown", markets),
			want: true,
		},
		{
			name: "different websocket config",
			a:    testWebSocketProviderConfig("unknown", markets),
			b: func() Config {
				cfg := testWebSocketProviderConfig("unknown", markets)
				cfg.WebSocket.ReadTimeout += time.Second
				return cfg
			}(),
			want: false,
		},
		{
			name: "invalid transport type",
			a: Config{
				Name:          "unknown",
				TransportType: base.TransportType("unknown"),
			},
			b: Config{
				Name:          "unknown",
				TransportType: base.TransportType("unknown"),
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.a.Equal(tc.b))
		})
	}
}

func TestConfigValidate(t *testing.T) {
	markets := types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}

	testCases := []struct {
		name    string
		cfg     func() Config
		wantErr string
	}{
		{
			name: "valid api",
			cfg:  func() Config { return testAPIProviderConfig("test", markets) },
		},
		{
			name: "valid websocket",
			cfg:  func() Config { return testWebSocketProviderConfig("test", markets) },
		},
		{
			name: "empty name",
			cfg: func() Config {
				cfg := testAPIProviderConfig("test", markets)
				cfg.Name = ""
				return cfg
			},
			wantErr: "provider name cannot be empty",
		},
		{
			name: "empty transport type",
			cfg: func() Config {
				cfg := testAPIProviderConfig("test", markets)
				cfg.TransportType = ""
				return cfg
			},
			wantErr: "provider transport type cannot be empty",
		},
		{
			name:    "no markets",
			cfg:     func() Config { return testAPIProviderConfig("test", nil) },
			wantErr: "markets is empty",
		},
		{
			name: "duplicate market pair",
			cfg: func() Config {
				return testAPIProviderConfig("test", types.Markets{
					{Pair: "USDT/USD", Symbol: "USDTUSD"},
					{Pair: "USDT/USD", Symbol: "USDT-USD"},
				})
			},
			wantErr: `duplicate pair "USDT/USD"`,
		},
		{
			name: "api name mismatch",
			cfg: func() Config {
				cfg := testAPIProviderConfig("test", markets)
				cfg.API.Name = "other"
				return cfg
			},
			wantErr: "mismatched provider and API config name",
		},
		{
			name: "invalid api config",
			cfg: func() Config {
				cfg := testAPIProviderConfig("test", markets)
				cfg.API.Timeout = 0
				return cfg
			},
			wantErr: "timeout must be greater than 0",
		},
		{
			name: "websocket name mismatch",
			cfg: func() Config {
				cfg := testWebSocketProviderConfig("test", markets)
				cfg.WebSocket.Name = "other"
				return cfg
			},
			wantErr: "mismatched provider and websocket config name",
		},
		{
			name: "invalid websocket config",
			cfg: func() Config {
				cfg := testWebSocketProviderConfig("test", markets)
				cfg.WebSocket.ReadTimeout = 0
				return cfg
			},
			wantErr: "websocket read timeout must be greater than 0",
		},
		{
			name: "websocket transport validates the websocket config",
			cfg: func() Config {
				cfg := testAPIProviderConfig("test", markets)
				cfg.TransportType = base.WebSocket
				return cfg
			},
			wantErr: "mismatched provider and websocket config name",
		},
		{
			name: "unknown transport type",
			cfg: func() Config {
				cfg := testAPIProviderConfig("test", markets)
				cfg.TransportType = base.TransportType("grpc")
				return cfg
			},
			wantErr: "invalid provider transport type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()

			err := cfg.Validate()

			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestConfigValidateRejectsNonPositiveMaxPriceAge(t *testing.T) {
	testCases := []struct {
		name        string
		maxPriceAge time.Duration
	}{
		{name: "zero", maxPriceAge: 0},
		{name: "negative", maxPriceAge: -time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testAPIProviderConfig("unknown", types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}})
			cfg.MaxPriceAge = tc.maxPriceAge

			err := cfg.Validate()

			require.ErrorContains(t, err, "provider max price age must be greater than 0")
		})
	}
}

// Zero disables extension; anything set must reach MaxPriceAge, or the bound
// would shrink MaxPriceAge for every price rather than extend it for unchanged
// ones.
func TestConfigValidateBoundsMaxUnchangedAge(t *testing.T) {
	testCases := []struct {
		name            string
		maxUnchangedAge time.Duration
		wantErr         string
	}{
		{name: "zero disables", maxUnchangedAge: 0},
		{name: "equal to max price age", maxUnchangedAge: time.Minute},
		{name: "above max price age", maxUnchangedAge: 2 * time.Minute},
		{
			name:            "below max price age",
			maxUnchangedAge: time.Minute - time.Second,
			wantErr:         "provider max unchanged age must be zero or at least max price age",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testAPIProviderConfig("unknown", types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}})
			cfg.MaxUnchangedAge = tc.maxUnchangedAge

			err := cfg.Validate()

			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func testAPIProviderConfig(name string, markets types.Markets) Config {
	return Config{
		Name:          name,
		TransportType: base.API,
		Markets:       markets,
		MaxPriceAge:   time.Minute,
		API: api.Config{
			Name:      name,
			Timeout:   time.Second,
			Interval:  time.Second,
			Endpoints: []types.Endpoint{{URL: "https://example.invalid/prices"}},
		},
	}
}

func testWebSocketProviderConfig(name string, markets types.Markets) Config {
	return Config{
		Name:          name,
		TransportType: base.WebSocket,
		Markets:       markets,
		MaxPriceAge:   time.Minute,
		WebSocket: websocket.Config{
			Name:                     name,
			MaxBufferSize:            1,
			ReconnectionTimeout:      time.Second,
			PostConnectionTimeout:    time.Second,
			Endpoints:                []types.Endpoint{{URL: "wss://example.invalid/stream"}},
			HandshakeTimeout:         time.Second,
			EnableCompression:        false,
			ReadTimeout:              time.Second,
			WriteTimeout:             time.Second,
			PingInterval:             time.Second,
			WriteInterval:            time.Second,
			MaxTickersPerConnection:  1,
			MaxSubscriptionsPerBatch: 1,
		},
	}
}
