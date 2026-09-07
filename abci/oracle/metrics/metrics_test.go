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

	"github.com/ararat-network/ark/abci/oracle/metrics"
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

	metrics.CountVoteReports(3, 1, 0)
	metrics.CountVoteReports(0, 0, 2)
	metrics.RecordBlockParticipation(2, 4, true)
	metrics.RecordBlockParticipation(0, 0, false)
	metrics.RecordVoteCoverage(2, 4, []string{"ajpy"})

	families, err := registry.Gather()
	require.NoError(t, err)

	t.Run("vote reports count per status and skip zero counts", func(t *testing.T) {
		family := metricFamily(t, families, "ark_oracle_vote_reports_total")
		require.Equal(t, dto.MetricType_COUNTER, family.GetType())

		counts := make(map[string]float64, len(family.Metric))
		for _, metric := range family.Metric {
			counts[metricLabels(metric)["status"]] = metric.GetCounter().GetValue()
		}
		require.Equal(t, map[string]float64{"valid": 3, "empty": 1, "invalid": 2}, counts)
	})

	t.Run("blocks count by functioning verdict and the share is the last block's", func(t *testing.T) {
		blocks := metricFamily(t, families, "ark_oracle_blocks_total")
		counts := make(map[string]float64, len(blocks.Metric))
		for _, metric := range blocks.Metric {
			counts[metricLabels(metric)["functioning"]] = metric.GetCounter().GetValue()
		}
		require.Equal(t, map[string]float64{"true": 1, "false": 1}, counts)

		// The powerless block recorded no share, so the gauge still reads the
		// functioning block's.
		share := metricFamily(t, families, "ark_oracle_participating_power_share")
		require.Len(t, share.Metric, 1)
		require.Equal(t, 0.5, share.Metric[0].GetGauge().GetValue())
	})

	t.Run("vote coverage splits targets by status and counts drops by denom", func(t *testing.T) {
		coverage := metricFamily(t, families, "ark_oracle_vote_targets")
		byStatus := make(map[string]float64, len(coverage.Metric))
		for _, metric := range coverage.Metric {
			byStatus[metricLabels(metric)["status"]] = metric.GetGauge().GetValue()
		}
		require.Equal(t, map[string]float64{"priced": 2, "omitted": 1, "dropped": 1}, byStatus)

		dropped := metricFamily(t, families, "ark_oracle_vote_dropped_targets_total")
		require.Len(t, dropped.Metric, 1)
		require.Equal(t, "ajpy", metricLabels(dropped.Metric[0])["denom"])
		require.Equal(t, float64(1), dropped.Metric[0].GetCounter().GetValue())
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
