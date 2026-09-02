package metrics_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/ararat-network/ark/pricefeed/sidecar/metrics"
)

func TestRecordOracleMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	metrics.RecordOracleTick(context.Background())
	metrics.RecordProviderPrice(context.Background(), "kraken", "USD/NOAH", 1.23)
	metrics.RecordAggregatePrice(context.Background(), "USD/NOAH", 1.25)
	metrics.RecordPairSampleCount(context.Background(), "USD/NOAH", 2)
	metrics.RecordResolvedSourceCount(context.Background(), "KRW/NOAH", 3)
	metrics.RecordRoutePrice(context.Background(), "KRW/NOAH", "krw-usd-noah", 2000)
	metrics.RecordMissingPrices(context.Background(), []string{"akrw"})

	families, err := registry.Gather()
	require.NoError(t, err)

	ticks := metricFamily(t, families, "ark_pricefeed_ticks_total")
	require.Equal(t, float64(1), counterValue(t, ticks, nil))

	providerPrices := metricFamily(t, families, "ark_pricefeed_provider_price")
	require.Equal(t, float64(1.23), gaugeValue(t, providerPrices, map[string]string{
		"provider": "kraken",
		"pair":     "usd/noah",
	}))

	aggregatePrices := metricFamily(t, families, "ark_pricefeed_aggregate_price")
	require.Equal(t, float64(1.25), gaugeValue(t, aggregatePrices, map[string]string{
		"pair": "usd/noah",
	}))

	pairSampleCounts := metricFamily(t, families, "ark_pricefeed_pair_sample_count")
	require.Equal(t, float64(2), gaugeValue(t, pairSampleCounts, map[string]string{
		"pair": "usd/noah",
	}))

	resolvedSourceCounts := metricFamily(t, families, "ark_pricefeed_resolved_source_count")
	require.Equal(t, float64(3), gaugeValue(t, resolvedSourceCounts, map[string]string{
		"pair": "krw/noah",
	}))

	routePrices := metricFamily(t, families, "ark_pricefeed_route_price")
	routePrice := matchingMetric(t, routePrices, map[string]string{
		"pair":  "krw/noah",
		"route": "krw-usd-noah",
	})
	require.Equal(t, float64(2000), routePrice.GetGauge().GetValue())
	requireNoLabel(t, routePrice, "denom")

	missingPrices := metricFamily(t, families, "ark_pricefeed_missing_prices_total")
	require.Equal(t, float64(1), counterValue(t, missingPrices, map[string]string{
		"denom": "akrw",
	}))
}

func metricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()

	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
		if family.GetName() == name {
			return family
		}
	}

	t.Fatalf("metric family %q not found in %v", name, names)
	return nil
}

func counterValue(
	t *testing.T,
	family *dto.MetricFamily,
	labels map[string]string,
) float64 {
	t.Helper()

	return matchingMetric(t, family, labels).GetCounter().GetValue()
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
