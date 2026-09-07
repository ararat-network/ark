package metrics

import (
	"context"
	"maps"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Skip reasons: the reason label of ark.pricefeed.skipped.samples.
const (
	// SkipReasonStale is a sample older than the provider's max_price_age.
	SkipReasonStale = "stale"
	// SkipReasonUnchanged is a sample whose price has not moved for longer
	// than the provider's max_unchanged_age.
	SkipReasonUnchanged = "unchanged"
)

var (
	meter = otel.Meter("ark/pricefeed/sidecar/metrics")

	ticks              metric.Int64Counter
	aggregatePrices    metric.Float64ObservableGauge
	pairSampleCounts   metric.Int64ObservableGauge
	missingPrices      metric.Int64Counter
	bootstrapPriceUses metric.Int64Counter
	skippedSamples     metric.Int64Counter
	rpcRequests        metric.Int64Counter
)

func init() {
	var err error

	ticks, err = meter.Int64Counter(
		"ark.pricefeed.ticks",
		metric.WithDescription("Number of sidecar aggregation ticks"),
	)
	if err != nil {
		panic(err)
	}

	aggregatePrices, err = meter.Float64ObservableGauge(
		"ark.pricefeed.aggregate.price",
		metric.WithDescription("Sidecar aggregate price per pair"),
	)
	if err != nil {
		panic(err)
	}

	pairSampleCounts, err = meter.Int64ObservableGauge(
		"ark.pricefeed.pair.sample.count",
		metric.WithDescription("Number of provider price samples used to aggregate a pair"),
	)
	if err != nil {
		panic(err)
	}

	missingPrices, err = meter.Int64Counter(
		"ark.pricefeed.missing.prices",
		metric.WithDescription("Number of aggregation ticks missing a feed price"),
	)
	if err != nil {
		panic(err)
	}

	bootstrapPriceUses, err = meter.Int64Counter(
		"ark.pricefeed.bootstrap.price.uses",
		metric.WithDescription("Number of route legs resolved with a bootstrap price"),
	)
	if err != nil {
		panic(err)
	}

	skippedSamples, err = meter.Int64Counter(
		"ark.pricefeed.skipped.samples",
		metric.WithDescription("Number of cached provider samples left out of an aggregation tick, by reason"),
	)
	if err != nil {
		panic(err)
	}

	rpcRequests, err = meter.Int64Counter(
		"ark.pricefeed.rpc.requests",
		metric.WithDescription("Number of RPCs the sidecar served, by method and gRPC status code"),
	)
	if err != nil {
		panic(err)
	}

	_, err = meter.RegisterCallback(observeAggregation, aggregatePrices, pairSampleCounts)
	if err != nil {
		panic(err)
	}
}

// RecordTick records a completed aggregation tick.
func RecordTick(ctx context.Context) {
	ticks.Add(ctx, 1)
}

// AggregationSnapshot is one completed resolution's metric observations.
// Prices absent from this snapshot are unavailable, never a numeric zero.
type AggregationSnapshot struct {
	Prices       map[string]float64
	SampleCounts map[string]int64
}

var aggregationSnapshot atomic.Pointer[AggregationSnapshot]

// PublishAggregationSnapshot takes a defensive copy, then atomically replaces
// both gauge families. Collection reads no provider or runtime locks.
func PublishAggregationSnapshot(snapshot AggregationSnapshot) {
	aggregationSnapshot.Store(&AggregationSnapshot{
		Prices:       maps.Clone(snapshot.Prices),
		SampleCounts: maps.Clone(snapshot.SampleCounts),
	})
}

func observeAggregation(_ context.Context, observer metric.Observer) error {
	snapshot := aggregationSnapshot.Load()
	if snapshot == nil {
		return nil
	}
	for pair, price := range snapshot.Prices {
		observer.ObserveFloat64(aggregatePrices, price, metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))))
	}
	for pair, count := range snapshot.SampleCounts {
		observer.ObserveInt64(pairSampleCounts, count, metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))))
	}
	return nil
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

// RecordSkippedSample records a cached provider sample an aggregation tick
// left out. The provider's own meters stay healthy through an unchanged
// heartbeat, so this is the series that shows a live provider contributing
// nothing.
func RecordSkippedSample(ctx context.Context, provider string, pair string, reason string) {
	skippedSamples.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("pair", normaliseLabelValue(pair)),
		attribute.String("reason", reason),
	))
}

// RecordRPC records one RPC the sidecar served: the full gRPC method name
// and the status code it answered with.
func RecordRPC(ctx context.Context, method string, code string) {
	methodAttr := attribute.String("method", method)
	rpcRequests.Add(ctx, 1, metric.WithAttributes(methodAttr, attribute.String("code", code)))
}

func normaliseLabelValue(value string) string {
	return strings.ToLower(value)
}

// Initialise seeds the aggregation heartbeat and known feed counters without
// inventing a completed tick or a successful price. Add(0) retains history.
func Initialise(ctx context.Context, feeds, pairs []string) {
	ticks.Add(ctx, 0)
	for _, denom := range feeds {
		missingPrices.Add(ctx, 0, metric.WithAttributes(attribute.String("denom", normaliseLabelValue(denom))))
	}
	for _, pair := range pairs {
		bootstrapPriceUses.Add(ctx, 0, metric.WithAttributes(attribute.String("pair", normaliseLabelValue(pair))))
	}
}
