package metrics

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/abci/oracle/metrics")

	prices metric.Float64Gauge
)

func init() {
	var err error
	prices, err = meter.Float64Gauge(
		"ark.oracle.price",
		metric.WithDescription("Oracle price written to state"),
	)
	if err != nil {
		panic(err)
	}
}

func ObservePriceForTicker(ticker string, price float64) {
	prices.Record(
		context.Background(),
		price,
		metric.WithAttributes(attribute.String("ticker", strings.ToLower(ticker))),
	)
}
