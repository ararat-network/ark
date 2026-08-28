package metrics_test

import (
	. "ark/pricefeed/sidecar/providers/base/metrics"
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"ark/pricefeed/sidecar/providers/types"
)

func TestRecordResponse(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	RecordResponse(
		context.Background(),
		"kraken",
		types.Ticker("ATOM/USD"),
		"api",
		types.OK,
	)
	RecordResponse(
		context.Background(),
		"kraken",
		types.Ticker("BTC/USD"),
		"api",
		types.ErrorNoResponse,
	)

	families, err := registry.Gather()
	require.NoError(t, err)

	responses := metricFamily(t, families, "ark_pricefeed_provider_responses_total")
	require.Len(t, responses.Metric, 2)
	require.Equal(t, float64(1), counterValue(t, responses, map[string]string{
		"provider":   "kraken",
		"ticker":     "ATOM/USD",
		"fetcher":    "api",
		"status":     "success",
		"error_code": "0",
	}))
	require.Equal(t, float64(1), counterValue(t, responses, map[string]string{
		"provider":   "kraken",
		"ticker":     "BTC/USD",
		"fetcher":    "api",
		"status":     "failure",
		"error_code": "7",
	}))

	lastSuccess := metricFamily(t, families, "ark_pricefeed_provider_last_success_seconds")
	require.Len(t, lastSuccess.Metric, 1)
	require.Positive(t, gaugeValue(t, lastSuccess, map[string]string{
		"provider": "kraken",
		"ticker":   "ATOM/USD",
		"fetcher":  "api",
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
