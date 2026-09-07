package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	gometrics "github.com/hashicorp/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/stretchr/testify/require"
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

// Exercise actual exposition: names that collide after Prometheus translation,
// generated histogram suffixes, and runtime collector names must coexist.
func TestGoMetricsSinkScrapeSafety(t *testing.T) {
	for _, prefix := range []string{"", "node.service"} {
		t.Run("service="+prefix, func(t *testing.T) {
			ctx := context.Background()
			registry := prometheus.NewRegistry()
			registry.MustRegister(collectors.NewGoCollector())
			provider, err := telemetry.NewPrometheusProvider("test", registry)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(ctx)) })
			sink := telemetry.NewGoMetricsSink(ctx, provider.Meter("gometrics"), telemetry.GoMetricsConfig{
				ServiceName:  prefix,
				IsQueryRoute: func(path string) bool { return path == "/cosmos.auth.v1beta1.Query/Account" },
			})
			key := func(parts ...string) []string {
				if prefix != "" {
					return append([]string{prefix}, parts...)
				}
				return parts
			}
			paths := []string{"/cosmos.auth.v1beta1.Query/Account", "/store/bank/key", "/app/version", "/p2p/filter/addr/host", "/go_goroutines", "", "count", "no-leading-slash"}
			for _, path := range paths {
				sink.IncrCounter(key("query", "count"), 1)
				sink.IncrCounter(key("query", path), 1)
				sink.AddSample(key(path), 2)
			}
			before, err := registry.Gather()
			require.NoError(t, err)
			for i := 0; i < 10000; i++ {
				path := "/unregistered/" + strconv.Itoa(i)
				sink.IncrCounter(key("query", path), 1)
				sink.AddSample(key(path), 3)
			}
			after, err := registry.Gather()
			require.NoError(t, err)
			require.Len(t, after, len(before), "unknown paths cannot create new families")
			for _, family := range after {
				if family.GetName() == "ark_sdk_query_duration_milliseconds" {
					require.Len(t, family.Metric, 5)
					var total uint64
					for _, m := range family.Metric {
						total += m.GetHistogram().GetSampleCount()
					}
					require.Equal(t, uint64(10000+len(paths)), total)
				}
			}
			for _, name := range []string{"go_goroutines", "collision.name", "collision/name", "collision_name", "collision_name_count"} {
				sink.SetGauge([]string{name}, 1)
				sink.IncrCounter([]string{name}, 1)
				sink.EmitKey([]string{name}, 1)
			}
			response := httptest.NewRecorder()
			telemetry.PrometheusHandler(registry).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		})
	}
}

func TestGoMetricsSinkValuesAndLabels(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(ctx)) })
	sink := telemetry.NewGoMetricsSink(ctx, provider.Meter("test"))
	labels := []gometrics.Label{{Name: "module", Value: "bank"}}
	sink.IncrCounterWithLabels([]string{"tx", "count"}, 2, labels)
	sink.IncrCounterWithLabels([]string{"tx", "count"}, 3, labels)
	sink.SetGaugeWithLabels([]string{"height"}, 1, labels)
	sink.SetPrecisionGaugeWithLabels([]string{"height"}, 4.5, labels)
	sink.AddSampleWithLabels([]string{"begin_blocker"}, 7, labels)
	sink.AddSampleWithLabels([]string{"begin_blocker"}, 9, labels)
	sink.EmitKey([]string{"emitted"}, 3)
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	found := map[string]bool{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch {
			case strings.HasPrefix(m.Name, "sdk_legacy_counter_tx_count_"):
				data := m.Data.(metricdata.Sum[float64])
				require.Len(t, data.DataPoints, 1)
				require.Equal(t, 5.0, data.DataPoints[0].Value)
				require.Equal(t, "bank", data.DataPoints[0].Attributes.ToSlice()[0].Value.AsString())
				found["counter"] = true
			case strings.HasPrefix(m.Name, "sdk_legacy_gauge_height_"):
				data := m.Data.(metricdata.Gauge[float64])
				require.Len(t, data.DataPoints, 1)
				require.Equal(t, 4.5, data.DataPoints[0].Value)
				found["gauge"] = true
			case strings.HasPrefix(m.Name, "sdk_legacy_histogram_begin_blocker_"):
				data := m.Data.(metricdata.Histogram[float64])
				require.Len(t, data.DataPoints, 1)
				require.Equal(t, uint64(2), data.DataPoints[0].Count)
				require.Equal(t, 16.0, data.DataPoints[0].Sum)
				found["timer"] = true
			case strings.HasPrefix(m.Name, "sdk_legacy_histogram_emitted_"):
				found["emitted"] = true
			default:
				t.Fatalf("unexpected instrument %s", m.Name)
			}
		}
	}
	require.Len(t, found, 4)
}

func TestGoMetricsSinkBoundAndConcurrency(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(ctx)) })
	sink := telemetry.NewGoMetricsSink(ctx, provider.Meter("test"))
	sink.IncrCounter([]string{"existing"}, 1)
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 256; i++ {
				sink.IncrCounter([]string{"existing"}, 1)
				sink.SetGauge([]string{"dynamic", strconv.Itoa(worker*256 + i)}, 1)
			}
		}()
	}
	wg.Wait()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	count := 0
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			count++
			if strings.HasPrefix(m.Name, "sdk_legacy_counter_existing_") {
				require.Equal(t, float64(4097), m.Data.(metricdata.Sum[float64]).DataPoints[0].Value)
			}
		}
	}
	require.Equal(t, 1024, count)
}

func TestGoMetricsSinkSurvivesAnUnusableMeter(t *testing.T) {
	sink := telemetry.NewGoMetricsSink(context.Background(), metricnoop.Meter{})
	require.NotPanics(t, func() {
		sink.SetGauge([]string{"height"}, 1)
		sink.SetPrecisionGauge([]string{"height"}, 2)
		sink.IncrCounter([]string{"tx", "count"}, 1)
		sink.AddSample([]string{"/unknown"}, 3)
		sink.EmitKey([]string{"event"}, 4)
	})
}
