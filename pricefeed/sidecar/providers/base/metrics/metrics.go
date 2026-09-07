package metrics

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var (
	meter = otel.Meter("ark/pricefeed/sidecar/providers/base/metrics")

	responses   metric.Int64Counter
	lastSuccess metric.Int64ObservableGauge
)

func init() {
	var err error
	responses, err = meter.Int64Counter(
		"ark.pricefeed.provider.responses",
		metric.WithDescription("Number of provider responses by error code, \"ok\" for a resolved one"),
	)
	if err != nil {
		panic(err)
	}

	lastSuccess, err = meter.Int64ObservableGauge(
		"ark.pricefeed.provider.last_success",
		metric.WithDescription("Unix timestamp of the last resolved provider response"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		successMu.RLock()
		defer successMu.RUnlock()
		for key, timestamp := range successes {
			observer.ObserveInt64(lastSuccess, timestamp, metric.WithAttributes(attribute.String("provider", key[0]), attribute.String("ticker", key[1])))
		}
		return nil
	}, lastSuccess)
	if err != nil {
		panic(err)
	}
}

// RecordResponse records the outcome of a provider response for a ticker.
func RecordResponse(
	ctx context.Context,
	provider string,
	ticker types.Ticker,
	errorCode types.ErrorCode,
) {
	normalisedTicker := ticker.Key()
	responses.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("ticker", normalisedTicker),
		attribute.String("error_code", errorCode.String()),
	))

	if errorCode == types.OK {
		successMu.Lock()
		successes[[2]string{provider, normalisedTicker}] = time.Now().UTC().Unix()
		successMu.Unlock()
	}
}

var (
	successMu sync.RWMutex
	successes = make(map[[2]string]int64)
)

// SetTickers retires removed markets and seeds never-successful ones, retaining
// the last response timestamp of unchanged markets across fetcher restarts.
func SetTickers(ctx context.Context, provider string, tickers []types.Ticker) {
	successMu.Lock()
	defer successMu.Unlock()
	wanted := make(map[[2]string]bool, len(tickers))
	for _, ticker := range tickers {
		key := [2]string{provider, ticker.Key()}
		wanted[key] = true
		if _, exists := successes[key]; !exists {
			successes[key] = 0
		}
		responses.Add(ctx, 0, metric.WithAttributes(attribute.String("provider", provider), attribute.String("ticker", key[1]), attribute.String("error_code", types.OK.String())))
	}
	for key := range successes {
		if key[0] == provider && !wanted[key] {
			delete(successes, key)
		}
	}
}

// RemoveProvider retires gauges once a removed provider has stopped.
func RemoveProvider(provider string) {
	successMu.Lock()
	defer successMu.Unlock()
	for key := range successes {
		if key[0] == provider {
			delete(successes, key)
		}
	}
}
