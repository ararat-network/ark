package resolver_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/types"
)

func TestConfigMarketPairs(t *testing.T) {
	testCases := []struct {
		name   string
		cfg    resolver.Config
		denoms []string
		want   map[types.Pair]struct{}
	}{
		{
			name:   "nil routes includes active direct pairs and inverses",
			cfg:    resolver.Config{},
			denoms: []string{"uusd"},
			want: map[types.Pair]struct{}{
				"ARK/USD": {},
				"USD/ARK": {},
			},
		},
		{
			name: "empty routes includes active direct pairs and inverses",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{},
			},
			denoms: []string{"uusd"},
			want: map[types.Pair]struct{}{
				"ARK/USD": {},
				"USD/ARK": {},
			},
		},
		{
			name: "includes route pairs and inverses",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{
					"ukrw": {
						{
							Name:  "ark-krw",
							Pairs: []types.Pair{"ARK/USD", "USD/KRW"},
						},
					},
				},
			},
			denoms: []string{"ukrw"},
			want: map[types.Pair]struct{}{
				"ARK/USD": {},
				"USD/ARK": {},
				"USD/KRW": {},
				"KRW/USD": {},
			},
		},
		{
			name: "collapses duplicate direct and inverse pairs",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{
					"uusd": {
						{
							Name:  "ark-usd",
							Pairs: []types.Pair{"ARK/USD", "USD/ARK", "ARK/USD"},
						},
					},
				},
			},
			denoms: []string{"uusd"},
			want: map[types.Pair]struct{}{
				"ARK/USD": {},
				"USD/ARK": {},
			},
		},
		{
			name: "adds default direct pairs for denoms without configured routes",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{
					"ukrw": {
						{
							Name:  "ark-krw",
							Pairs: []types.Pair{"ARK/USD", "USD/KRW"},
						},
					},
				},
			},
			denoms: []string{"uusd", "ukrw"},
			want: map[types.Pair]struct{}{
				"ARK/USD": {},
				"USD/ARK": {},
				"USD/KRW": {},
				"KRW/USD": {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.cfg.MarketPairs(tc.denoms))
		})
	}
}

func TestConfigValidateAllowsEmptyRoutesForDefaultDirectPath(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"uusd": {},
		},
	}

	require.NoError(t, cfg.Validate())
}

func TestConfigEqual(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*resolver.Config)
		want   bool
	}{
		{
			name: "equal",
			want: true,
		},
		{
			name: "route denom differs",
			mutate: func(cfg *resolver.Config) {
				cfg.Routes["ukrw"] = cfg.Routes["uusd"]
				delete(cfg.Routes, "uusd")
			},
		},
		{
			name: "route count differs",
			mutate: func(cfg *resolver.Config) {
				cfg.Routes["uusd"] = append(cfg.Routes["uusd"], resolver.Route{
					Name:  "fallback",
					Pairs: []types.Pair{"USD/KRW"},
				})
			},
		},
		{
			name: "route name differs",
			mutate: func(cfg *resolver.Config) {
				cfg.Routes["uusd"][0].Name = "fallback"
			},
		},
		{
			name: "route pairs differ",
			mutate: func(cfg *resolver.Config) {
				cfg.Routes["uusd"][0].Pairs[0] = "USD/ARK"
			},
		},
		{
			name: "route order differs",
			mutate: func(cfg *resolver.Config) {
				cfg.Routes["uusd"][0], cfg.Routes["uusd"][1] = cfg.Routes["uusd"][1], cfg.Routes["uusd"][0]
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := resolver.Config{
				Routes: map[string][]resolver.Route{
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
			b := resolver.Config{
				Routes: map[string][]resolver.Route{
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
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
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
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
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
