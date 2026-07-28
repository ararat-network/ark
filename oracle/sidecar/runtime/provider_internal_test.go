package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"ark/oracle/sidecar/providers/base"
	basetestutil "ark/oracle/sidecar/providers/base/testutil"
	providertypes "ark/oracle/sidecar/providers/types"
)

func TestManagedProviderStopDoesNotLogIntentionalExit(t *testing.T) {
	logs := new(bytes.Buffer)
	started := make(chan struct{})
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()
	fetcher.EXPECT().ResponseBufferSize(gomock.Any()).Return(1)
	fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			ctx context.Context,
			_ []providertypes.Ticker,
			_ chan<- providertypes.Response,
		) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	provider, err := base.NewProvider(
		"test",
		base.API,
		providertypes.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		fetcher,
	)
	require.NoError(t, err)
	managed := &managedProvider{provider: provider}
	oracle := &Runtime{logger: log.NewLogger(logs, log.ColorOption(false))}

	oracle.updateMu.Lock()
	managed.start(context.Background(), nil, oracle.logger)
	oracle.updateMu.Unlock()
	requireSignal(t, started, "provider did not start")

	oracle.updateMu.Lock()
	managed.stop()
	oracle.updateMu.Unlock()

	require.NotContains(t, logs.String(), "provider exited")
}

func TestManagedProviderStartLogsUnexpectedExit(t *testing.T) {
	logs := new(bytes.Buffer)
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()
	fetcher.EXPECT().ResponseBufferSize(gomock.Any()).Return(1)
	fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("fetch failed"))
	provider, err := base.NewProvider(
		"test",
		base.API,
		providertypes.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		fetcher,
	)
	require.NoError(t, err)
	managed := &managedProvider{provider: provider}
	oracle := &Runtime{logger: log.NewLogger(logs, log.ColorOption(false))}

	oracle.updateMu.Lock()
	managed.start(context.Background(), nil, oracle.logger)
	done := managed.done
	oracle.updateMu.Unlock()
	requireSignal(t, done, "provider did not exit")

	oracle.updateMu.Lock()
	managed.stop()
	oracle.updateMu.Unlock()

	require.Contains(t, logs.String(), "provider exited")
	require.Contains(t, logs.String(), "fetch failed")
}
