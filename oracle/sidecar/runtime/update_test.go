package runtime_test

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"ark/oracle/sidecar/chainstate"
	"ark/oracle/sidecar/providers"
	providertypes "ark/oracle/sidecar/providers/types"
	. "ark/oracle/sidecar/runtime"
	oracletestutil "ark/oracle/sidecar/runtime/testutil"
)

func TestUpdateConfigAppliesUpdateIntervalWithoutRestart(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})
	cfg.UpdateInterval = time.Hour
	cfg.FallbackDenoms = []string{"uusd"}
	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	started := make(chan struct{})
	expectFetcherRun(mp.fetcher, started)

	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		WithChainStateClient(newPassthroughChainStateClient(t, ctrl)),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Run(ctx)
	}()
	requireOracleStarted(t, oracle)
	requireProviderStarted(t, started)

	newCfg := cfg
	newCfg.UpdateInterval = 10 * time.Millisecond
	require.NoError(t, oracle.Update(newCfg))

	require.Eventually(t, func() bool {
		return !oracle.GetPriceSnapshot().Timestamp.IsZero()
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestUpdateConfigAppliesResolverConfigOnNextTick(t *testing.T) {
	markets := testRouteMarkets()
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", markets),
	})
	cfg.UpdateInterval = 5 * time.Millisecond
	newCfg := cfg
	newCfg.Resolver = testResolverConfig("ukrw", "noah-krw", "NOAH/USD", "USD/KRW")

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", markets)
	firstStarted := make(chan struct{})
	restarted := make(chan struct{})
	var runMu sync.Mutex
	runs := 0
	mp.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			ctx context.Context,
			tickers []providertypes.Ticker,
			responseCh chan<- providertypes.Response,
		) error {
			runMu.Lock()
			runs++
			run := runs
			runMu.Unlock()

			results := map[providertypes.Ticker]providertypes.Result{
				"NOAHUSD": providertypes.NewResult(big.NewFloat(2), time.Now().UTC()),
			}
			switch run {
			case 1:
				require.Equal(t, []providertypes.Ticker{"NOAHUSD"}, tickers)
				close(firstStarted)
			case 2:
				require.Equal(t, []providertypes.Ticker{"NOAHUSD", "USDKRW"}, tickers)
				results["USDKRW"] = providertypes.NewResult(big.NewFloat(1000), time.Now().UTC())
				close(restarted)
			default:
				t.Fatalf("unexpected provider run %d", run)
			}
			responseCh <- providertypes.NewResponse(results, nil)
			<-ctx.Done()
			return ctx.Err()
		}).
		Times(2)

	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"uusd", "ukrw"}, nil).
		AnyTimes()
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, firstStarted)
	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		usd := snapshot.Prices["uusd"]
		_, hasKRW := snapshot.Prices["ukrw"]
		return usd != nil && usd.Cmp(big.NewFloat(2)) == 0 && !hasKRW
	}, time.Second, time.Millisecond)

	require.NoError(t, oracle.Update(newCfg))
	requireProviderStarted(t, restarted)
	require.Eventually(t, func() bool {
		price := oracle.GetPriceSnapshot().Prices["ukrw"]
		return price != nil && price.Cmp(big.NewFloat(2000)) == 0
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestUpdateConfigReturnsInvalidResolverErrorWithoutChangingConfig(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})
	newCfg := cfg
	newCfg.Resolver = testResolverConfig("ukrw", "bad-route", "USDT/USD")

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
	)
	require.NoError(t, err)

	err = oracle.Update(newCfg)

	require.ErrorContains(t, err, "resolver denom \"ukrw\" route \"bad-route\" resolves to \"USDT/USD\", want \"NOAH/KRW\"")
}

func TestUpdateConfigUpdatesVoteTargetsClientConfig(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})
	newCfg := cfg
	newCfg.Client.Interval = 10 * time.Millisecond

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	voteTargetsClient, voteTargetsRecorder := newRecordingChainStateClient(t, ctrl)
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	require.NoError(t, oracle.Update(newCfg))
	require.Equal(t, []chainstate.Config{newCfg.Client}, voteTargetsRecorder.updateConfigs())
}

func TestUpdateConfigRefreshesFallbackDenomsWhenNoVoteTargetsHaveLoaded(t *testing.T) {
	providerCfg := testUnknownAPIProviderConfig("unknown", testMarkets())
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
	)
	require.NoError(t, err)

	newCfg := cfg
	newCfg.FallbackDenoms = []string{"uusd"}

	require.NoError(t, oracle.Update(newCfg))
	require.Equal(t, []providertypes.Ticker{"NOAHUSD"}, mp.provider.GetTickers())
}
