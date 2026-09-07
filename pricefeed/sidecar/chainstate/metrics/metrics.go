package metrics

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/pricefeed/sidecar/chainstate/metrics")

	refreshes   metric.Int64Counter
	lastSuccess metric.Int64ObservableGauge
)

// statusSuccess is the refresh status that moves the last-success gauge.
const statusSuccess = "success"

func init() {
	var err error

	refreshes, err = meter.Int64Counter(
		"ark.pricefeed.chainstate.refreshes",
		metric.WithDescription("Number of chain state feed refresh attempts by chain node"),
	)
	if err != nil {
		panic(err)
	}

	lastSuccess, err = meter.Int64ObservableGauge(
		"ark.pricefeed.chainstate.last_success",
		metric.WithDescription("Unix timestamp of the last successful feed refresh from each chain node"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		successMu.RLock()
		defer successMu.RUnlock()
		for address, timestamp := range successes {
			observer.ObserveInt64(lastSuccess, timestamp, metric.WithAttributes(attribute.String("address", address)))
		}
		return nil
	}, lastSuccess)
	if err != nil {
		panic(err)
	}
}

// RecordRefresh records one chainstate feed refresh attempt against one chain
// node. The address is what makes a failover visible: a sweep records one
// attempt per endpoint it tried, so the served node is the one whose success
// count is still climbing.
func RecordRefresh(ctx context.Context, address, status string) {
	status = normaliseLabelValue(status)
	refreshes.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("address", address),
			attribute.String("status", status),
		),
	)
	if status == statusSuccess {
		successMu.Lock()
		successes[address] = time.Now().UTC().Unix()
		successMu.Unlock()
	}
}

func normaliseLabelValue(value string) string {
	return strings.ToLower(value)
}

var (
	successMu sync.RWMutex
	successes = make(map[string]int64)
)

// SetEndpoints reconciles the configured endpoint set without resetting
// retained timestamps on retries, reconnects, or timing-only updates.
func SetEndpoints(ctx context.Context, addresses []string) {
	successMu.Lock()
	defer successMu.Unlock()
	next := make(map[string]int64, len(addresses))
	for _, address := range addresses {
		next[address] = successes[address]
		for _, status := range []string{"success", "error"} {
			refreshes.Add(ctx, 0, metric.WithAttributes(attribute.String("address", address), attribute.String("status", status)))
		}
	}
	successes = next
}
