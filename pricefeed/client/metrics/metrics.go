package metrics

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/pricefeed/client/metrics")

	sidecarResponseLatency metric.Float64Histogram
	sidecarResponses       metric.Int64Counter
	sidecarLastSuccess     metric.Int64ObservableGauge
	snapshotTimestamp      metric.Int64Gauge
)

func init() {
	var err error
	sidecarResponseLatency, err = meter.Float64Histogram(
		"ark.pricefeed.response.duration",
		metric.WithDescription("Duration of price-feed sidecar responses"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}

	sidecarResponses, err = meter.Int64Counter(
		"ark.pricefeed.responses",
		metric.WithDescription("Number of price-feed sidecar responses by status"),
	)
	if err != nil {
		panic(err)
	}

	sidecarLastSuccess, err = meter.Int64ObservableGauge(
		"ark.pricefeed.sidecar.last_success",
		metric.WithDescription("Unix timestamp of the last valid price response from each sidecar"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}

	snapshotTimestamp, err = meter.Int64Gauge(
		"ark.pricefeed.snapshot.timestamp",
		metric.WithDescription("Unix timestamp the sidecar stamped on the last valid price snapshot; last_success is when the node fetched it"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		successMu.RLock()
		defer successMu.RUnlock()
		for address, timestamp := range successes {
			observer.ObserveInt64(sidecarLastSuccess, timestamp, metric.WithAttributes(attribute.String("address", address)))
		}
		return nil
	}, sidecarLastSuccess)
	if err != nil {
		panic(err)
	}
}

// RecordSidecarResponse records the latency and outcome of one request to the
// sidecar at address. A valid response also stamps the sidecar's last-success
// time: a sidecar that answers promptly but never validly is visible there and
// nowhere in the latency series.
func RecordSidecarResponse(address string, duration time.Duration, err error) {
	ctx := context.Background()
	addressAttr := metric.WithAttributes(attribute.String("address", address))
	sidecarResponseLatency.Record(ctx, float64(duration)/float64(time.Millisecond), addressAttr)
	sidecarResponses.Add(
		ctx,
		1,
		addressAttr,
		metric.WithAttributes(attribute.String("status", responseStatus(err))),
	)
	if err == nil {
		successMu.Lock()
		successes[address] = time.Now().Unix()
		successMu.Unlock()
	}
}

func responseStatus(err error) string {
	if err == nil {
		return "success"
	}
	return "failure"
}

// RecordSnapshotTimestamp records when the sidecar at address aggregated the
// snapshot the node just accepted. Against last_success it separates a sidecar
// that answers from one whose snapshot is moving: the node rejects a snapshot
// at price_ttl, and this is the lead time before it does.
func RecordSnapshotTimestamp(address string, at time.Time) {
	snapshotTimestamp.Record(
		context.Background(),
		at.UTC().Unix(),
		metric.WithAttributes(attribute.String("address", address)),
	)
}

var (
	successMu sync.RWMutex
	successes = make(map[string]int64)
)

// SetEndpoints publishes configured endpoints, including those never contacted.
// Reinitialisation preserves success times for retained endpoints.
func SetEndpoints(ctx context.Context, addresses []string) {
	successMu.Lock()
	defer successMu.Unlock()
	next := make(map[string]int64, len(addresses))
	for _, address := range addresses {
		next[address] = successes[address]
		for _, status := range []string{"success", "failure"} {
			sidecarResponses.Add(ctx, 0, metric.WithAttributes(attribute.String("address", address), attribute.String("status", status)))
		}
	}
	successes = next
}
