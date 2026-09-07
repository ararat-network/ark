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
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	clientmetrics "github.com/ararat-network/ark/pricefeed/client/metrics"
)

func TestRecordSidecarResponse(t *testing.T) {
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

	clientmetrics.SetEndpoints(context.Background(), []string{"sidecar-a:8080", "sidecar-b:8080"})
	initial, err := registry.Gather()
	require.NoError(t, err)
	for _, m := range metricFamily(t, initial, "ark_pricefeed_sidecar_last_success_seconds").Metric {
		require.Zero(t, m.GetGauge().GetValue())
	}

	clientmetrics.RecordSidecarResponse("sidecar-a:8080", 1250*time.Millisecond, nil)
	clientmetrics.RecordSidecarResponse("sidecar-b:8080", 250*time.Millisecond, errors.New("failure"))
	clientmetrics.RecordSnapshotTimestamp("sidecar-a:8080", time.Unix(1_700_000_000, 0))

	families, err := registry.Gather()
	require.NoError(t, err)

	// Both series carry the address, so an operator can tell which sidecar
	// is serving and which is failing.
	responses := metricFamily(t, families, "ark_pricefeed_responses_total")
	require.Len(t, responses.Metric, 4)
	require.Equal(t, float64(1), metricFor(t, responses, "sidecar-a:8080", "success").GetCounter().GetValue())
	require.Equal(t, float64(1), metricFor(t, responses, "sidecar-b:8080", "failure").GetCounter().GetValue())

	duration := metricFamily(t, families, "ark_pricefeed_response_duration_milliseconds")
	require.Len(t, duration.Metric, 2)
	served := metricFor(t, duration, "sidecar-a:8080", "").GetHistogram()
	require.Equal(t, uint64(1), served.GetSampleCount())
	require.InDelta(t, 1250, served.GetSampleSum(), 0.001)
	failed := metricFor(t, duration, "sidecar-b:8080", "").GetHistogram()
	require.Equal(t, uint64(1), failed.GetSampleCount())
	require.InDelta(t, 250, failed.GetSampleSum(), 0.001)

	// Never-successful endpoints remain at zero, so the age of this
	// node's prices is time() minus the newest stamp.
	lastSuccess := metricFamily(t, families, "ark_pricefeed_sidecar_last_success_seconds")
	require.Len(t, lastSuccess.Metric, 2)
	require.Zero(t, metricFor(t, lastSuccess, "sidecar-b:8080", "").GetGauge().GetValue())
	stamp := metricFor(t, lastSuccess, "sidecar-a:8080", "").GetGauge().GetValue()
	require.InDelta(t, float64(time.Now().Unix()), stamp, 5)

	// The snapshot's own stamp is the sidecar's clock, not the fetch time.
	snapshots := metricFamily(t, families, "ark_pricefeed_snapshot_timestamp_seconds")
	require.Len(t, snapshots.Metric, 1)
	require.Equal(t, float64(1_700_000_000), metricFor(t, snapshots, "sidecar-a:8080", "").GetGauge().GetValue())
	t.Run("endpoint replacement preserves retained timestamps", func(t *testing.T) {
		clientmetrics.SetEndpoints(context.Background(), []string{"sidecar-a:8080", "new:8080"})
		families, err := registry.Gather()
		require.NoError(t, err)
		last := metricFamily(t, families, "ark_pricefeed_sidecar_last_success_seconds")
		require.Len(t, last.Metric, 2)
		require.Equal(t, stamp, metricFor(t, last, "sidecar-a:8080", "").GetGauge().GetValue())
		require.Zero(t, metricFor(t, last, "new:8080", "").GetGauge().GetValue())
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

// metricFor finds the series labelled with address and, when status is not
// empty, with status.
func metricFor(t *testing.T, family *dto.MetricFamily, address, status string) *dto.Metric {
	t.Helper()

	for _, metric := range family.Metric {
		if hasLabel(metric, "address", address) && (status == "" || hasLabel(metric, "status", status)) {
			return metric
		}
	}

	t.Fatalf("metric with address %q and status %q not found", address, status)
	return nil
}

func hasLabel(metric *dto.Metric, name, value string) bool {
	for _, label := range metric.Label {
		if label.GetName() == name && label.GetValue() == value {
			return true
		}
	}
	return false
}
