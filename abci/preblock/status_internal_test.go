package preblock

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.uber.org/mock/gomock"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
	abcitestutil "github.com/ararat-network/ark/abci/testutil"
	abcitypes "github.com/ararat-network/ark/abci/types"
	"github.com/ararat-network/ark/pkg/telemetry"
)

func TestPreblockStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want abcimetrics.Status
	}{
		{name: "nil is success", err: nil, want: abcimetrics.StatusSuccess},
		{name: "nil request", err: fmt.Errorf("%w for %s", abcitypes.ErrNilRequest, abcimetrics.PreBlock), want: abcimetrics.StatusNilRequest},
		{name: "wrapped handler", err: fmt.Errorf("%w for %s: %w", abcitypes.ErrWrappedHandler, abcimetrics.PreBlock, errors.New("boom")), want: abcimetrics.StatusWrappedHandler},
		{name: "oracle keeper", err: fmt.Errorf("%w: %w", abcitypes.ErrOracleKeeper, errors.New("boom")), want: abcimetrics.StatusOracleKeeper},
		{name: "codec", err: fmt.Errorf("%w: %w", abcitypes.ErrCodec, errors.New("boom")), want: abcimetrics.StatusCodec},
		{name: "missing commit info", err: abcitypes.ErrMissingCommitInfo, want: abcimetrics.StatusMissingCommitInfo},
		{name: "unknown error is failure", err: errors.New("boom"), want: abcimetrics.StatusFailure},
		// The outer error names the stage that failed, so it wins over the
		// sentinel it wraps.
		{name: "wrapped handler outranks codec", err: fmt.Errorf("%w: %w", abcitypes.ErrWrappedHandler, abcitypes.ErrCodec), want: abcimetrics.StatusWrappedHandler},
		{name: "oracle keeper outranks codec", err: fmt.Errorf("%w: %w", abcitypes.ErrOracleKeeper, abcitypes.ErrCodec), want: abcimetrics.StatusOracleKeeper},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, preblockStatus(tt.err))
		})
	}
}

func TestPreblockPanicMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	provider, err := telemetry.NewPrometheusProvider("test", registry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	otel.SetMeterProvider(provider)
	for _, tc := range []struct {
		name string
		mode sdk.ExecMode
		want float64
	}{
		{"non finalise", sdk.ExecModePrepareProposal, 0}, {"finalise", sdk.ExecModeFinalize, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keeper := abcitestutil.NewMockOracleKeeper(gomock.NewController(t))
			cause := errors.New("original panic")
			keeper.EXPECT().AdvanceFeeds(gomock.Any()).DoAndReturn(func(context.Context) error { panic(cause) })
			handler := NewHandler(keeper).WrappedPreBlocker(&module.Manager{})
			require.PanicsWithValue(t, cause, func() {
				_, _ = handler(abcitestutil.NewSDKContext(1, 2, tc.mode), &cmtabci.RequestFinalizeBlock{Height: 1})
			})
			families, err := registry.Gather()
			require.NoError(t, err)
			var count float64
			for _, f := range families {
				if f.GetName() == "ark_abci_requests_total" {
					for _, m := range f.Metric {
						for _, l := range m.Label {
							if l.GetName() == "status" {
								require.Equal(t, "Panic", l.GetValue())
							}
						}
						count += m.GetCounter().GetValue()
					}
				}
			}
			require.Equal(t, tc.want, count)
		})
	}
}
