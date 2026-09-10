package cmd

import (
	"context"
	"net/http"
	"net/http/pprof"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/telemetry"
)

// prometheusEndpoint is the sidecar's scrape endpoint: the default registry
// and the provider exporting into it.
type prometheusEndpoint struct {
	address  string
	provider *sdkmetric.MeterProvider
}

// newPrometheusEndpoint returns the configured endpoint or nil when disabled.
// The sidecar owns the default registry, so its Go and process collectors
// ride along.
func newPrometheusEndpoint(enabled bool, address string) (*prometheusEndpoint, error) {
	if !enabled {
		return nil, nil
	}

	provider, err := telemetry.NewPrometheusProvider(serviceName, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	return &prometheusEndpoint{address: address, provider: provider}, nil
}

// install sets the global provider, so subsystem metrics created through the
// OpenTelemetry API are exported by this process.
func (e *prometheusEndpoint) install() {
	if e == nil {
		return
	}
	otel.SetMeterProvider(e.provider)
}

// serve runs the scrape endpoint under g.
func (e *prometheusEndpoint) serve(ctx context.Context, g *errgroup.Group, logger log.Logger) {
	if e == nil {
		return
	}
	telemetry.ServeScrape(ctx, g, e.address, prometheus.DefaultGatherer, e.provider, logger)
}

// newPprofHandler uses a private mux so the pprof endpoint cannot expose
// unrelated handlers registered on http.DefaultServeMux.
func newPprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
