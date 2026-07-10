package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type prometheusTelemetry struct {
	provider *sdkmetric.MeterProvider
	handler  http.Handler
}

// initPrometheus installs the provider globally so subsystem metrics created
// through the OpenTelemetry API are exported by this process.
func initPrometheus(serviceName string) (*prometheusTelemetry, error) {
	provider, err := newPrometheusProvider(serviceName, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	otel.SetMeterProvider(provider)
	return &prometheusTelemetry{
		provider: provider,
		handler:  promhttp.Handler(),
	}, nil
}

func newPrometheusProvider(
	serviceName string,
	registerer prometheus.Registerer,
) (*sdkmetric.MeterProvider, error) {
	if strings.TrimSpace(serviceName) == "" {
		return nil, fmt.Errorf("service name cannot be empty")
	}

	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(registerer),
	)
	if err != nil {
		return nil, fmt.Errorf("creating Prometheus exporter: %w", err)
	}

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(resource.NewSchemaless(
			attribute.String("service.name", serviceName),
		)),
	), nil
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
