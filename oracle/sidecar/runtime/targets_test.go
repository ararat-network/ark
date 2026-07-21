package runtime_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	providertypes "ark/oracle/sidecar/providers/types"
	"ark/oracle/sidecar/runtime"
	oracletestutil "ark/oracle/sidecar/runtime/testutil"
)

func TestRunUsesVoteTargetsWhenRefreshSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"uusd"}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCommittedDenoms(t, oracle, "uusd")
	require.Equal(t, []providertypes.Ticker{"NOAHUSD"}, mp.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunUsesAuthoritativeEmptyVoteTargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCommittedDenoms(t, oracle)
	require.Eventually(t, func() bool {
		return len(mp.provider.GetTickers()) == 0
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunDoesNotRestartProviderWhenVoteTargetsAreUnchanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRun(mp.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"uusd"}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"uusd"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)
	requireCommittedDenoms(t, oracle, "uusd")

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunRestartsStoppedProviderWhenVoteTargetsChangeMarkets(t *testing.T) {
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
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"ukrw"}, nil).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"uusd"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(voteTargetsClient),
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

func TestRunUsesFallbackDenomsWhenVoteTargetsFailBeforeSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return(nil, errors.New("node unavailable")).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCommittedDenoms(t, oracle, "ukrw")

	cancel()
	requireOracleStopped(t, errCh)
}

func TestRunKeepsLastVoteTargetsAfterRefreshFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRunAnyTimes(mp.fetcher, started)
	voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
	expectVoteTargetsLifecycle(voteTargetsClient)
	calls := 0
	callCh := make(chan int, 2)
	voteTargetsClient.EXPECT().
		VoteTargets().
		DoAndReturn(func() ([]string, error) {
			calls++
			select {
			case callCh <- calls:
			default:
			}
			if calls == 1 {
				return []string{"uusd"}, nil
			}
			return nil, errors.New("node unavailable")
		}).
		AnyTimes()

	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ukrw"}
	oracle, err := runtime.NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		runtime.WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, started)

	requireCallNumber(t, callCh, 1)
	requireCommittedDenoms(t, oracle, "uusd")
	firstSnapshot := oracle.GetPriceSnapshot()
	requireCallNumber(t, callCh, 2)
	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		_, hasUSD := snapshot.Prices["uusd"]
		return snapshot.Timestamp.After(firstSnapshot.Timestamp) && hasUSD && len(snapshot.Prices) == 1
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}
func requireCommittedDenoms(t *testing.T, oracle *runtime.Runtime, denoms ...string) {
	t.Helper()

	require.Eventually(t, func() bool {
		snapshot := oracle.GetPriceSnapshot()
		if snapshot.Timestamp.IsZero() || len(snapshot.Prices) != len(denoms) {
			return false
		}
		for _, denom := range denoms {
			if _, ok := snapshot.Prices[denom]; !ok {
				return false
			}
		}
		return true
	}, time.Second, time.Millisecond)
}

func requireCallNumber(t *testing.T, calls <-chan int, want int) {
	t.Helper()

	select {
	case got := <-calls:
		require.Equal(t, want, got)
	case <-time.After(time.Second):
		t.Fatalf("vote-target call %d did not occur", want)
	}
}
