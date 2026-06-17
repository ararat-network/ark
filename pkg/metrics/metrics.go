package metrics

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("noah/service/metrics")

	oracleResponseLatency    metric.Float64Histogram
	oracleResponseCounter    metric.Int64Counter
	abciMethodLatency        metric.Float64Histogram
	abciRequests             metric.Int64Counter
	messageSize              metric.Int64Histogram
	prices                   metric.Float64Gauge
	reportsPerValidator      metric.Float64Gauge
	reportStatusPerValidator metric.Int64Counter
)

func init() {
	var err error
	oracleResponseLatency, err = meter.Float64Histogram(
		"noah.oracle.response.duration",
		metric.WithDescription("Duration of oracle service responses"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}

	oracleResponseCounter, err = meter.Int64Counter(
		"noah.oracle.responses",
		metric.WithDescription("Number of oracle service responses"),
	)
	if err != nil {
		panic(err)
	}

	abciMethodLatency, err = meter.Float64Histogram(
		"noah.abci.method.duration",
		metric.WithDescription("Duration of ABCI method execution"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}

	abciRequests, err = meter.Int64Counter(
		"noah.abci.requests",
		metric.WithDescription("Number of ABCI requests"),
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

	prices, err = meter.Float64Gauge(
		"noah.oracle.price",
		metric.WithDescription("Oracle price written to state"),
	)
	if err != nil {
		panic(err)
	}

	reportsPerValidator, err = meter.Float64Gauge(
		"noah.oracle.validator.price",
		metric.WithDescription("Oracle price reported by a validator"),
	)
	if err != nil {
		panic(err)
	}

	reportStatusPerValidator, err = meter.Int64Counter(
		"noah.oracle.validator.reports",
		metric.WithDescription("Number of validator oracle reports by status"),
	)
	if err != nil {
		panic(err)
	}
}

func ObserveOracleResponseLatency(duration time.Duration) {
	oracleResponseLatency.Record(context.Background(), durationMillis(duration))
}

func AddOracleResponse(status Labeller) {
	oracleResponseCounter.Add(
		context.Background(),
		1,
		metric.WithAttributes(attribute.String("status", label(status))),
	)
}

func ObserveABCIMethodLatency(method ABCIMethod, duration time.Duration) {
	observeABCIMethodLatency(context.Background(), "", method, duration)
}

func AddABCIRequest(method ABCIMethod, status Labeller) {
	abciRequests.Add(
		context.Background(),
		1,
		metric.WithAttributes(
			attribute.String("method", method.String()),
			attribute.String("status", label(status)),
		),
	)
}

func ObserveMessageSize(msg MessageType, size int) {
	messageSize.Record(
		context.Background(),
		int64(size),
		metric.WithAttributes(attribute.String("message_type", msg.String())),
	)
}

func ObservePriceForTicker(ticker string, price float64) {
	prices.Record(
		context.Background(),
		price,
		metric.WithAttributes(attribute.String("ticker", strings.ToLower(ticker))),
	)
}

func AddValidatorPriceForTicker(validator string, ticker string, price float64) {
	reportsPerValidator.Record(
		context.Background(),
		price,
		metric.WithAttributes(
			attribute.String("validator", validator),
			attribute.String("ticker", strings.ToLower(ticker)),
		),
	)
}

func AddValidatorReportForTicker(validator string, ticker string, status ReportStatus) {
	reportStatusPerValidator.Add(
		context.Background(),
		1,
		metric.WithAttributes(
			attribute.String("validator", validator),
			attribute.String("ticker", strings.ToLower(ticker)),
			attribute.String("status", status.String()),
		),
	)
}

func observeABCIMethodLatency(ctx context.Context, module string, method ABCIMethod, duration time.Duration) {
	attrs := []attribute.KeyValue{attribute.String("method", method.String())}
	if module != "" {
		attrs = append(attrs, attribute.String("module", module))
	}

	abciMethodLatency.Record(
		ctx,
		durationMillis(duration),
		metric.WithAttributes(attrs...),
	)
}

// RecordABCIMethodLatency records the duration of an ABCI method when the returned function is called.
func RecordABCIMethodLatency(ctx context.Context, module string, method ABCIMethod) func() {
	start := time.Now()

	return func() {
		observeABCIMethodLatency(ctx, module, method, time.Since(start))
	}
}

// RecordLatencyAndStatus records an ABCI method's latency and final status.
func RecordLatencyAndStatus(latency time.Duration, err error, method ABCIMethod) {
	ObserveABCIMethodLatency(method, latency)
	AddABCIRequest(method, StatusFromError(err))
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
