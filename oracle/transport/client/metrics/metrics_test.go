package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"

	clientmetrics "noah/oracle/transport/client/metrics"
)

func TestRecordOracleResponse(t *testing.T) {
	originalProvider := otel.GetMeterProvider()
	t.Cleanup(func() {
		otel.SetMeterProvider(originalProvider)
	})

	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	clientmetrics.RecordOracleResponse(1250*time.Millisecond, nil)
	clientmetrics.RecordOracleResponse(250*time.Millisecond, errors.New("failure"))

	families, err := registry.Gather()
	require.NoError(t, err)

	responses := metricFamily(t, families, "noah_oracle_responses_total")
	require.Len(t, responses.Metric, 2)
	require.Equal(t, float64(1), counterValue(t, responses, "Success"))
	require.Equal(t, float64(1), counterValue(t, responses, "Failure"))

	duration := metricFamily(t, families, "noah_oracle_response_duration_milliseconds")
	require.Len(t, duration.Metric, 1)
	require.Equal(t, uint64(2), duration.Metric[0].GetHistogram().GetSampleCount())
	require.InDelta(t, 1500, duration.Metric[0].GetHistogram().GetSampleSum(), 0.001)
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

func counterValue(t *testing.T, family *dto.MetricFamily, status string) float64 {
	t.Helper()

	for _, metric := range family.Metric {
		for _, label := range metric.Label {
			if label.GetName() == "status" && label.GetValue() == status {
				return metric.GetCounter().GetValue()
			}
		}
	}

	t.Fatalf("metric with status %q not found", status)
	return 0
}
