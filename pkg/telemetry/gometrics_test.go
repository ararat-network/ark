package telemetry_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	gometrics "github.com/hashicorp/go-metrics"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/ararat-network/ark/pkg/telemetry"
)

func TestSanitiseInstrumentName(t *testing.T) {
	cases := []struct {
		name, key, want string
	}{
		{"valid name passes through", "abci.query", "abci.query"},
		{"leading slash dropped", "/cosmos.auth.v1beta1.Query/Account", "cosmos.auth.v1beta1.Query/Account"},
		{"leading digits dropped", "123abc", "abc"},
		{"space becomes underscore", "tx count", "tx_count"},
		{"non-ascii bytes become underscores", "h\u00e9llo", "h__llo"},
		{"no letter at all", "/123", "unnamed"},
		{"empty", "", "unnamed"},
		{"truncated at 255", "a" + strings.Repeat("b", 300), "a" + strings.Repeat("b", 254)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, telemetry.SanitiseInstrumentName(tc.key))
		})
	}
}

// TestGoMetricsSinkRecordsUnderSanitisedNames pins the reason the sink
// exists: a query path the SDK's bridge panics on records under a valid
// name, alongside ordinary keys.
func TestGoMetricsSinkRecordsUnderSanitisedNames(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	sink := telemetry.NewGoMetricsSink(context.Background(), provider.Meter("test"))

	require.NotPanics(t, func() {
		sink.AddSample([]string{"/cosmos.auth.v1beta1.Query/Account"}, 1.5)
		sink.IncrCounter([]string{"tx", "count"}, 1)
		sink.SetGauge([]string{"/"}, 2)
		sink.SetPrecisionGauge([]string{"height"}, 3)
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	for _, want := range []string{"cosmos.auth.v1beta1.Query/Account", "tx.count", "unnamed", "height"} {
		require.True(t, names[want], "missing %s in %v", want, names)
	}
}

// TestGoMetricsSinkRecordsLabelsAsAttributes covers the labelled half of the
// sink. Labels are what separate one series from another once the SDK emits
// the same key for several modules, so a bridge that dropped them would
// collapse those series into one without any error surfacing.
func TestGoMetricsSinkRecordsLabelsAsAttributes(t *testing.T) {
	reader, sink := newTestSink(t)

	labels := []gometrics.Label{{Name: "module", Value: "treasury"}, {Name: "denom", Value: "anoah"}}
	sink.SetGaugeWithLabels([]string{"fund", "balance"}, 7, labels)
	sink.SetPrecisionGaugeWithLabels([]string{"exposure", "multiplier"}, 1.5, labels)
	sink.IncrCounterWithLabels([]string{"tx", "count"}, 2, labels)
	sink.AddSampleWithLabels([]string{"block", "seconds"}, 0.75, labels)
	sink.EmitKey([]string{"emitted", "key"}, 3)

	metrics := collect(t, reader)
	want := map[string]string{
		"module": "treasury",
		"denom":  "anoah",
	}
	for _, name := range []string{"fund.balance", "exposure.multiplier", "tx.count", "block.seconds"} {
		require.Contains(t, metrics, name)
		require.Equal(t, want, attributesOf(t, metrics[name]), "attributes on %s", name)
	}
	// EmitKey takes no labels, so its point carries none.
	require.Contains(t, metrics, "emitted.key")
	require.Empty(t, attributesOf(t, metrics["emitted.key"]))
}

// TestGoMetricsSinkRecordsValues pins each method onto the instrument kind it
// belongs to: a counter that became a gauge would silently stop accumulating.
func TestGoMetricsSinkRecordsValues(t *testing.T) {
	reader, sink := newTestSink(t)

	sink.IncrCounter([]string{"counted"}, 2)
	sink.IncrCounterWithLabels([]string{"counted"}, 3, nil)
	sink.SetGauge([]string{"gauged"}, 1)
	sink.SetPrecisionGauge([]string{"gauged"}, 4.5)
	sink.AddSample([]string{"sampled"}, 0.2)
	sink.EmitKey([]string{"sampled"}, 0.3)

	metrics := collect(t, reader)

	counter, ok := metrics["counted"].Data.(metricdata.Sum[float64])
	require.True(t, ok, "counter should record as a sum")
	require.True(t, counter.IsMonotonic)
	// Both calls land on one instrument and accumulate.
	require.Len(t, counter.DataPoints, 1)
	require.InDelta(t, 5.0, counter.DataPoints[0].Value, 1e-9)

	gauge, ok := metrics["gauged"].Data.(metricdata.Gauge[float64])
	require.True(t, ok, "gauge should record as a gauge")
	require.Len(t, gauge.DataPoints, 1)
	// A gauge keeps the last value written, not the sum.
	require.InDelta(t, 4.5, gauge.DataPoints[0].Value, 1e-9)

	histogram, ok := metrics["sampled"].Data.(metricdata.Histogram[float64])
	require.True(t, ok, "samples and emitted keys should share a histogram")
	require.Len(t, histogram.DataPoints, 1)
	require.Equal(t, uint64(2), histogram.DataPoints[0].Count)
}

// TestGoMetricsSinkReusesInstruments proves the caches are keyed on the
// sanitised name: two keys that sanitise alike must not create rival
// instruments, which OTel would report as a duplicate-registration conflict.
func TestGoMetricsSinkReusesInstruments(t *testing.T) {
	reader, sink := newTestSink(t)

	sink.IncrCounter([]string{"tx count"}, 1)
	sink.IncrCounter([]string{"tx/count"}, 1)
	sink.IncrCounter([]string{"tx_count"}, 1)

	metrics := collect(t, reader)
	require.Contains(t, metrics, "tx_count")
	require.Contains(t, metrics, "tx/count")

	sum := metrics["tx_count"].Data.(metricdata.Sum[float64])
	require.Len(t, sum.DataPoints, 1)
	// "tx count" and "tx_count" both sanitise to tx_count and share one series.
	require.InDelta(t, 2.0, sum.DataPoints[0].Value, 1e-9)
}

// TestGoMetricsSinkSurvivesAnUnusableMeter is the no-panic guarantee the type
// exists for: a meter that refuses every instrument must leave the sink inert
// rather than take the node down from a metrics call.
func TestGoMetricsSinkSurvivesAnUnusableMeter(t *testing.T) {
	sink := telemetry.NewGoMetricsSink(context.Background(), metricnoop.Meter{})

	require.NotPanics(t, func() {
		sink.SetGauge([]string{"gauged"}, 1)
		sink.SetGaugeWithLabels([]string{"gauged"}, 1, nil)
		sink.SetPrecisionGauge([]string{"gauged"}, 1)
		sink.SetPrecisionGaugeWithLabels([]string{"gauged"}, 1, nil)
		sink.IncrCounter([]string{"counted"}, 1)
		sink.IncrCounterWithLabels([]string{"counted"}, 1, nil)
		sink.AddSample([]string{"sampled"}, 1)
		sink.AddSampleWithLabels([]string{"sampled"}, 1, nil)
		sink.EmitKey([]string{"emitted"}, 1)
	})
}

// TestGoMetricsSinkIsConcurrencySafe exercises the sync.Map caches from many
// goroutines, since the SDK emits metrics from every ABCI path at once.
func TestGoMetricsSinkIsConcurrencySafe(t *testing.T) {
	reader, sink := newTestSink(t)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 32 {
				sink.IncrCounterWithLabels([]string{"tx", "count"}, 1,
					[]gometrics.Label{{Name: "worker", Value: strconv.Itoa(i % 4)}})
			}
		}()
	}
	wg.Wait()

	metrics := collect(t, reader)
	sum, ok := metrics["tx.count"].Data.(metricdata.Sum[float64])
	require.True(t, ok)

	var total float64
	for _, point := range sum.DataPoints {
		total += point.Value
	}
	require.InDelta(t, float64(16*32), total, 1e-9)
	// Four distinct worker labels, so four series rather than one per goroutine.
	require.Len(t, sum.DataPoints, 4)
}

func newTestSink(t *testing.T) (*sdkmetric.ManualReader, *telemetry.GoMetricsSink) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	return reader, telemetry.NewGoMetricsSink(context.Background(), provider.Meter("test"))
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	metrics := make(map[string]metricdata.Metrics)
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			metrics[metric.Name] = metric
		}
	}
	return metrics
}

// attributesOf returns the attributes on a metric's single data point.
func attributesOf(t *testing.T, metric metricdata.Metrics) map[string]string {
	t.Helper()

	var set attribute.Set
	switch data := metric.Data.(type) {
	case metricdata.Gauge[float64]:
		require.Len(t, data.DataPoints, 1)
		set = data.DataPoints[0].Attributes
	case metricdata.Sum[float64]:
		require.Len(t, data.DataPoints, 1)
		set = data.DataPoints[0].Attributes
	case metricdata.Histogram[float64]:
		require.Len(t, data.DataPoints, 1)
		set = data.DataPoints[0].Attributes
	default:
		t.Fatalf("unexpected metric data %T for %s", metric.Data, metric.Name)
	}

	got := make(map[string]string, set.Len())
	for _, kv := range set.ToSlice() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	return got
}
