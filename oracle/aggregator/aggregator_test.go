package aggregator

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

	"noah/oracle/types"
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

	aggregator := NewMedianAggregator()
	aggregator.SetProviderPrices("binance", types.Prices{
		"uusd": mustBigFloat(t, "1.20"),
		"ukrw": mustBigFloat(t, "0.10"),
	})
	aggregator.SetProviderPrices("coinbase", types.Prices{
		"uusd": mustBigFloat(t, "1.40"),
	})

	aggregator.AggregatePrices()

	families, err := registry.Gather()
	require.NoError(t, err)

	providerPrices := metricFamily(t, families, "noah_oracle_provider_price")
	require.Equal(t, float64(1.20), gaugeValue(t, providerPrices, map[string]string{
		"provider": "binance",
		"denom":    "uusd",
	}))
	require.Equal(t, float64(1.40), gaugeValue(t, providerPrices, map[string]string{
		"provider": "coinbase",
		"denom":    "uusd",
	}))

	contributions := metricFamily(t, families, "noah_oracle_provider_contributions_total")
	require.Equal(t, float64(1), counterValue(t, contributions, map[string]string{
		"provider": "binance",
		"denom":    "uusd",
		"status":   "success",
	}))
	require.Equal(t, float64(1), counterValue(t, contributions, map[string]string{
		"provider": "coinbase",
		"denom":    "uusd",
		"status":   "success",
	}))

	providerCounts := metricFamily(t, families, "noah_oracle_provider_count")
	require.Equal(t, float64(2), gaugeValue(t, providerCounts, map[string]string{
		"denom": "uusd",
	}))
	require.Equal(t, float64(1), gaugeValue(t, providerCounts, map[string]string{
		"denom": "ukrw",
	}))

	aggregatePrices := metricFamily(t, families, "noah_oracle_aggregate_price")
	require.Equal(t, float64(1.30), gaugeValue(t, aggregatePrices, map[string]string{
		"denom": "uusd",
	}))
	require.Equal(t, float64(0.10), gaugeValue(t, aggregatePrices, map[string]string{
		"denom": "ukrw",
	}))
}

func mustBigFloat(t *testing.T, value string) *big.Float {
	t.Helper()

	price, _, err := big.ParseFloat(value, 10, 256, big.ToNearestEven)
	require.NoError(t, err)
	return price
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
