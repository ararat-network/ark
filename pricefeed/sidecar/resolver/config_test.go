package resolver_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	"github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func TestConfigMarketPairs(t *testing.T) {
	testCases := []struct {
		name  string
		cfg   resolver.Config
		feeds []string
		want  map[types.Pair]struct{}
	}{
		{
			name:  "nil routes includes active direct pairs and inverses",
			cfg:   resolver.Config{},
			feeds: []string{"ausd"},
			want: map[types.Pair]struct{}{
				"NOAH/USD": {},
				"USD/NOAH": {},
			},
		},
		{
			name: "empty routes includes active direct pairs and inverses",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{},
			},
			feeds: []string{"ausd"},
			want: map[types.Pair]struct{}{
				"NOAH/USD": {},
				"USD/NOAH": {},
			},
		},
		{
			name: "includes route pairs and inverses",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{
					"akrw": {
						{
							Name:  "noah-krw",
							Pairs: []types.Pair{"NOAH/USD", "USD/KRW"},
						},
					},
				},
			},
			feeds: []string{"akrw"},
			want: map[types.Pair]struct{}{
				"NOAH/USD": {},
				"USD/NOAH": {},
				"USD/KRW":  {},
				"KRW/USD":  {},
			},
		},
		{
			name: "collapses duplicate direct and inverse pairs",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{
					"ausd": {
						{
							Name:  "noah-usd",
							Pairs: []types.Pair{"NOAH/USD", "USD/NOAH", "NOAH/USD"},
						},
					},
				},
			},
			feeds: []string{"ausd"},
			want: map[types.Pair]struct{}{
				"NOAH/USD": {},
				"USD/NOAH": {},
			},
		},
		{
			name: "adds default direct pairs for feeds without configured routes",
			cfg: resolver.Config{
				Routes: map[string][]resolver.Route{
					"akrw": {
						{
							Name:  "noah-krw",
							Pairs: []types.Pair{"NOAH/USD", "USD/KRW"},
						},
					},
				},
			},
			feeds: []string{"ausd", "akrw"},
			want: map[types.Pair]struct{}{
				"NOAH/USD": {},
				"USD/NOAH": {},
				"USD/KRW":  {},
				"KRW/USD":  {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.cfg.MarketPairs(tc.feeds))
		})
	}
}

func TestConfigValidateAllowsEmptyRoutesForDefaultDirectPath(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"ausd": {},
		},
	}

	require.NoError(t, cfg.Validate())
}

func TestConfigValidateRejectsNonCanonicalDenomWithEmptyRoutes(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"aUSD": {},
		},
	}

	err := cfg.Validate()

	require.ErrorContains(t, err, "Ark-native base denom matching")
}

func TestConfigValidateBootstrapPrices(t *testing.T) {
	valid := resolver.BootstrapPrice{
		Pair:       "NOAH/USD",
		Price:      "0.25",
		ValidUntil: "2030-01-01T00:00:00Z",
	}
	testCases := []struct {
		name      string
		bootstrap []resolver.BootstrapPrice
		expectErr string
	}{
		{
			name:      "valid",
			bootstrap: []resolver.BootstrapPrice{valid},
		},
		{
			name: "invalid pair",
			bootstrap: []resolver.BootstrapPrice{{
				Pair:       "noah/usd",
				Price:      valid.Price,
				ValidUntil: valid.ValidUntil,
			}},
			expectErr: "bootstrap price pair",
		},
		{
			name: "invalid price",
			bootstrap: []resolver.BootstrapPrice{{
				Pair:       valid.Pair,
				Price:      "not-a-price",
				ValidUntil: valid.ValidUntil,
			}},
			expectErr: "bootstrap price for NOAH/USD is invalid",
		},
		{
			name: "non-positive price",
			bootstrap: []resolver.BootstrapPrice{{
				Pair:       valid.Pair,
				Price:      "0",
				ValidUntil: valid.ValidUntil,
			}},
			expectErr: "bootstrap price for NOAH/USD must be positive",
		},
		{
			name: "invalid expiry",
			bootstrap: []resolver.BootstrapPrice{{
				Pair:       valid.Pair,
				Price:      valid.Price,
				ValidUntil: "tomorrow",
			}},
			expectErr: "bootstrap price for NOAH/USD validUntil",
		},
		{
			name:      "duplicate pair",
			bootstrap: []resolver.BootstrapPrice{valid, valid},
			expectErr: "duplicate bootstrap price pair",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := resolver.Config{BootstrapPrices: tc.bootstrap}

			err := cfg.Validate()

			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestConfigCloneCopiesBootstrapPrices(t *testing.T) {
	cfg := resolver.Config{
		BootstrapPrices: []resolver.BootstrapPrice{{
			Pair:       "NOAH/USD",
			Price:      "0.25",
			ValidUntil: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		}},
	}

	cloned := cfg.Clone()
	cloned.BootstrapPrices[0].Price = "1.00"

	require.Equal(t, "0.25", cfg.BootstrapPrices[0].Price)
}

func TestConfigValidateRejectsRouteOutputMismatch(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"akrw": {
				{
					Name:  "noah-usd",
					Pairs: []types.Pair{"NOAH/USD"},
				},
			},
		},
	}

	err := cfg.Validate()

	require.ErrorContains(t, err, `resolver denom "akrw" route "noah-usd" resolves to "NOAH/USD", want "NOAH/KRW"`)
}

func TestConfigValidateRejectsDisconnectedRoute(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"akrw": {
				{
					Name:  "bad-path",
					Pairs: []types.Pair{"NOAH/USD", "EUR/KRW"},
				},
			},
		},
	}

	err := cfg.Validate()

	require.ErrorContains(t, err, `resolver denom "akrw" route "bad-path" is invalid: pair "EUR/KRW" does not connect after "USD"`)
}
