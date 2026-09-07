package metrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	poolTxs          metric.Int64Gauge
	poolBytes        metric.Int64Gauge
	admissionResults metric.Int64Counter
)

func init() {
	var err error
	poolTxs, err = meter.Int64Gauge("ark.mempool.transactions", metric.WithDescription("Pending transactions by admission allocation"))
	if err != nil {
		panic(err)
	}
	poolBytes, err = meter.Int64Gauge("ark.mempool.bytes", metric.WithUnit("By"), metric.WithDescription("Pending transaction bytes by admission allocation"))
	if err != nil {
		panic(err)
	}
	admissionResults, err = meter.Int64Counter("ark.mempool.admissions", metric.WithDescription("Admission attempt outcomes, including post-commit revalidation"))
	if err != nil {
		panic(err)
	}
}

func RecordPool(counts [3]int, sizes [3]int64) {
	for i, lane := range []string{"normal", "governance", "committee"} {
		attrs := metric.WithAttributes(attribute.String("lane", lane))
		poolTxs.Record(context.Background(), int64(counts[i]), attrs)
		poolBytes.Record(context.Background(), sizes[i], attrs)
	}
}

func RecordAdmission(status string) {
	admissionResults.Add(context.Background(), 1, metric.WithAttributes(attribute.String("status", status)))
}
