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

	prices                   metric.Float64Gauge
	reportsPerValidator      metric.Float64Gauge
	reportStatusPerValidator metric.Int64Counter
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

	reportsPerValidator, err = meter.Float64Gauge(
		"ark.oracle.validator.price",
		metric.WithDescription("Oracle price reported by a validator"),
	)
	if err != nil {
		panic(err)
	}

	reportStatusPerValidator, err = meter.Int64Counter(
		"ark.oracle.validator.reports",
		metric.WithDescription("Number of validator oracle reports by status"),
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
