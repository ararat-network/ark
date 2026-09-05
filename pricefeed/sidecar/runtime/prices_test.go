package runtime_test

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/mock/gomock"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	"github.com/ararat-network/ark/pricefeed/sidecar/runtime"
	runtimetestutil "github.com/ararat-network/ark/pricefeed/sidecar/runtime/testutil"
)

func TestRunFiltersStaleProviderPricesAndRecordsSyncTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	now := time.Now().UTC()
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	mp.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
			responseCh <- providertypes.NewResponse(
				map[providertypes.Ticker]providertypes.Result{
					"NOAHUSD": providertypes.NewResult(big.NewFloat(0.25), now),
					"NOAHKRW": providertypes.NewResult(big.NewFloat(2.50), now.Add(-2*time.Minute)),
				},
				nil,
			)
			<-ctx.Done()
			return ctx.Err()
		})

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(cfg, withInitialProviders(mp.provider))
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()

	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		prices := snapshot.Prices
		// The venue quotes a quarter dollar per NOAH; the USD/NOAH leg
		// normalises it to four NOAH per dollar, which is what the feed
		// publishes.
		if prices["ausd"] == nil || prices["ausd"].Cmp(big.NewFloat(4)) != 0 {
			return false
		}
		if _, ok := prices["akrw"]; ok {
			return false
		}
		return !snapshot.Timestamp.IsZero()
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunAppliesProviderSpecificMaxPriceAge(t *testing.T) {
	ctrl := gomock.NewController(t)
	markets := testMarkets()
	longLived := newMockProvider(t, ctrl, "long-lived", markets)
	shortLived := newMockProvider(t, ctrl, "short-lived", markets)
	priceTime := time.Now().UTC().Add(-30 * time.Second)
	for _, testProvider := range []struct {
		mockProvider mockProvider
		price        *big.Float
	}{
		{mockProvider: longLived, price: big.NewFloat(0.25)},
		{mockProvider: shortLived, price: big.NewFloat(9.25)},
	} {
		testProvider.mockProvider.fetcher.EXPECT().
			Run(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
				responseCh <- providertypes.NewResponse(
					map[providertypes.Ticker]providertypes.Result{
						"NOAHUSD": providertypes.NewResult(testProvider.price, priceTime),
					},
					nil,
				)
				<-ctx.Done()
				return ctx.Err()
			})
	}

	longCfg := testUnknownAPIProviderConfig("long-lived", markets)
	longCfg.MaxPriceAge = time.Minute
	shortCfg := testUnknownAPIProviderConfig("short-lived", markets)
	shortCfg.MaxPriceAge = 10 * time.Second
	cfg := testOracleConfig(map[string]providers.Config{
		longCfg.Name:  longCfg,
		shortCfg.Name: shortCfg,
	})
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"ausd"}

	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(longLived.provider, shortLived.provider),
		runtime.WithChainStateClient(newPassthroughChainStateClient(t, ctrl)),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		price := snapshot.Prices["ausd"]
		return !snapshot.Timestamp.IsZero() && price != nil &&
			price.Cmp(big.NewFloat(4)) == 0
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

// TestRunBoundsUnchangedResultsByLastObservation drives the same sequence
// through three providers -- a real price thirty seconds ago, then an unchanged
// refresh now -- under a ten-second MaxPriceAge. The refresh satisfies
// MaxPriceAge for all three; only MaxUnchangedAge separates them. The snapshot
// equalling the extended provider's price alone is a positive assertion that
// zero cannot extend and that a set bound is enforced.
func TestRunBoundsUnchangedResultsByLastObservation(t *testing.T) {
	ctrl := gomock.NewController(t)
	markets := testMarkets()
	observedAt := time.Now().UTC().Add(-30 * time.Second)
	testProviders := []struct {
		name            string
		price           *big.Float
		maxUnchangedAge time.Duration
	}{
		{name: "extended", price: big.NewFloat(0.5), maxUnchangedAge: time.Minute},
		{name: "unextended", price: big.NewFloat(0.1), maxUnchangedAge: 0},
		{name: "bounded", price: big.NewFloat(0.2), maxUnchangedAge: 20 * time.Second},
	}

	providerCfgs := make(map[string]providers.Config, len(testProviders))
	initial := make([]*base.Provider, 0, len(testProviders))
	for _, testProvider := range testProviders {
		mock := newMockProvider(t, ctrl, testProvider.name, markets)
		mock.fetcher.EXPECT().
			Run(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
				responseCh <- providertypes.NewResponse(
					map[providertypes.Ticker]providertypes.Result{
						"NOAHUSD": providertypes.NewResult(testProvider.price, observedAt),
					},
					nil,
				)
				responseCh <- providertypes.NewResponse(
					map[providertypes.Ticker]providertypes.Result{
						"NOAHUSD": providertypes.NewUnchangedResult(time.Now().UTC()),
					},
					nil,
				)
				<-ctx.Done()
				return ctx.Err()
			})

		cfg := testUnknownAPIProviderConfig(testProvider.name, markets)
		cfg.MaxPriceAge = 10 * time.Second
		cfg.MaxUnchangedAge = testProvider.maxUnchangedAge
		providerCfgs[cfg.Name] = cfg
		initial = append(initial, mock.provider)
	}

	cfg := testOracleConfig(providerCfgs)
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"ausd"}

	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(initial...),
		runtime.WithChainStateClient(newPassthroughChainStateClient(t, ctrl)),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		price := snapshot.Prices["ausd"]
		return !snapshot.Timestamp.IsZero() && price != nil &&
			price.Cmp(big.NewFloat(2)) == 0
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunUsesBootstrapPriceWhenProviderSampleIsMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"ausd"}
	cfg.Resolver.BootstrapPrices = []resolver.BootstrapPrice{{
		Pair:       "USD/NOAH",
		Price:      "0.25",
		ValidUntil: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(newPassthroughChainStateClient(t, ctrl)),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		price := snapshot.Prices["ausd"]
		return !snapshot.Timestamp.IsZero() && price != nil &&
			price.Cmp(big.NewFloat(0.25)) == 0
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunRecordsMissingPriceMetricsFromFallbackFeeds(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(meterProvider)

	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(cfg, withInitialProviders(mp.provider))
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	require.Eventually(t, func() bool {
		families, err := registry.Gather()
		if err != nil {
			return false
		}
		missingPrices := findOracleMetricFamily(families, "ark_pricefeed_missing_prices_total")
		if missingPrices == nil {
			return false
		}
		value, ok := oracleCounterValue(missingPrices, map[string]string{"denom": "akrw"})
		return ok && value >= 1
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestUpdateWaitsForInFlightPriceTick(t *testing.T) {
	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	mp.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
			responseCh <- providertypes.NewResponse(
				map[providertypes.Ticker]providertypes.Result{
					"NOAHUSD": providertypes.NewResult(big.NewFloat(1.25), time.Now().UTC()),
				},
				nil,
			)
			<-ctx.Done()
			return ctx.Err()
		}).
		AnyTimes()

	feedsClient := runtimetestutil.NewMockChainStateClient(ctrl)
	expectFeedsLifecycle(feedsClient)
	feedsStarted := make(chan struct{})
	allowFeeds := make(chan struct{})
	var feedsStartedOnce sync.Once
	var unblockFeedsOnce sync.Once
	unblockFeeds := func() {
		unblockFeedsOnce.Do(func() {
			close(allowFeeds)
		})
	}
	defer unblockFeeds()
	feedsClient.EXPECT().
		Feeds().
		DoAndReturn(func() ([]string, error) {
			feedsStartedOnce.Do(func() {
				close(feedsStarted)
			})
			<-allowFeeds
			return []string{"ausd"}, nil
		}).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireSignal(t, feedsStarted, "price tick did not start")

	newCfg := cfg
	newCfg.Resolver = testResolverConfig("ausd", "direct", "USD/NOAH")

	updateErrCh := make(chan error, 1)
	go func() {
		updateErrCh <- oracle.Update(newCfg)
	}()

	select {
	case err := <-updateErrCh:
		require.NoError(t, err)
		t.Fatal("runtime update returned while price tick was in flight")
	case <-time.After(20 * time.Millisecond):
	}

	unblockFeeds()
	select {
	case err := <-updateErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("runtime update did not return after price tick completed")
	}

	cancel()
	requireOracleStopped(t, errCh)
}

func findOracleMetricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	return nil
}

func oracleCounterValue(
	family *dto.MetricFamily,
	labels map[string]string,
) (float64, bool) {
	metric, ok := oracleMatchingMetric(family, labels)
	if !ok {
		return 0, false
	}
	return metric.GetCounter().GetValue(), true
}

func oracleMatchingMetric(
	family *dto.MetricFamily,
	labels map[string]string,
) (*dto.Metric, bool) {
	for _, metric := range family.Metric {
		actual := make(map[string]string, len(metric.Label))
		for _, label := range metric.Label {
			actual[label.GetName()] = label.GetValue()
		}
		matches := true
		for name, value := range labels {
			if actual[name] != value {
				matches = false
				break
			}
		}
		if matches {
			return metric, true
		}
	}

	return nil, false
}
