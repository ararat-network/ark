// Package telemetry wires process-owned metrics export for the ark binaries.
// Each process builds its own provider and serves its own scrape endpoint.
// Installing the provider globally stays with the caller: arkd has to do it
// after the SDK's own telemetry init, the sidecar at process start.
package telemetry

import (
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
)

// NewPrometheusProvider returns a MeterProvider exporting through registerer,
// with service.name and attrs as its resource. The registerer is explicit
// because it decides which scrape endpoint the metrics land on: CometBFT and
// the SDK's legacy sink both own the default registry, so arkd passes its own.
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

	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registerer))
	if err != nil {
		return nil, fmt.Errorf("creating Prometheus exporter: %w", err)
	}

	attrs = append([]attribute.KeyValue{attribute.String("service.name", serviceName)}, attrs...)
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(resource.NewSchemaless(attrs...)),
	), nil
}

// PrometheusHandler serves gatherer in the Prometheus exposition format.
func PrometheusHandler(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
}
