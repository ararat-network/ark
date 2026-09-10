package telemetry

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

// TestServeScrapeServesUntilCancelled runs the loop both binaries' endpoints
// share: the registry is scrapeable until ctx ends, then the listener and
// the provider are gone.
func TestServeScrapeServesUntilCancelled(t *testing.T) {
	registry := prometheus.NewRegistry()
	provider, err := NewPrometheusProvider("oracle", registry)
	require.NoError(t, err)
	counter, err := provider.Meter("ark/test").Int64Counter("ark.test.scrapes")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	address := freeLoopbackAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	var g errgroup.Group
	ServeScrape(ctx, &g, address, registry, provider, nil)

	require.Contains(t, scrapeEventually(t, "http://"+address+"/metrics"), "ark_test_scrapes_total 1")

	cancel()
	require.NoError(t, g.Wait())
	_, err = http.Get("http://" + address + "/metrics") //nolint:noctx // the listener is closed; this must fail
	require.Error(t, err)
}

// freeLoopbackAddress returns a loopback address nothing is listening on.
func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := ln.Addr().String()
	require.NoError(t, ln.Close())
	return address
}

// scrapeEventually returns the body once url answers, within the deadline.
func scrapeEventually(t *testing.T, url string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:noctx // the deadline bounds the loop
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			require.NoError(t, readErr)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			return string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never answered: %v", url, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

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
