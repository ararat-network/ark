package resolver_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/types"
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

	aggregator, err := resolver.NewResolver(resolver.Config{})
	require.NoError(t, err)
	aggregator.SetProviderPrices("binance", types.Prices{
		"USDT/USD": mustBigFloat(t, "1.20"),
		"USDT/KRW": mustBigFloat(t, "0.10"),
	})
	aggregator.SetProviderPrices("coinbase", types.Prices{
		"USDT/USD": mustBigFloat(t, "1.40"),
	})

	families, err := registry.Gather()
	require.NoError(t, err)

	providerPrices := metricFamily(t, families, "noah_oracle_provider_price")
	require.Equal(t, float64(1.20), gaugeValue(t, providerPrices, map[string]string{
		"provider": "binance",
		"pair":     "usdt/usd",
	}))
	require.Equal(t, float64(1.40), gaugeValue(t, providerPrices, map[string]string{
		"provider": "coinbase",
		"pair":     "usdt/usd",
	}))

	aggregator.ResolvePrices([]string{"uusd", "ukrw"})

	families, err = registry.Gather()
	require.NoError(t, err)

	pairSampleCounts := metricFamily(t, families, "noah_oracle_pair_sample_count")
	require.Equal(t, float64(2), gaugeValue(t, pairSampleCounts, map[string]string{
		"pair": "usdt/usd",
	}))
	require.Equal(t, float64(1), gaugeValue(t, pairSampleCounts, map[string]string{
		"pair": "usdt/krw",
	}))

	aggregatePrices := metricFamily(t, families, "noah_oracle_aggregate_price")
	require.Equal(t, float64(1.30), gaugeValue(t, aggregatePrices, map[string]string{
		"pair": "usdt/usd",
	}))
	require.Equal(t, float64(0.10), gaugeValue(t, aggregatePrices, map[string]string{
		"pair": "usdt/krw",
	}))

	require.Contains(t, aggregator.GetPrices(), types.Pair("USDT/USD"))

	routed, err := resolver.NewResolver(resolver.Config{
		Routes: map[string][]resolver.Route{
			"ukrw": {
				{
					Name:  "ark-usd-krw",
					Pairs: []types.Pair{"ARK/USD", "USD/KRW"},
				},
				{
					Name:  "ark-usdt-krw",
					Pairs: []types.Pair{"ARK/USDT", "USDT/KRW"},
				},
			},
		},
	})
	require.NoError(t, err)
	routed.SetProviderPrices("binance", types.Prices{
		"ARK/USD":  mustBigFloat(t, "2"),
		"USD/KRW":  mustBigFloat(t, "1000"),
		"ARK/USDT": mustBigFloat(t, "2"),
		"USDT/KRW": mustBigFloat(t, "1100"),
	})
	routed.ResolvePrices([]string{"ukrw"})

	families, err = registry.Gather()
	require.NoError(t, err)

	resolvedSourceCounts := metricFamily(t, families, "noah_oracle_resolved_source_count")
	require.Equal(t, float64(2), gaugeValue(t, resolvedSourceCounts, map[string]string{
		"pair": "ark/krw",
	}))

	routePrices := metricFamily(t, families, "noah_oracle_route_price")
	arkUSDRoutePrice := matchingMetric(t, routePrices, map[string]string{
		"pair":  "ark/krw",
		"route": "ark-usd-krw",
	})
	require.Equal(t, float64(2000), arkUSDRoutePrice.GetGauge().GetValue())
	requireNoLabel(t, arkUSDRoutePrice, "denom")
	arkUSDTRoutePrice := matchingMetric(t, routePrices, map[string]string{
		"pair":  "ark/krw",
		"route": "ark-usdt-krw",
	})
	require.Equal(t, float64(2200), arkUSDTRoutePrice.GetGauge().GetValue())
	requireNoLabel(t, arkUSDTRoutePrice, "denom")

	require.Nil(t, findMetricFamily(families, "noah_oracle_route_premium"))
}

func TestResolvePricesAveragesConfiguredRoutePricesForVoteTarget(t *testing.T) {
	resolver, err := resolver.NewResolver(resolver.Config{
		Routes: map[string][]resolver.Route{
			"ukrw": {
				{
					Name:  "ark-usd-krw",
					Pairs: []types.Pair{"ARK/USD", "USD/KRW"},
				},
				{
					Name:  "ark-usdt-krw",
					Pairs: []types.Pair{"ARK/USDT", "USDT/KRW"},
				},
				{
					Name:  "ark-eur-krw",
					Pairs: []types.Pair{"ARK/EUR", "EUR/KRW"},
				},
			},
		},
	})
	require.NoError(t, err)
	resolver.SetProviderPrices("binance", types.Prices{
		"ARK/USD":    mustBigFloat(t, "2"),
		"USD/KRW":    mustBigFloat(t, "1000"),
		"ARK/USDT":   mustBigFloat(t, "2"),
		"USDT/KRW":   mustBigFloat(t, "1100"),
		"ARK/EUR":    mustBigFloat(t, "2"),
		"EUR/KRW":    mustBigFloat(t, "1800"),
		"UNUSED/USD": mustBigFloat(t, "999"),
	})

	resolver.ResolvePrices([]string{"ukrw"})

	prices := resolver.GetPrices()
	require.Len(t, prices, 1)
	requireBigFloatEqual(t, "2600", prices[types.Pair("ARK/KRW")])
}

func TestResolvePricesFallsBackToRequestedDirectPairs(t *testing.T) {
	resolver, err := resolver.NewResolver(testResolverConfig("ukrw", "ark-usd-krw", "ARK/USD", "USD/KRW"))
	require.NoError(t, err)
	resolver.SetProviderPrices("binance", types.Prices{
		"ARK/USD": mustBigFloat(t, "2"),
		"USD/KRW": mustBigFloat(t, "1000"),
	})

	resolver.ResolvePrices([]string{"uusd", "ukrw"})

	prices := resolver.GetPrices()
	require.Len(t, prices, 2)
	requireBigFloatEqual(t, "2", prices[types.Pair("ARK/USD")])
	requireBigFloatEqual(t, "2000", prices[types.Pair("ARK/KRW")])
}

func TestResolvePricesUsesInverseStepPrice(t *testing.T) {
	resolver, err := resolver.NewResolver(testResolverConfig("ukrw", "ark-usd-krw", "ARK/USD", "USD/KRW"))
	require.NoError(t, err)
	resolver.SetProviderPrices("binance", types.Prices{
		"ARK/USD": mustBigFloat(t, "3"),
		"KRW/USD": mustBigFloat(t, "0.5"),
	})

	resolver.ResolvePrices([]string{"ukrw"})

	requireBigFloatEqual(t, "6", resolver.GetPrices()[types.Pair("ARK/KRW")])
}

func TestUpdateConfigSwapsConfigAndClearsCachedPrices(t *testing.T) {
	oldCfg := testResolverConfig("uusd", "ark-usd", "ARK/USD")
	newCfg := testResolverConfig("ukrw", "ark-krw", "ARK/USD", "USD/KRW")
	resolver, err := resolver.NewResolver(oldCfg)
	require.NoError(t, err)
	resolver.SetProviderPrices("binance", types.Prices{
		"ARK/USD": mustBigFloat(t, "1.20"),
	})
	resolver.ResolvePrices([]string{"uusd"})
	require.NotEmpty(t, resolver.GetPrices())

	require.NoError(t, resolver.UpdateConfig(newCfg))

	require.Empty(t, resolver.GetPrices())
}

func TestUpdateConfigRejectsInvalidConfigWithoutMutation(t *testing.T) {
	oldCfg := testResolverConfig("uusd", "ark-usd", "ARK/USD")
	r, err := resolver.NewResolver(oldCfg)
	require.NoError(t, err)
	r.SetProviderPrices("binance", types.Prices{
		"ARK/USD": mustBigFloat(t, "1.20"),
	})
	r.ResolvePrices([]string{"uusd"})
	before := r.GetPrices()
	require.NotEmpty(t, before)

	err = r.UpdateConfig(resolver.Config{
		Routes: map[string][]resolver.Route{
			"ukrw": {
				{
					Pairs: []types.Pair{"ARK/USD"},
				},
			},
		},
	})

	require.ErrorContains(t, err, "route name cannot be empty")
	require.Equal(t, before, r.GetPrices())
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
