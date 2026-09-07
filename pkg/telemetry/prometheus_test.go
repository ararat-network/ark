package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestNewPrometheusProviderExportsInstrument(t *testing.T) {
	registry := prometheus.NewRegistry()
	provider, err := NewPrometheusProvider("oracle", registry, attribute.String("ark.chain.id", "ark-test"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	counter, err := provider.Meter("ark/test").Int64Counter("ark.test.requests")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	families, err := registry.Gather()
	require.NoError(t, err)
	names := metricFamilyNames(families)
	require.Contains(t, names, "ark_test_requests_total")
	require.Contains(t, names, "target_info")

	resp := httptest.NewRecorder()
	PrometheusHandler(registry).ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "ark_test_requests_total")
	require.Contains(t, resp.Body.String(), `service_name="oracle"`)
	require.Contains(t, resp.Body.String(), `service_version="`)
	require.Contains(t, resp.Body.String(), `ark_chain_id="ark-test"`)
}

func TestNewPrometheusProviderRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		registerer  prometheus.Registerer
		errorSubstr string
	}{
		{
			name:        "empty service name",
			serviceName: " ",
			registerer:  prometheus.NewRegistry(),
			errorSubstr: "service name cannot be empty",
		},
		{
			name:        "nil registerer",
			serviceName: "oracle",
			errorSubstr: "registerer cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPrometheusProvider(tt.serviceName, tt.registerer)
			require.ErrorContains(t, err, tt.errorSubstr)
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
