package metrics

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("noah/oracle/metrics")

	ticks                 metric.Int64Counter
	providerPrices        metric.Float64Gauge
	aggregatePrices       metric.Float64Gauge
	providerContributions metric.Int64Counter
	providerCounts        metric.Int64Gauge
	missingPrices         metric.Int64Counter
)

func init() {
	var err error

	ticks, err = meter.Int64Counter(
		"noah.oracle.ticks",
		metric.WithDescription("Number of standalone oracle aggregation ticks"),
	)
	if err != nil {
		panic(err)
	}

	providerPrices, err = meter.Float64Gauge(
		"noah.oracle.provider.price",
		metric.WithDescription("Provider price used by standalone oracle aggregation"),
	)
	if err != nil {
		panic(err)
	}

	aggregatePrices, err = meter.Float64Gauge(
		"noah.oracle.aggregate.price",
		metric.WithDescription("Aggregated standalone oracle price"),
	)
	if err != nil {
		panic(err)
	}

	providerContributions, err = meter.Int64Counter(
		"noah.oracle.provider.contributions",
		metric.WithDescription("Number of provider contributions considered by standalone oracle aggregation"),
	)
	if err != nil {
		panic(err)
	}

	providerCounts, err = meter.Int64Gauge(
		"noah.oracle.provider.count",
		metric.WithDescription("Number of providers used for a standalone oracle denom"),
	)
	if err != nil {
		panic(err)
	}

	missingPrices, err = meter.Int64Counter(
		"noah.oracle.missing.prices",
		metric.WithDescription("Number of standalone oracle aggregation ticks missing a denom price"),
	)
	if err != nil {
		panic(err)
	}
}

func RecordOracleTick(ctx context.Context) {
	ticks.Add(ctx, 1)
}

func RecordProviderPrice(ctx context.Context, provider string, denom string, price float64) {
	providerPrices.Record(
		ctx,
		price,
		metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("denom", normaliseDenom(denom)),
		),
	)
}

func RecordAggregatePrice(ctx context.Context, denom string, price float64) {
	aggregatePrices.Record(
		ctx,
		price,
		metric.WithAttributes(attribute.String("denom", normaliseDenom(denom))),
	)
}

func RecordProviderContribution(ctx context.Context, provider string, denom string, success bool) {
	providerContributions.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("denom", normaliseDenom(denom)),
			attribute.String("status", contributionStatus(success)),
		),
	)
}

func RecordProviderCount(ctx context.Context, denom string, count int) {
	providerCounts.Record(
		ctx,
		int64(count),
		metric.WithAttributes(attribute.String("denom", normaliseDenom(denom))),
	)
}

func RecordMissingPrice(ctx context.Context, denom string) {
	missingPrices.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("denom", normaliseDenom(denom))),
	)
}

func RecordMissingPrices(ctx context.Context, denoms []string) {
	for _, denom := range denoms {
		RecordMissingPrice(ctx, denom)
	}
}

func normaliseDenom(denom string) string {
	return strings.ToLower(denom)
}

func contributionStatus(success bool) string {
	if success {
		return "success"
	}
	return "failure"
}
