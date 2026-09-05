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

	"github.com/ararat-network/ark/pkg/metrics"
)

func TestRecordModuleMethodLatency(t *testing.T) {
	originalProvider := otel.GetMeterProvider()
	t.Cleanup(func() {
		otel.SetMeterProvider(originalProvider)
	})

	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	metrics.RecordModuleMethodLatency(context.Background(), "oracle", metrics.EndBlock)()

	families, err := registry.Gather()
	require.NoError(t, err)

	duration := metricFamily(t, families, "ark_module_method_duration_milliseconds")
	require.Len(t, duration.Metric, 1)

	metric := duration.Metric[0]
	require.Equal(t, uint64(1), metric.GetHistogram().GetSampleCount())
	require.GreaterOrEqual(t, metric.GetHistogram().GetSampleSum(), float64(0))

	labels := metricLabels(metric)
	require.Equal(t, "end_blocker", labels["method"])
	require.Equal(t, "oracle", labels["module"])
}

func metricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()

	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	t.Fatalf("metric family %q not found", name)
	return nil
}

func metricLabels(metric *dto.Metric) map[string]string {
	labels := make(map[string]string, len(metric.Label))
	for _, label := range metric.Label {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}
