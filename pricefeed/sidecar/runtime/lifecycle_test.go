package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"ark/pricefeed/sidecar/providers"
	providertypes "ark/pricefeed/sidecar/providers/types"
	. "ark/pricefeed/sidecar/runtime"
)

func TestRunStartsAndStopsChainStateClient(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})

	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRun(mp.fetcher, started)

	feedsClient, feedsRecorder := newRecordingChainStateClient(t, ctrl)
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		errCh <- oracle.Run(ctx)
	}()
	requireOracleStarted(t, oracle)
	requireProviderStarted(t, started)
	feedsRecorder.requireStarted(t)

	cancel()
	requireOracleStopped(t, errCh)
	require.True(t, feedsRecorder.stopped())
}

func TestRunShutdownWaitsForConcurrentConfigUpdateProviderStops(t *testing.T) {
	markets := testMarkets()
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, providerCfg.Name, markets)
	started := make(chan struct{})
	stopStarted := make(chan struct{})
	allowStop := make(chan struct{})
	defer func() {
		select {
		case <-allowStop:
		default:
			close(allowStop)
		}
	}()
	mp.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, _ chan<- providertypes.Response) error {
			close(started)
			<-ctx.Done()
			close(stopStarted)
			<-allowStop
			return ctx.Err()
		})

	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		WithChainStateClient(newPassthroughChainStateClient(t, ctrl)),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	requireSignal(t, started, "provider did not start")

	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: newProviderCfg,
	})

	updateErrCh := make(chan error, 1)
	go func() {
		updateErrCh <- oracle.Update(newCfg)
	}()
	requireSignal(t, stopStarted, "provider stop did not begin")

	cancel()

	select {
	case <-errCh:
		t.Fatal("runtime Run returned before config update provider stop finished")
	case <-time.After(20 * time.Millisecond):
	}

	close(allowStop)
	select {
	case err := <-updateErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("runtime update did not return")
	}
	requireOracleStopped(t, errCh)
}
