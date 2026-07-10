package metrics_test

import (
	. "ark/oracle/sidecar/providers/base/websocket/metrics"
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestRecordWebSocketMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	RecordConnectionEvent(context.Background(), "kraken", ConnectionEventHealthy)
	RecordReconnect(context.Background(), "kraken")
	RecordParseError(context.Background(), "kraken")
	RecordWriteError(context.Background(), "kraken", WriteOperationHeartbeat)

	families, err := registry.Gather()
	require.NoError(t, err)

	connectionEvents := metricFamily(t, families, "ark_oracle_provider_websocket_connection_events_total")
	require.Equal(t, float64(1), counterValue(t, connectionEvents, map[string]string{
		"provider": "kraken",
		"event":    ConnectionEventHealthy,
	}))

	reconnects := metricFamily(t, families, "ark_oracle_provider_websocket_reconnects_total")
	require.Equal(t, float64(1), counterValue(t, reconnects, map[string]string{
		"provider": "kraken",
	}))

	parseErrors := metricFamily(t, families, "ark_oracle_provider_websocket_parse_errors_total")
	require.Equal(t, float64(1), counterValue(t, parseErrors, map[string]string{
		"provider": "kraken",
	}))

	writeErrors := metricFamily(t, families, "ark_oracle_provider_websocket_write_errors_total")
	require.Equal(t, float64(1), counterValue(t, writeErrors, map[string]string{
		"provider":  "kraken",
		"operation": WriteOperationHeartbeat,
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
