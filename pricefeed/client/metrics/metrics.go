package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/pricefeed/client/metrics")

	oracleResponseLatency metric.Float64Histogram
	oracleResponses       metric.Int64Counter
)

func init() {
	var err error
	oracleResponseLatency, err = meter.Float64Histogram(
		"ark.pricefeed.response.duration",
		metric.WithDescription("Duration of oracle service responses"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}

	oracleResponses, err = meter.Int64Counter(
		"ark.pricefeed.responses",
		metric.WithDescription("Number of oracle service responses"),
	)
	if err != nil {
		panic(err)
	}
}

// RecordOracleResponse records the latency and outcome of one sidecar request.
func RecordOracleResponse(duration time.Duration, err error) {
	oracleResponseLatency.Record(
		context.Background(),
		float64(duration)/float64(time.Millisecond),
	)
	oracleResponses.Add(
		context.Background(),
		1,
		metric.WithAttributes(attribute.String("status", responseStatus(err))),
	)
}

func responseStatus(err error) string {
	if err == nil {
		return "success"
	}
	return "failure"
}
