package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestPrometheusProviderExportsInstrument(t *testing.T) {
	registry := prometheus.NewRegistry()
	provider, err := newPrometheusProvider("oracle", registry)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	counter, err := provider.Meter("ark/test").Int64Counter("ark.test.requests")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	families, err := registry.Gather()
	require.NoError(t, err)
	require.Contains(t, metricFamilyNames(families), "ark_test_requests_total")
}

func TestPrometheusProviderRejectsEmptyServiceName(t *testing.T) {
	_, err := newPrometheusProvider("", prometheus.NewRegistry())
	require.ErrorContains(t, err, "service name cannot be empty")
}

func TestPprofHandlerRegistersIndexAndNamedProfiles(t *testing.T) {
	handler := newPprofHandler()

	tests := []string{
		"/debug/pprof/",
		"/debug/pprof/goroutine?debug=1",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			resp := httptest.NewRecorder()

			handler.ServeHTTP(resp, req)

			require.Equal(t, http.StatusOK, resp.Code)
		})
	}
}

func metricFamilyNames(families []*dto.MetricFamily) []string {
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	return names
}
