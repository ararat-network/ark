package metrics

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("noah/oracle/sidecar/chainstate/metrics")

	refreshes metric.Int64Counter
)

func init() {
	var err error

	refreshes, err = meter.Int64Counter(
		"noah.oracle.chainstate.refreshes",
		metric.WithDescription("Number of chain state vote-target refresh attempts"),
	)
	if err != nil {
		panic(err)
	}
}

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
