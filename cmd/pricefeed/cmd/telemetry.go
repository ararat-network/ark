package cmd

import (
	"context"
	"net/http"
	"net/http/pprof"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/ararat-network/ark/pkg/telemetry"
)

type prometheusTelemetry struct {
	provider *sdkmetric.MeterProvider
	handler  http.Handler
}

// initPrometheus installs the provider globally so subsystem metrics created
// through the OpenTelemetry API are exported by this process. The sidecar
// owns the default registry, so its Go and process collectors ride along.
func initPrometheus(serviceName string) (*prometheusTelemetry, error) {
	provider, err := telemetry.NewPrometheusProvider(serviceName, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	otel.SetMeterProvider(provider)
	return &prometheusTelemetry{
		provider: provider,
		handler:  telemetry.PrometheusHandler(prometheus.DefaultGatherer),
	}, nil
}

func (t *prometheusTelemetry) Handler() http.Handler {
	if t == nil {
		return nil
	}
	return t.handler
}

func (t *prometheusTelemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
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
