package resolver_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"ark/oracle/sidecar/resolver"
	"ark/oracle/sidecar/types"
)

func TestAggregatePricesRecordsMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	resolved := resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"binance": {
			"NOAH/USD": mustBigFloat(t, "1.20"),
			"NOAH/KRW": mustBigFloat(t, "0.10"),
		},
		"coinbase": {
			"NOAH/USD": mustBigFloat(t, "1.40"),
		},
	}, []string{"ausd", "akrw"}, time.Now().UTC())

	families, err := registry.Gather()
	require.NoError(t, err)

	providerPrices := metricFamily(t, families, "ark_oracle_provider_price")
	require.Equal(t, float64(1.20), gaugeValue(t, providerPrices, map[string]string{
		"provider": "binance",
		"pair":     "noah/usd",
	}))
	require.Equal(t, float64(1.40), gaugeValue(t, providerPrices, map[string]string{
		"provider": "coinbase",
		"pair":     "noah/usd",
	}))

	families, err = registry.Gather()
	require.NoError(t, err)

	pairSampleCounts := metricFamily(t, families, "ark_oracle_pair_sample_count")
	require.Equal(t, float64(2), gaugeValue(t, pairSampleCounts, map[string]string{
		"pair": "noah/usd",
	}))
	require.Equal(t, float64(1), gaugeValue(t, pairSampleCounts, map[string]string{
		"pair": "noah/krw",
	}))

	aggregatePrices := metricFamily(t, families, "ark_oracle_aggregate_price")
	require.Equal(t, float64(1.30), gaugeValue(t, aggregatePrices, map[string]string{
		"pair": "noah/usd",
	}))
	require.Equal(t, float64(0.10), gaugeValue(t, aggregatePrices, map[string]string{
		"pair": "noah/krw",
	}))

	require.Contains(t, resolved, types.Pair("NOAH/USD"))

	routedCfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"akrw": {
				{
					Name:  "noah-usd-krw",
					Pairs: []types.Pair{"NOAH/USD", "USD/KRW"},
				},
				{
					Name:  "noah-usdt-krw",
					Pairs: []types.Pair{"NOAH/USDT", "USDT/KRW"},
				},
			},
		},
	}
	resolver.ResolvePrices(context.Background(), routedCfg, map[string]types.Prices{
		"binance": {
			"NOAH/USD":  mustBigFloat(t, "2"),
			"USD/KRW":   mustBigFloat(t, "1000"),
			"NOAH/USDT": mustBigFloat(t, "2"),
			"USDT/KRW":  mustBigFloat(t, "1100"),
		},
	}, []string{"akrw"}, time.Now().UTC())

	families, err = registry.Gather()
	require.NoError(t, err)

	resolvedSourceCounts := metricFamily(t, families, "ark_oracle_resolved_source_count")
	require.Equal(t, float64(2), gaugeValue(t, resolvedSourceCounts, map[string]string{
		"pair": "noah/krw",
	}))

	routePrices := metricFamily(t, families, "ark_oracle_route_price")
	noahUSDRoutePrice := matchingMetric(t, routePrices, map[string]string{
		"pair":  "noah/krw",
		"route": "noah-usd-krw",
	})
	require.Equal(t, float64(2000), noahUSDRoutePrice.GetGauge().GetValue())
	requireNoLabel(t, noahUSDRoutePrice, "denom")
	noahUSDTRoutePrice := matchingMetric(t, routePrices, map[string]string{
		"pair":  "noah/krw",
		"route": "noah-usdt-krw",
	})
	require.Equal(t, float64(2200), noahUSDTRoutePrice.GetGauge().GetValue())
	requireNoLabel(t, noahUSDTRoutePrice, "denom")

	require.Nil(t, findMetricFamily(families, "ark_oracle_route_premium"))

	now := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	resolver.ResolvePrices(context.Background(), resolver.Config{
		BootstrapPrices: []resolver.BootstrapPrice{{
			Pair:       "NOAH/USD",
			Price:      "0.25",
			ValidUntil: now.Add(time.Hour).Format(time.RFC3339),
		}},
	}, nil, []string{"ausd"}, now)

	families, err = registry.Gather()
	require.NoError(t, err)
	bootstrapPriceUses := metricFamily(t, families, "ark_oracle_bootstrap_price_uses_total")
	require.Equal(t, float64(1), matchingMetric(t, bootstrapPriceUses, map[string]string{
		"pair": "noah/usd",
	}).GetCounter().GetValue())
}

func TestResolvePricesAveragesConfiguredRoutePricesForVoteTarget(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"akrw": {
				{
					Name:  "noah-usd-krw",
					Pairs: []types.Pair{"NOAH/USD", "USD/KRW"},
				},
				{
					Name:  "noah-usdt-krw",
					Pairs: []types.Pair{"NOAH/USDT", "USDT/KRW"},
				},
				{
					Name:  "noah-eur-krw",
					Pairs: []types.Pair{"NOAH/EUR", "EUR/KRW"},
				},
			},
		},
	}
	prices := resolver.ResolvePrices(context.Background(), cfg, map[string]types.Prices{
		"binance": {
			"NOAH/USD":   mustBigFloat(t, "2"),
			"USD/KRW":    mustBigFloat(t, "1000"),
			"NOAH/USDT":  mustBigFloat(t, "2"),
			"USDT/KRW":   mustBigFloat(t, "1100"),
			"NOAH/EUR":   mustBigFloat(t, "2"),
			"EUR/KRW":    mustBigFloat(t, "1800"),
			"UNUSED/USD": mustBigFloat(t, "999"),
		},
	}, []string{"akrw"}, time.Now().UTC())
	require.Len(t, prices, 1)
	requireBigFloatEqual(t, "2600", prices[types.Pair("NOAH/KRW")])
}

func TestResolvePricesFallsBackToRequestedDirectPairs(t *testing.T) {
	cfg := testResolverConfig("akrw", "noah-usd-krw", "NOAH/USD", "USD/KRW")
	prices := resolver.ResolvePrices(context.Background(), cfg, map[string]types.Prices{
		"binance": {
			"NOAH/USD": mustBigFloat(t, "2"),
			"USD/KRW":  mustBigFloat(t, "1000"),
		},
	}, []string{"ausd", "akrw"}, time.Now().UTC())
	require.Len(t, prices, 2)
	requireBigFloatEqual(t, "2", prices[types.Pair("NOAH/USD")])
	requireBigFloatEqual(t, "2000", prices[types.Pair("NOAH/KRW")])
}

func TestResolvePricesUsesDefaultDirectPathForEmptyRoutes(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"ausd": {},
		},
	}
	prices := resolver.ResolvePrices(context.Background(), cfg, map[string]types.Prices{
		"binance": {
			"NOAH/USD": mustBigFloat(t, "2"),
		},
	}, []string{"ausd"}, time.Now().UTC())
	require.Len(t, prices, 1)
	requireBigFloatEqual(t, "2", prices[types.Pair("NOAH/USD")])
}

func TestResolvePricesUsesInverseStepPrice(t *testing.T) {
	cfg := testResolverConfig("akrw", "noah-usd-krw", "NOAH/USD", "USD/KRW")
	prices := resolver.ResolvePrices(context.Background(), cfg, map[string]types.Prices{
		"binance": {
			"NOAH/USD": mustBigFloat(t, "3"),
			"KRW/USD":  mustBigFloat(t, "0.5"),
		},
	}, []string{"akrw"}, time.Now().UTC())

	requireBigFloatEqual(t, "6", prices[types.Pair("NOAH/KRW")])
}

func TestResolvePricesAggregatesDirectAndInverseProviders(t *testing.T) {
	prices := resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"direct": {
			"NOAH/USD": mustBigFloat(t, "1"),
		},
		"inverse-one": {
			"USD/NOAH": mustBigFloat(t, "0.5"),
		},
		"inverse-two": {
			"USD/NOAH": mustBigFloat(t, "0.25"),
		},
	}, []string{"ausd"}, time.Now().UTC())

	requireBigFloatEqual(t, "2", prices[types.Pair("NOAH/USD")])
}

func TestResolvePricesCountsProviderOnceWhenBothOrientationsExist(t *testing.T) {
	prices := resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"both": {
			"NOAH/USD": mustBigFloat(t, "1"),
			"USD/NOAH": mustBigFloat(t, "0.25"),
		},
		"direct": {
			"NOAH/USD": mustBigFloat(t, "3"),
		},
	}, []string{"ausd"}, time.Now().UTC())

	requireBigFloatEqual(t, "2", prices[types.Pair("NOAH/USD")])
}

func TestResolvePricesDoesNotRetainProviderSnapshots(t *testing.T) {
	resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"first": {
			"NOAH/USD": mustBigFloat(t, "1"),
		},
	}, []string{"ausd"}, time.Now().UTC())

	prices := resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"second": {
			"NOAH/USD": mustBigFloat(t, "5"),
		},
	}, []string{"ausd"}, time.Now().UTC())

	requireBigFloatEqual(t, "5", prices[types.Pair("NOAH/USD")])
}

func TestResolvePricesIgnoresInfinitePrices(t *testing.T) {
	prices := resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"invalid": {
			"NOAH/USD": new(big.Float).SetInf(false),
		},
		"valid": {
			"NOAH/USD": mustBigFloat(t, "2"),
		},
	}, []string{"ausd"}, time.Now().UTC())

	requireBigFloatEqual(t, "2", prices[types.Pair("NOAH/USD")])
}

func TestResolvePricesIgnoresPricesOutsideLegacyDecRange(t *testing.T) {
	tooLarge := new(big.Float).SetPrec(types.PricePrecisionBits)
	tooLarge.SetInt(new(big.Int).Lsh(big.NewInt(1), 256))
	prices := resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"invalid": {
			"NOAH/USD": tooLarge,
		},
		"valid": {
			"NOAH/USD": mustBigFloat(t, "2"),
		},
	}, []string{"ausd"}, time.Now().UTC())

	requireBigFloatEqual(t, "2", prices[types.Pair("NOAH/USD")])
}

func TestResolvePricesPreservesInputPrecision(t *testing.T) {
	price := mustBigFloat(t, "1234.123456789123456789")
	prices := resolver.ResolvePrices(context.Background(), resolver.Config{}, map[string]types.Prices{
		"provider": {
			"NOAH/USD": price,
		},
	}, []string{"ausd"}, time.Now().UTC())

	resolved := prices[types.Pair("NOAH/USD")]
	require.NotNil(t, resolved)
	require.Equal(t, price.Prec(), resolved.Prec())
	require.Zero(t, price.Cmp(resolved))
}

func TestResolvePricesUsesBootstrapPriceWhenProviderSampleIsMissing(t *testing.T) {
	now := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	cfg := resolver.Config{
		BootstrapPrices: []resolver.BootstrapPrice{{
			Pair:       "NOAH/USD",
			Price:      "0.25",
			ValidUntil: now.Add(time.Hour).Format(time.RFC3339),
		}},
	}

	prices := resolver.ResolvePrices(context.Background(), cfg, nil, []string{"ausd"}, now)

	requireBigFloatEqual(t, "0.25", prices[types.Pair("NOAH/USD")])
}

func TestResolvePricesProviderSampleOverridesBootstrapPrice(t *testing.T) {
	now := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	cfg := resolver.Config{
		BootstrapPrices: []resolver.BootstrapPrice{{
			Pair:       "NOAH/USD",
			Price:      "10",
			ValidUntil: now.Add(time.Hour).Format(time.RFC3339),
		}},
	}
	testCases := []struct {
		name   string
		prices types.Prices
	}{
		{
			name: "direct provider sample",
			prices: types.Prices{
				"NOAH/USD": mustBigFloat(t, "2"),
			},
		},
		{
			name: "inverse provider sample",
			prices: types.Prices{
				"USD/NOAH": mustBigFloat(t, "0.5"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prices := resolver.ResolvePrices(
				context.Background(),
				cfg,
				map[string]types.Prices{"provider": tc.prices},
				[]string{"ausd"},
				now,
			)

			requireBigFloatEqual(t, "2", prices[types.Pair("NOAH/USD")])
		})
	}
}

func TestResolvePricesIgnoresExpiredBootstrapPrice(t *testing.T) {
	now := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	cfg := resolver.Config{
		BootstrapPrices: []resolver.BootstrapPrice{{
			Pair:       "NOAH/USD",
			Price:      "0.25",
			ValidUntil: now.Format(time.RFC3339),
		}},
	}

	prices := resolver.ResolvePrices(context.Background(), cfg, nil, []string{"ausd"}, now)

	require.Empty(t, prices)
}

func TestResolvePricesUsesBootstrapPriceAsRouteLeg(t *testing.T) {
	now := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	cfg := testResolverConfig("akrw", "noah-usd-krw", "NOAH/USD", "USD/KRW")
	cfg.BootstrapPrices = []resolver.BootstrapPrice{{
		Pair:       "NOAH/USD",
		Price:      "2",
		ValidUntil: now.Add(time.Hour).Format(time.RFC3339),
	}}

	prices := resolver.ResolvePrices(
		context.Background(),
		cfg,
		map[string]types.Prices{
			"frankfurter": {
				"USD/KRW": mustBigFloat(t, "1000"),
			},
		},
		[]string{"akrw"},
		now,
	)

	requireBigFloatEqual(t, "2000", prices[types.Pair("NOAH/KRW")])
}

func TestConfigValidateRejectsInvalidConfig(t *testing.T) {
	cfg := resolver.Config{
		Routes: map[string][]resolver.Route{
			"akrw": {
				{
					Pairs: []types.Pair{"NOAH/USD"},
				},
			},
		},
	}
	err := cfg.Validate()

	require.ErrorContains(t, err, "route name cannot be empty")
}

func testResolverConfig(denom, routeName string, pairs ...types.Pair) resolver.Config {
	return resolver.Config{
		Routes: map[string][]resolver.Route{
			denom: {
				{
					Name:  routeName,
					Pairs: pairs,
				},
			},
		},
	}
}

func mustBigFloat(t *testing.T, value string) *big.Float {
	t.Helper()

	price, _, err := big.ParseFloat(value, 10, 256, big.ToNearestEven)
	require.NoError(t, err)
	return price
}

func requireBigFloatEqual(t *testing.T, want string, got *big.Float) {
	t.Helper()

	require.NotNil(t, got)
	require.Equal(t, 0, got.Cmp(mustBigFloat(t, want)))
}

func metricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()

	if family := findMetricFamily(families, name); family != nil {
		return family
	}

	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	t.Fatalf("metric family %q not found in %v", name, names)
	return nil
}

func findMetricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	return nil
}

func gaugeValue(
	t *testing.T,
	family *dto.MetricFamily,
	labels map[string]string,
) float64 {
	t.Helper()

	return matchingMetric(t, family, labels).GetGauge().GetValue()
}

func matchingMetric(
	t *testing.T,
	family *dto.MetricFamily,
	labels map[string]string,
) *dto.Metric {
	t.Helper()

	for _, metric := range family.Metric {
		actual := make(map[string]string, len(metric.Label))
		for _, label := range metric.Label {
			actual[label.GetName()] = label.GetValue()
		}
		matches := true
		for name, value := range labels {
			if actual[name] != value {
				matches = false
				break
			}
		}
		if matches {
			return metric
		}
	}

	t.Fatalf("metric with labels %v not found", labels)
	return nil
}

func requireNoLabel(t *testing.T, metric *dto.Metric, name string) {
	t.Helper()

	for _, label := range metric.Label {
		require.NotEqual(t, name, label.GetName())
	}
}
