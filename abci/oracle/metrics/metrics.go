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

	prices      metric.Float64Gauge
	voteReports metric.Int64Counter
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

	voteReports, err = meter.Int64Counter(
		"ark.oracle.vote_reports",
		metric.WithDescription("Validator oracle reports processed in preblock by status"),
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

// CountVoteReports records how many validator oracle reports in one block were
// valid, carried no payload at all, or carried a payload that failed decoding
// or validation.
func CountVoteReports(valid, empty, invalid int64) {
	addVoteReports("valid", valid)
	addVoteReports("empty", empty)
	addVoteReports("invalid", invalid)
}

func addVoteReports(status string, count int64) {
	if count == 0 {
		return
	}
	voteReports.Add(
		context.Background(),
		count,
		metric.WithAttributes(attribute.String("status", status)),
	)
}
