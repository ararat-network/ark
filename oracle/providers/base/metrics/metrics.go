package metrics

import (
	"context"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"noah/oracle/providers/types"
)

var (
	meter = otel.Meter("noah/oracle/providers/base/metrics")

	responses   metric.Int64Counter
	lastSuccess metric.Int64Gauge
)

func init() {
	var err error
	responses, err = meter.Int64Counter(
		"noah.oracle.provider.responses",
		metric.WithDescription("Number of resolved and unresolved provider responses"),
	)
	if err != nil {
		panic(err)
	}

	lastSuccess, err = meter.Int64Gauge(
		"noah.oracle.provider.last_success",
		metric.WithDescription("Unix timestamp of the last resolved provider response"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}
}

// RecordResponse records the outcome of a provider response for a ticker.
func RecordResponse(
	ctx context.Context,
	provider string,
	ticker types.Ticker,
	fetcherType string,
	errorCode types.ErrorCode,
) {
	normalisedTicker := ticker.Key()
	attrs := metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("ticker", normalisedTicker),
		attribute.String("fetcher", fetcherType),
		attribute.String("status", status(errorCode)),
		attribute.String("error_code", strconv.Itoa(int(errorCode))),
	)
	responses.Add(ctx, 1, attrs)

	if errorCode == types.OK {
		lastSuccess.Record(
			ctx,
			time.Now().UTC().Unix(),
			metric.WithAttributes(
				attribute.String("provider", provider),
				attribute.String("ticker", normalisedTicker),
				attribute.String("fetcher", fetcherType),
			),
		)
	}
}

func status(errorCode types.ErrorCode) string {
	if errorCode == types.OK {
		return "success"
	}
	return "failure"
}
