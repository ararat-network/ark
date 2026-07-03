package metrics

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("noah/oracle/sidecar/metrics")

	ticks                metric.Int64Counter
	providerPrices       metric.Float64Gauge
	aggregatePrices      metric.Float64Gauge
	routePrices          metric.Float64Gauge
	pairSampleCounts     metric.Int64Gauge
	resolvedSourceCounts metric.Int64Gauge
	missingPrices        metric.Int64Counter
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

	routePrices, err = meter.Float64Gauge(
		"noah.oracle.route.price",
		metric.WithDescription("Resolved standalone oracle route price before final aggregation"),
	)
	if err != nil {
		panic(err)
	}

	pairSampleCounts, err = meter.Int64Gauge(
		"noah.oracle.pair.sample.count",
		metric.WithDescription("Number of provider price samples used to aggregate an oracle pair"),
	)
	if err != nil {
		panic(err)
	}

	resolvedSourceCounts, err = meter.Int64Gauge(
		"noah.oracle.resolved.source.count",
		metric.WithDescription("Number of resolved route sources used for a standalone oracle pair"),
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

func RecordProviderPrice(ctx context.Context, provider string, pair string, price float64) {
	providerPrices.Record(
		ctx,
		price,
		metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("pair", normaliseLabelValue(pair)),
		),
	)
}

func RecordAggregatePrice(ctx context.Context, pair string, price float64) {
	aggregatePrices.Record(
		ctx,
		price,
		metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))),
	)
}

func RecordRoutePrice(ctx context.Context, pair string, route string, price float64) {
	routePrices.Record(
		ctx,
		price,
		metric.WithAttributes(routeAttributes(pair, route)...),
	)
}

func RecordPairSampleCount(ctx context.Context, pair string, count int) {
	pairSampleCounts.Record(
		ctx,
		int64(count),
		metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))),
	)
}

func RecordResolvedSourceCount(ctx context.Context, pair string, count int) {
	resolvedSourceCounts.Record(
		ctx,
		int64(count),
		metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))),
	)
}

func RecordMissingPrice(ctx context.Context, denom string) {
	missingPrices.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("denom", normaliseLabelValue(denom))),
	)
}

func RecordMissingPrices(ctx context.Context, denoms []string) {
	for _, denom := range denoms {
		RecordMissingPrice(ctx, denom)
	}
}

func normaliseLabelValue(value string) string {
	return strings.ToLower(value)
}

func routeAttributes(pair string, route string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("pair", normaliseLabelValue(pair)),
		attribute.String("route", normaliseLabelValue(route)),
	}
}
