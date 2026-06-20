package telemetry

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestPrometheusProviderExportsPrecreatedInstrument(t *testing.T) {
	originalProvider := otel.GetMeterProvider()
	t.Cleanup(func() {
		otel.SetMeterProvider(originalProvider)
	})

	counter, err := otel.Meter("noah/test").Int64Counter("noah.test.requests")
	require.NoError(t, err)

	registry := prometheus.NewRegistry()
	provider, err := newPrometheusProvider("noahd", registry)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	otel.SetMeterProvider(provider)
	counter.Add(context.Background(), 1)

	families, err := registry.Gather()
	require.NoError(t, err)
	require.Contains(t, metricFamilyNames(families), "noah_test_requests_total")
}

func TestPrometheusProviderRejectsEmptyServiceName(t *testing.T) {
	_, err := newPrometheusProvider("", prometheus.NewRegistry())
	require.ErrorContains(t, err, "service name cannot be empty")
}

func metricFamilyNames(families []*dto.MetricFamily) []string {
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	return names
}
