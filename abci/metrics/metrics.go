package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("noah/abci/metrics")

	methodLatency metric.Float64Histogram
	requests      metric.Int64Counter
	messageSize   metric.Int64Histogram
)

func init() {
	var err error
	methodLatency, err = meter.Float64Histogram(
		"noah.abci.method.duration",
		metric.WithDescription("Duration of ABCI++ method execution"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}

	requests, err = meter.Int64Counter(
		"noah.abci.requests",
		metric.WithDescription("Number of ABCI++ requests"),
	)
	if err != nil {
		panic(err)
	}

	messageSize, err = meter.Int64Histogram(
		"noah.abci.message.size",
		metric.WithDescription("Size of oracle ABCI messages"),
		metric.WithUnit("By"),
	)
	if err != nil {
		panic(err)
	}
}

func ObserveMethodLatency(method Method, duration time.Duration) {
	methodLatency.Record(
		context.Background(),
		durationMillis(duration),
		metric.WithAttributes(attribute.String("method", method.String())),
	)
}

func AddRequest(method Method, status Labeller) {
	requests.Add(
		context.Background(),
		1,
		metric.WithAttributes(
			attribute.String("method", method.String()),
			attribute.String("status", label(status)),
		),
	)
}

func RecordLatencyAndStatus(latency time.Duration, err error, method Method) {
	ObserveMethodLatency(method, latency)
	AddRequest(method, StatusFromError(err))
}

func ObserveMessageSize(msg MessageType, size int) {
	messageSize.Record(
		context.Background(),
		int64(size),
		metric.WithAttributes(attribute.String("message_type", msg.String())),
	)
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func label(labeller Labeller) string {
	if labeller == nil {
		return Failure{}.Label()
	}
	return labeller.Label()
}
