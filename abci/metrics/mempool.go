package metrics

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	poolTxs          metric.Int64ObservableGauge
	poolBytes        metric.Int64ObservableGauge
	admissionResults metric.Int64Counter

	// The occupancy read installed by ObservePool; one pool per process.
	poolUsage atomic.Pointer[func() ([3]int, [3]int64)]

	// Reuse immutable options instead of allocating labels on each scrape.
	poolOptions = [3][]metric.ObserveOption{
		{metric.WithAttributes(attribute.String("lane", "normal"))},
		{metric.WithAttributes(attribute.String("lane", "governance"))},
		{metric.WithAttributes(attribute.String("lane", "committee"))},
	}
)

func init() {
	var err error
	poolTxs, err = meter.Int64ObservableGauge("ark.mempool.transactions", metric.WithDescription("Pending transactions by admission allocation"))
	if err != nil {
		panic(err)
	}
	poolBytes, err = meter.Int64ObservableGauge("ark.mempool.bytes", metric.WithUnit("By"), metric.WithDescription("Pending transaction bytes by admission allocation"))
	if err != nil {
		panic(err)
	}
	if _, err = meter.RegisterCallback(observePool, poolTxs, poolBytes); err != nil {
		panic(err)
	}
	admissionResults, err = meter.Int64Counter("ark.mempool.admissions", metric.WithDescription("SDK CheckTx outcomes, including CometBFT rechecks"))
	if err != nil {
		panic(err)
	}
}

// ObservePool installs the occupancy read the pool gauges report at scrape
// time, keeping metric writes off the admission path.
func ObservePool(usage func() ([3]int, [3]int64)) { poolUsage.Store(&usage) }

func observePool(_ context.Context, o metric.Observer) error {
	usage := poolUsage.Load()
	if usage == nil {
		return nil
	}
	counts, sizes := (*usage)()
	for i, opts := range poolOptions {
		o.ObserveInt64(poolTxs, int64(counts[i]), opts...)
		o.ObserveInt64(poolBytes, sizes[i], opts...)
	}
	return nil
}

func RecordAdmission(status string) {
	admissionResults.Add(context.Background(), 1, metric.WithAttributes(attribute.String("status", status)))
}
