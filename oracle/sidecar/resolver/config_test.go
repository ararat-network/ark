package resolver_test

import (
	. "noah/oracle/sidecar/resolver"
	"testing"

	"github.com/stretchr/testify/require"

	"noah/oracle/sidecar/types"
)

func TestConfigMarketPairs(t *testing.T) {
	testCases := []struct {
		name string
		cfg  Config
		want map[types.Pair]struct{}
	}{
		{
			name: "nil routes returns nil",
			cfg:  Config{},
			want: nil,
		},
		{
			name: "empty routes returns nil",
			cfg: Config{
				Routes: map[string][]Route{},
			},
			want: nil,
		},
		{
			name: "includes route pairs and inverses",
			cfg: Config{
				Routes: map[string][]Route{
					"ukrw": {
						{
							Name:  "ark-krw",
							Pairs: []types.Pair{"ARK/USD", "USD/KRW"},
						},
					},
				},
			},
			want: map[types.Pair]struct{}{
				"ARK/USD": {},
				"USD/ARK": {},
				"USD/KRW": {},
				"KRW/USD": {},
			},
		},
		{
			name: "collapses duplicate direct and inverse pairs",
			cfg: Config{
				Routes: map[string][]Route{
					"uusd": {
						{
							Name:  "ark-usd",
							Pairs: []types.Pair{"ARK/USD", "USD/ARK", "ARK/USD"},
						},
					},
				},
			},
			want: map[types.Pair]struct{}{
				"ARK/USD": {},
				"USD/ARK": {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.cfg.MarketPairs())
		})
	}
}

func TestConfigEqual(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{
			name: "equal",
			want: true,
		},
		{
			name: "route denom differs",
			mutate: func(cfg *Config) {
				cfg.Routes["ukrw"] = cfg.Routes["uusd"]
				delete(cfg.Routes, "uusd")
			},
		},
		{
			name: "route count differs",
			mutate: func(cfg *Config) {
				cfg.Routes["uusd"] = append(cfg.Routes["uusd"], Route{
					Name:  "fallback",
					Pairs: []types.Pair{"USD/KRW"},
				})
			},
		},
		{
			name: "route name differs",
			mutate: func(cfg *Config) {
				cfg.Routes["uusd"][0].Name = "fallback"
			},
		},
		{
			name: "route pairs differ",
			mutate: func(cfg *Config) {
				cfg.Routes["uusd"][0].Pairs[0] = "USD/ARK"
			},
		},
		{
			name: "route order differs",
			mutate: func(cfg *Config) {
				cfg.Routes["uusd"][0], cfg.Routes["uusd"][1] = cfg.Routes["uusd"][1], cfg.Routes["uusd"][0]
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := Config{
				Routes: map[string][]Route{
					"uusd": {
						{
							Name:  "ark-usd",
							Pairs: []types.Pair{"ARK/USD"},
						},
						{
							Name:  "ark-usdt-usd",
							Pairs: []types.Pair{"ARK/USDT", "USDT/USD"},
						},
					},
				},
			}
			b := Config{
				Routes: map[string][]Route{
					"uusd": {
						{
							Name:  "ark-usd",
							Pairs: []types.Pair{"ARK/USD"},
						},
						{
							Name:  "ark-usdt-usd",
							Pairs: []types.Pair{"ARK/USDT", "USDT/USD"},
						},
					},
				},
			}
			if tc.mutate != nil {
				tc.mutate(&b)
			}

			require.Equal(t, tc.want, a.Equal(b))
		})
	}
}

func TestConfigValidateRejectsRouteOutputMismatch(t *testing.T) {
	cfg := Config{
		Routes: map[string][]Route{
			"ukrw": {
				{
					Name:  "ark-usd",
					Pairs: []types.Pair{"ARK/USD"},
				},
			},
		},
	}

	err := cfg.Validate()

	require.ErrorContains(t, err, `resolver denom "ukrw" route "ark-usd" resolves to "ARK/USD", want "ARK/KRW"`)
}

func TestConfigValidateRejectsDisconnectedRoute(t *testing.T) {
	cfg := Config{
		Routes: map[string][]Route{
			"ukrw": {
				{
					Name:  "bad-path",
					Pairs: []types.Pair{"ARK/USD", "EUR/KRW"},
				},
			},
		},
	}

	err := cfg.Validate()

	require.ErrorContains(t, err, `resolver denom "ukrw" route "bad-path" is invalid: pair "EUR/KRW" does not connect after "USD"`)
}
