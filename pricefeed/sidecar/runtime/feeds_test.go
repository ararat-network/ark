package runtime_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	"github.com/ararat-network/ark/pricefeed/sidecar/runtime"
	oracletestutil "github.com/ararat-network/ark/pricefeed/sidecar/runtime/testutil"
)

func TestRunUsesFeedsWhenRefreshSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	feedsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectFeedsLifecycle(feedsClient)
	feedsClient.EXPECT().
		Feeds().
		Return([]string{"ausd"}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"akrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCommittedSnapshot(t, oracle)
	require.Equal(t, []providertypes.Ticker{"NOAHUSD"}, mp.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunUsesAuthoritativeEmptyFeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	feedsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectFeedsLifecycle(feedsClient)
	feedsClient.EXPECT().
		Feeds().
		Return([]string{}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"akrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCommittedSnapshot(t, oracle)
	require.Eventually(t, func() bool {
		return len(mp.provider.GetTickers()) == 0
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunDoesNotRestartProviderWhenFeedsAreUnchanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRun(mp.fetcher, started)
	feedsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectFeedsLifecycle(feedsClient)
	feedsClient.EXPECT().
		Feeds().
		Return([]string{"ausd"}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"ausd"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)
	requireCommittedSnapshot(t, oracle)
	require.Equal(t, []providertypes.Ticker{"NOAHUSD"}, mp.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunRestartsStoppedProviderWhenFeedsChangeMarkets(t *testing.T) {
	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	firstStarted := make(chan struct{})
	restarted := make(chan struct{})
	expectFetcherRunErrorThenBlock(
		t,
		mp.fetcher,
		firstStarted,
		restarted,
		[]providertypes.Ticker{"NOAHUSD"},
		[]providertypes.Ticker{"NOAHKRW"},
	)
	feedsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectFeedsLifecycle(feedsClient)
	feedsClient.EXPECT().
		Feeds().
		Return([]string{"akrw"}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"ausd"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, firstStarted)
	requireProviderStarted(t, restarted)
	require.Equal(t, []providertypes.Ticker{"NOAHKRW"}, mp.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunUsesFallbackFeedsWhenFeedsFailBeforeSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	feedsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectFeedsLifecycle(feedsClient)
	feedsClient.EXPECT().
		Feeds().
		Return(nil, errors.New("node unavailable")).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"akrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCommittedSnapshot(t, oracle)
	require.Equal(t, []providertypes.Ticker{"NOAHKRW"}, mp.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunKeepsLastFeedsAfterRefreshFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	feedsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectFeedsLifecycle(feedsClient)
	calls := 0
	callCh := make(chan int, 2)
	feedsClient.EXPECT().
		Feeds().
		DoAndReturn(func() ([]string, error) {
			calls++
			select {
			case callCh <- calls:
			default:
			}
			if calls == 1 {
				return []string{"ausd"}, nil
			}
			return nil, errors.New("node unavailable")
		}).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackFeeds = []string{"akrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(feedsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCallNumber(t, callCh, 1)
	requireCommittedSnapshot(t, oracle)
	firstSnapshot := oracle.GetPriceSnapshot()
	requireCallNumber(t, callCh, 2)
	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		return snapshot.Timestamp.After(firstSnapshot.Timestamp)
	}, time.Second, time.Millisecond)
	require.Equal(t, []providertypes.Ticker{"NOAHUSD"}, mp.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func requireCommittedSnapshot(t *testing.T, oracle *runtime.Runtime) {
	t.Helper()

	require.Eventually(t, func() bool {
		return !oracle.GetPriceSnapshot().Timestamp.IsZero()
	}, time.Second, time.Millisecond)
}

func requireCallNumber(t *testing.T, calls <-chan int, want int) {
	t.Helper()

	select {
	case got := <-calls:
		require.Equal(t, want, got)
	case <-time.After(time.Second):
		t.Fatalf("feed call %d did not occur", want)
	}
}
