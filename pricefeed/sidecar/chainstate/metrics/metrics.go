package metrics

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/pricefeed/sidecar/chainstate/metrics")

	refreshes metric.Int64Counter
)

func init() {
	var err error

	refreshes, err = meter.Int64Counter(
		"ark.pricefeed.chainstate.refreshes",
		metric.WithDescription("Number of chain state feed refresh attempts"),
	)
	if err != nil {
		panic(err)
	}
}

// RecordRefresh records one chainstate feed refresh attempt.
func RecordRefresh(ctx context.Context, status string) {
	refreshes.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("status", normaliseLabelValue(status))),
	)
}

func normaliseLabelValue(value string) string {
	return strings.ToLower(value)
}
