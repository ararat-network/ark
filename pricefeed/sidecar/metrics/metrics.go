package metrics

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/pricefeed/sidecar/metrics")

	ticks                metric.Int64Counter
	providerPrices       metric.Float64Gauge
	aggregatePrices      metric.Float64Gauge
	routePrices          metric.Float64Gauge
	pairSampleCounts     metric.Int64Gauge
	resolvedSourceCounts metric.Int64Gauge
	missingPrices        metric.Int64Counter
	bootstrapPriceUses   metric.Int64Counter
)

func init() {
	var err error

	ticks, err = meter.Int64Counter(
		"ark.pricefeed.ticks",
		metric.WithDescription("Number of standalone oracle aggregation ticks"),
	)
	if err != nil {
		panic(err)
	}

	providerPrices, err = meter.Float64Gauge(
		"ark.pricefeed.provider.price",
		metric.WithDescription("Provider price used by standalone oracle aggregation"),
	)
	if err != nil {
		panic(err)
	}

	aggregatePrices, err = meter.Float64Gauge(
		"ark.pricefeed.aggregate.price",
		metric.WithDescription("Aggregated standalone oracle price"),
	)
	if err != nil {
		panic(err)
	}

	routePrices, err = meter.Float64Gauge(
		"ark.pricefeed.route.price",
		metric.WithDescription("Resolved standalone oracle route price before final aggregation"),
	)
	if err != nil {
		panic(err)
	}

	pairSampleCounts, err = meter.Int64Gauge(
		"ark.pricefeed.pair.sample.count",
		metric.WithDescription("Number of provider price samples used to aggregate an oracle pair"),
	)
	if err != nil {
		panic(err)
	}

	resolvedSourceCounts, err = meter.Int64Gauge(
		"ark.pricefeed.resolved.source.count",
		metric.WithDescription("Number of resolved route sources used for a standalone oracle pair"),
	)
	if err != nil {
		panic(err)
	}

	missingPrices, err = meter.Int64Counter(
		"ark.pricefeed.missing.prices",
		metric.WithDescription("Number of standalone oracle aggregation ticks missing a feed price"),
	)
	if err != nil {
		panic(err)
	}

	bootstrapPriceUses, err = meter.Int64Counter(
		"ark.pricefeed.bootstrap.price.uses",
		metric.WithDescription("Number of standalone oracle route legs resolved with a bootstrap price"),
	)
	if err != nil {
		panic(err)
	}
}

// RecordOracleTick records a completed standalone oracle aggregation tick.
func RecordOracleTick(ctx context.Context) {
	ticks.Add(ctx, 1)
}

// RecordProviderPrice records a fresh provider observation used by aggregation.
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

// RecordAggregatePrice records the final aggregated price for a pair.
func RecordAggregatePrice(ctx context.Context, pair string, price float64) {
	aggregatePrices.Record(
		ctx,
		price,
		metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))),
	)
}

// RecordRoutePrice records one resolved route before final route averaging.
func RecordRoutePrice(ctx context.Context, pair string, route string, price float64) {
	routePrices.Record(
		ctx,
		price,
		metric.WithAttributes(routeAttributes(pair, route)...),
	)
}

// RecordPairSampleCount records how many provider samples contributed to a pair median.
func RecordPairSampleCount(ctx context.Context, pair string, count int) {
	pairSampleCounts.Record(
		ctx,
		int64(count),
		metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))),
	)
}

// RecordResolvedSourceCount records how many routes contributed to a final pair price.
func RecordResolvedSourceCount(ctx context.Context, pair string, count int) {
	resolvedSourceCounts.Record(
		ctx,
		int64(count),
		metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))),
	)
}

// RecordBootstrapPriceUse records a route leg resolved without a fresh provider sample.
func RecordBootstrapPriceUse(ctx context.Context, pair string) {
	bootstrapPriceUses.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))),
	)
}

// RecordMissingPrice records an active feed missing from a price snapshot.
func RecordMissingPrice(ctx context.Context, denom string) {
	missingPrices.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("denom", normaliseLabelValue(denom))),
	)
}

// RecordMissingPrices records every active feed missing from an aggregation tick.
func RecordMissingPrices(ctx context.Context, feeds []string) {
	for _, denom := range feeds {
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
