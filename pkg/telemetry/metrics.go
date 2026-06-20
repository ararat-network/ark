package telemetry

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func InitPrometheus(serviceName string) (*sdkmetric.MeterProvider, error) {
	provider, err := newPrometheusProvider(serviceName, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	otel.SetMeterProvider(provider)
	return provider, nil
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
