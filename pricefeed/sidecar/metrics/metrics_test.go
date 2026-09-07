package metrics_test

import (
	"context"
	"sync"
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

	metrics.RecordTick(context.Background())
	metrics.PublishAggregationSnapshot(metrics.AggregationSnapshot{
		Prices:       map[string]float64{"USD/NOAH": 1.25},
		SampleCounts: map[string]int64{"USD/NOAH": 2},
	})
	metrics.RecordMissingPrices(context.Background(), []string{"akrw"})
	metrics.RecordSkippedSample(context.Background(), "kraken", "USD/NOAH", metrics.SkipReasonUnchanged)
	metrics.RecordRPC(context.Background(), "/ark.pricefeed.v1.PriceFeed/Prices", "OK")

	families, err := registry.Gather()
	require.NoError(t, err)

	ticks := metricFamily(t, families, "ark_pricefeed_ticks_total")
	require.Equal(t, float64(1), counterValue(t, ticks, nil))

	aggregatePrices := metricFamily(t, families, "ark_pricefeed_aggregate_price")
	require.Equal(t, float64(1.25), gaugeValue(t, aggregatePrices, map[string]string{
		"pair": "usd/noah",
	}))

	pairSampleCounts := metricFamily(t, families, "ark_pricefeed_pair_sample_count")
	require.Equal(t, float64(2), gaugeValue(t, pairSampleCounts, map[string]string{
		"pair": "usd/noah",
	}))

	missingPrices := metricFamily(t, families, "ark_pricefeed_missing_prices_total")
	require.Equal(t, float64(1), counterValue(t, missingPrices, map[string]string{
		"denom": "akrw",
	}))

	skipped := metricFamily(t, families, "ark_pricefeed_skipped_samples_total")
	require.Equal(t, float64(1), counterValue(t, skipped, map[string]string{
		"provider": "kraken",
		"pair":     "usd/noah",
		"reason":   metrics.SkipReasonUnchanged,
	}))

	rpcRequests := metricFamily(t, families, "ark_pricefeed_rpc_requests_total")
	require.Equal(t, float64(1), counterValue(t, rpcRequests, map[string]string{
		"method": "/ark.pricefeed.v1.PriceFeed/Prices",
		"code":   "OK",
	}))
	t.Run("publication owns data and collection is concurrent", func(t *testing.T) {
		snapshot := metrics.AggregationSnapshot{Prices: map[string]float64{"USD/NOAH": 2}, SampleCounts: map[string]int64{"USD/NOAH": 1}}
		metrics.PublishAggregationSnapshot(snapshot)
		snapshot.Prices["USD/NOAH"] = 999
		families, err := registry.Gather()
		require.NoError(t, err)
		require.Equal(t, 2.0, gaugeValue(t, metricFamily(t, families, "ark_pricefeed_aggregate_price"), map[string]string{"pair": "usd/noah"}))
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				metrics.PublishAggregationSnapshot(snapshot)
			}
		}()
		for range 100 {
			_, err := registry.Gather()
			require.NoError(t, err)
		}
		wg.Wait()
	})
	t.Run("unavailable and removed observations disappear", func(t *testing.T) {
		metrics.PublishAggregationSnapshot(metrics.AggregationSnapshot{SampleCounts: map[string]int64{"USD/NOAH": 0}})
		families, err := registry.Gather()
		require.NoError(t, err)
		require.Zero(t, gaugeValue(t, metricFamily(t, families, "ark_pricefeed_pair_sample_count"), map[string]string{"pair": "usd/noah"}))
		for _, family := range families {
			require.NotEqual(t, "ark_pricefeed_aggregate_price", family.GetName())
		}
		metrics.PublishAggregationSnapshot(metrics.AggregationSnapshot{})
		families, err = registry.Gather()
		require.NoError(t, err)
		for _, family := range families {
			require.NotEqual(t, "ark_pricefeed_pair_sample_count", family.GetName())
		}
	})
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
