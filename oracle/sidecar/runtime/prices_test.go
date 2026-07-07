package runtime_test

import (
	"context"
	"errors"
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

	providertypes "noah/oracle/sidecar/providers/types"
	resolverpkg "noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/runtime"
	oracletestutil "noah/oracle/sidecar/runtime/testutil"
	"noah/oracle/sidecar/types"
)

func TestStartFiltersStaleProviderPricesAndRecordsSyncTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	now := time.Now().UTC()
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	provider.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
			responseCh <- providertypes.NewResponse(
				map[providertypes.Ticker]providertypes.Result{
					"ARKUSD": providertypes.NewResult(big.NewFloat(1.25), now),
					"ARKKRW": providertypes.NewResult(big.NewFloat(2.50), now.Add(-2*time.Minute)),
				},
				nil,
			)
			<-ctx.Done()
			return ctx.Err()
		})

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(cfg, runtime.WithProviders(provider.provider))
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()

	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		prices := snapshot.Prices
		if prices["uusd"] == nil || prices["uusd"].Cmp(big.NewFloat(1.25)) != 0 {
			return false
		}
		if prices["ukrw"] == nil || prices["ukrw"].Sign() != 0 {
			return false
		}
		return !snapshot.Timestamp.IsZero()
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartRecordsMissingPriceMetricsFromFallbackDenoms(t *testing.T) {
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
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(provider.fetcher, started)

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(cfg, runtime.WithProviders(provider.provider))
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	require.Eventually(t, func() bool {
		families, err := registry.Gather()
		if err != nil {
			return false
		}
		missingPrices := findOracleMetricFamily(families, "noah_oracle_missing_prices_total")
		if missingPrices == nil {
			return false
		}
		value, ok := oracleCounterValue(missingPrices, map[string]string{"denom": "ukrw"})
		return ok && value >= 1
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartDoesNotRecordMissingPriceMetricForPresentZeroPrice(t *testing.T) {
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
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(provider.fetcher, started)
	resolver := newRecordingResolver(types.Prices{
		"ARK/KRW": new(big.Float),
	})

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithProviders(provider.provider),
		runtime.WithResolver(resolver),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	require.Eventually(t, func() bool {
		return !oracle.GetPriceSnapshot().Timestamp.IsZero()
	}, time.Second, time.Millisecond)
	families, err := registry.Gather()
	require.NoError(t, err)
	missingPrices := findOracleMetricFamily(families, "noah_oracle_missing_prices_total")
	if missingPrices != nil {
		_, ok := oracleCounterValue(missingPrices, map[string]string{"denom": "ukrw"})
		require.False(t, ok)
	}

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartUsesVoteTargetsWhenRefreshSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(provider.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"uusd"}, nil).
		AnyTimes()

	resolver := newRecordingResolver(nil)
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithProviders(provider.provider),
		runtime.WithResolver(resolver),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	require.Equal(t, []string{"uusd"}, requireResolvedDenoms(t, resolver))
	require.Equal(t, []providertypes.Ticker{"ARKUSD"}, provider.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartRestartsStoppedProviderWhenVoteTargetsChangeMarkets(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	firstStarted := make(chan struct{})
	restarted := make(chan struct{})
	expectFetcherRunErrorThenBlock(
		t,
		provider.fetcher,
		firstStarted,
		restarted,
		testMarkets().Tickers(),
		[]providertypes.Ticker{"ARKKRW"},
	)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"ukrw"}, nil).
		AnyTimes()

	resolver := newRecordingResolver(nil)
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"uusd"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithProviders(provider.provider),
		runtime.WithResolver(resolver),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, firstStarted)
	requireProviderStarted(t, restarted)
	require.Equal(t, []providertypes.Ticker{"ARKKRW"}, provider.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartUsesFallbackDenomsWhenVoteTargetsFailBeforeSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(provider.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return(nil, errors.New("node unavailable")).
		AnyTimes()

	resolver := newRecordingResolver(nil)
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithProviders(provider.provider),
		runtime.WithResolver(resolver),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	require.Equal(t, []string{"ukrw"}, requireResolvedDenoms(t, resolver))

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartKeepsLastVoteTargetsAfterRefreshFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(provider.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	calls := 0
	voteTargetsClient.EXPECT().
		VoteTargets().
		DoAndReturn(func() ([]string, error) {
			calls++
			if calls == 1 {
				return []string{"uusd"}, nil
			}
			return nil, errors.New("node unavailable")
		}).
		AnyTimes()

	resolver := newRecordingResolver(nil)
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithProviders(provider.provider),
		runtime.WithResolver(resolver),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	require.Equal(t, []string{"uusd"}, requireResolvedDenoms(t, resolver))
	require.Equal(t, []string{"uusd"}, requireResolvedDenoms(t, resolver))
	require.GreaterOrEqual(t, calls, 2)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestUpdateWaitsForInFlightPriceTick(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	provider.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
			responseCh <- providertypes.NewResponse(
				map[providertypes.Ticker]providertypes.Result{
					"ARKUSD": providertypes.NewResult(big.NewFloat(1.25), time.Now().UTC()),
				},
				nil,
			)
			<-ctx.Done()
			return ctx.Err()
		}).
		AnyTimes()

	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"uusd"}, nil).
		AnyTimes()

	resolver := newBlockingTickResolver()
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithProviders(provider.provider),
		runtime.WithResolver(resolver),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)
	defer resolver.unblock()

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireSignal(t, resolver.setStartedCh, "price tick did not start")

	newCfg := cfg
	newCfg.Resolver = testResolverConfig("uusd", "direct", "ARK/USD")

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

	resolver.unblock()
	select {
	case err := <-updateErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("runtime update did not return after price tick completed")
	}

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartPanicsWhenResolverResetPanics(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(provider.fetcher, started)

	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"uusd"}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = time.Millisecond
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithProviders(provider.provider),
		runtime.WithResolver(panicResetResolver{}),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	require.PanicsWithValue(t, "resolver reset exploded", func() {
		require.NoError(t, oracle.Start(context.Background()))
	})
}

type recordingResolver struct {
	mut       sync.Mutex
	prices    types.Prices
	resolveCh chan []string
	updates   []resolverpkg.Config
}

func newRecordingResolver(prices types.Prices) *recordingResolver {
	return &recordingResolver{
		prices:    prices,
		resolveCh: make(chan []string, 10),
	}
}

func (r *recordingResolver) SetProviderPrices(string, types.Prices) {}

func (r *recordingResolver) ResolvePrices(denoms []string) {
	r.resolveCh <- append([]string(nil), denoms...)
}

func (r *recordingResolver) GetPrices() types.Prices {
	prices := make(types.Prices, len(r.prices))
	for pair, price := range r.prices {
		prices[pair] = new(big.Float).Copy(price)
	}
	return prices
}

func (r *recordingResolver) Update(cfg resolverpkg.Config) {
	r.mut.Lock()
	defer r.mut.Unlock()

	r.updates = append(r.updates, cfg)
}

func (r *recordingResolver) updateConfigs() []resolverpkg.Config {
	r.mut.Lock()
	defer r.mut.Unlock()

	return append([]resolverpkg.Config(nil), r.updates...)
}

func (r *recordingResolver) Reset() {}

type panicResetResolver struct{}

func (panicResetResolver) SetProviderPrices(string, types.Prices) {}

func (panicResetResolver) ResolvePrices([]string) {}

func (panicResetResolver) GetPrices() types.Prices { return nil }

func (panicResetResolver) Update(resolverpkg.Config) {}

func (panicResetResolver) Reset() {
	panic("resolver reset exploded")
}

type blockingTickResolver struct {
	setStarted sync.Once
	unblocked  sync.Once

	setStartedCh chan struct{}
	allowSet     chan struct{}
}

func newBlockingTickResolver() *blockingTickResolver {
	return &blockingTickResolver{
		setStartedCh: make(chan struct{}),
		allowSet:     make(chan struct{}),
	}
}

func (r *blockingTickResolver) SetProviderPrices(string, types.Prices) {
	r.setStarted.Do(func() {
		close(r.setStartedCh)
	})
	<-r.allowSet
}

func (r *blockingTickResolver) ResolvePrices([]string) {}

func (r *blockingTickResolver) GetPrices() types.Prices {
	return types.Prices{}
}

func (r *blockingTickResolver) Update(resolverpkg.Config) {}

func (r *blockingTickResolver) Reset() {}

func (r *blockingTickResolver) unblock() {
	r.unblocked.Do(func() {
		close(r.allowSet)
	})
}

func expectVoteTargetsLifecycle(client *oracletestutil.MockChainStateClient) {
	client.EXPECT().
		Start(gomock.Any()).
		Return(nil).
		AnyTimes()
	client.EXPECT().Stop().AnyTimes()
}

func requireResolvedDenoms(t *testing.T, resolver *recordingResolver) []string {
	t.Helper()

	select {
	case denoms := <-resolver.resolveCh:
		return denoms
	case <-time.After(time.Second):
		t.Fatal("oracle did not resolve prices")
		return nil
	}
}

func startOracle(t *testing.T, oracle *runtime.Runtime) (<-chan error, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Start(ctx)
	}()
	requireOracleStarted(t, oracle)

	return errCh, cancel
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
