package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("noah/pkg/metrics")

	moduleMethodLatency metric.Float64Histogram
)

func init() {
	var err error
	moduleMethodLatency, err = meter.Float64Histogram(
		"noah.module.method.duration",
		metric.WithDescription("Duration of Cosmos SDK module lifecycle method execution"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}
}

func RecordModuleMethodLatency(ctx context.Context, module string, method ModuleMethod) func() {
	start := time.Now()

	return func() {
		moduleMethodLatency.Record(
			ctx,
			float64(time.Since(start))/float64(time.Millisecond),
			metric.WithAttributes(
				attribute.String("module", module),
				attribute.String("method", method.String()),
			),
		)
	}
}
