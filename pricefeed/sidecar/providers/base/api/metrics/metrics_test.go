package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api/metrics"
)

func TestRecordAPIMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	RecordRequest(
		context.Background(),
		"kraken",
		time.Millisecond,
		&http.Response{StatusCode: http.StatusInternalServerError},
		nil,
	)
	RecordRequest(context.Background(), "kraken", time.Millisecond, nil, errors.New("dial tcp: refused"))

	families, err := registry.Gather()
	require.NoError(t, err)

	durations := metricFamily(t, families, "ark_pricefeed_provider_api_request_duration_milliseconds")
	require.Equal(t, uint64(1), histogramSampleCount(t, durations, map[string]string{
		"provider":    "kraken",
		"status_code": "500",
	}))
	// A transport failure has no response, so the code reads "none".
	require.Equal(t, uint64(1), histogramSampleCount(t, durations, map[string]string{
		"provider":    "kraken",
		"status_code": "none",
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

func histogramSampleCount(
	t *testing.T,
	family *dto.MetricFamily,
	labels map[string]string,
) uint64 {
	t.Helper()

	return matchingMetric(t, family, labels).GetHistogram().GetSampleCount()
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
