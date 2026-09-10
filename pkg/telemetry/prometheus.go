// Package telemetry wires process-owned metrics export for the ark binaries.
// Each process builds its own provider and serves its own scrape endpoint.
// Installing the provider globally stays with the caller: arkd has to do it
// after the SDK's own telemetry init, the sidecar at process start.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"
)

// NewPrometheusProvider exports through the explicit registry with service name, version, and
// supplied resource attributes. Scope labels are omitted because Ark instrument names are unique
// and meters are unversioned.
func NewPrometheusProvider(
	serviceName string,
	registerer prometheus.Registerer,
	attrs ...attribute.KeyValue,
) (*sdkmetric.MeterProvider, error) {
	if strings.TrimSpace(serviceName) == "" {
		return nil, errors.New("service name cannot be empty")
	}
	if registerer == nil {
		return nil, errors.New("registerer cannot be nil")
	}

	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(registerer),
		otelprometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating Prometheus exporter: %w", err)
	}

	attrs = append([]attribute.KeyValue{
		attribute.String("service.name", serviceName),
		attribute.String("service.version", BuildVersion()),
	}, attrs...)
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(resource.NewSchemaless(attrs...)),
	), nil
}

// PrometheusHandler serves gatherer in the Prometheus exposition format.
func PrometheusHandler(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
}

// ServeScrape serves gatherer at address under g until ctx ends, then shuts
// provider down. A listener that cannot bind fails g, as the SDK's own
// listeners do.
func ServeScrape(
	ctx context.Context,
	g *errgroup.Group,
	address string,
	gatherer prometheus.Gatherer,
	provider *sdkmetric.MeterProvider,
	logger log.Logger,
) {
	g.Go(func() error {
		serveErr := RunHTTPServer(ctx, address, PrometheusHandler(gatherer), logger, "prometheus metrics")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
		defer cancel()
		return errors.Join(serveErr, provider.Shutdown(shutdownCtx))
	})
}
