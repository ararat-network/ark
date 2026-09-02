package telemetry_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
