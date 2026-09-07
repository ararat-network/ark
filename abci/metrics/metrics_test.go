package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/ararat-network/ark/abci/metrics"
)

// The global binds this package's instruments to the first provider
// installed in the process, so one test installs one provider and every
// exposition check lives under it.
func TestExposition(t *testing.T) {
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

	metrics.RecordLatencyAndStatus(1500*time.Millisecond, metrics.StatusCodec, metrics.PrepareProposal)
	metrics.RecordLatencyAndStatus(500*time.Millisecond, metrics.StatusSuccess, metrics.PrepareProposal)
	metrics.RecordLatencyAndStatus(250*time.Millisecond, metrics.Status(""), metrics.ProcessProposal)

	families, err := registry.Gather()
	require.NoError(t, err)

	t.Run("method duration is a millisecond histogram per method", func(t *testing.T) {
		family := metricFamily(t, families, "ark_abci_method_duration_milliseconds")
		require.Equal(t, dto.MetricType_HISTOGRAM, family.GetType())

		byMethod := seriesByLabel(family, "method")
		require.Len(t, byMethod, 2)

		prepare := byMethod["prepare_proposal"].GetHistogram()
		require.Equal(t, uint64(2), prepare.GetSampleCount())
		require.Equal(t, float64(2000), prepare.GetSampleSum())

		process := byMethod["process_proposal"].GetHistogram()
		require.Equal(t, uint64(1), process.GetSampleCount())
		require.Equal(t, float64(250), process.GetSampleSum())
	})

	t.Run("requests count per method and status", func(t *testing.T) {
		family := metricFamily(t, families, "ark_abci_requests_total")
		require.Equal(t, dto.MetricType_COUNTER, family.GetType())

		counts := make(map[string]float64, len(family.Metric))
		for _, metric := range family.Metric {
			labels := metricLabels(metric)
			counts[labels["method"]+"/"+labels["status"]] = metric.GetCounter().GetValue()
		}
		require.Equal(t, map[string]float64{
			"prepare_proposal/CodecError": 1,
			"prepare_proposal/Success":    1,
			// An unset status records as the generic failure.
			"process_proposal/Failure": 1,
		}, counts)
	})
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

// seriesByLabel indexes a family's series by the value of one label.
func seriesByLabel(family *dto.MetricFamily, label string) map[string]*dto.Metric {
	series := make(map[string]*dto.Metric, len(family.Metric))
	for _, metric := range family.Metric {
		series[metricLabels(metric)[label]] = metric
	}
	return series
}
