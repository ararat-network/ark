package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/abci/metrics")

	methodLatency metric.Float64Histogram
	requests      metric.Int64Counter
)

func init() {
	var err error
	methodLatency, err = meter.Float64Histogram(
		"ark.abci.method.duration",
		metric.WithDescription("Duration of ABCI++ method execution"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}

	requests, err = meter.Int64Counter(
		"ark.abci.requests",
		metric.WithDescription("Number of ABCI++ requests"),
	)
	if err != nil {
		panic(err)
	}
}

func observeMethodLatency(method Method, duration time.Duration) {
	methodLatency.Record(
		context.Background(),
		durationMillis(duration),
		metric.WithAttributes(attribute.String("method", method.String())),
	)
}

func addRequest(method Method, status Status) {
	requests.Add(
		context.Background(),
		1,
		metric.WithAttributes(
			attribute.String("method", method.String()),
			attribute.String("status", status.String()),
		),
	)
}

func RecordLatencyAndStatus(latency time.Duration, status Status, method Method) {
	observeMethodLatency(method, latency)
	addRequest(method, status)
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
