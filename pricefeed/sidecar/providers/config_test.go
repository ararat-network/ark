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
