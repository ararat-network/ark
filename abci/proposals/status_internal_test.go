package proposals

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
	abcitestutil "github.com/ararat-network/ark/abci/testutil"
	abcitypes "github.com/ararat-network/ark/abci/types"
	"github.com/ararat-network/ark/pkg/telemetry"
)

func TestProposalStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want abcimetrics.Status
	}{
		{name: "nil is success", err: nil, want: abcimetrics.StatusSuccess},
		{name: "nil request", err: fmt.Errorf("%w for %s", abcitypes.ErrNilRequest, abcimetrics.PrepareProposal), want: abcimetrics.StatusNilRequest},
		{name: "wrapped handler", err: fmt.Errorf("%w for %s: %w", abcitypes.ErrWrappedHandler, abcimetrics.PrepareProposal, errors.New("boom")), want: abcimetrics.StatusWrappedHandler},
		{name: "extended commit validation", err: fmt.Errorf("%w: %w", ErrExtendedCommitValidation, errors.New("boom")), want: abcimetrics.StatusExtendedCommitValidation},
		{name: "codec", err: fmt.Errorf("%w: %w", abcitypes.ErrCodec, errors.New("boom")), want: abcimetrics.StatusCodec},
		{name: "missing commit info", err: abcitypes.ErrMissingCommitInfo, want: abcimetrics.StatusMissingCommitInfo},
		{name: "unknown error is failure", err: errors.New("boom"), want: abcimetrics.StatusFailure},
		// The outer error names the stage that failed, so it wins over the
		// sentinel it wraps.
		{name: "wrapped handler outranks codec", err: fmt.Errorf("%w: %w", abcitypes.ErrWrappedHandler, abcitypes.ErrCodec), want: abcimetrics.StatusWrappedHandler},
		{name: "extended commit validation outranks codec", err: fmt.Errorf("%w: %w", ErrExtendedCommitValidation, abcitypes.ErrCodec), want: abcimetrics.StatusExtendedCommitValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, proposalStatus(tt.err))
		})
	}
}

func TestProposalOutcomeMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	provider, err := telemetry.NewPrometheusProvider("test", registry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	otel.SetMeterProvider(provider)
	count := func(method, status string) float64 {
		families, err := registry.Gather()
		require.NoError(t, err)
		for _, f := range families {
			if f.GetName() == "ark_abci_requests_total" {
				for _, m := range f.Metric {
					labels := map[string]string{}
					for _, l := range m.Label {
						labels[l.GetName()] = l.GetValue()
					}
					if labels["method"] == method && labels["status"] == status {
						return m.GetCounter().GetValue()
					}
				}
			}
		}
		return 0
	}
	cause := errors.New("original panic")
	for _, tc := range []struct {
		name, method, status string
		verdict              cmtabci.ResponseProcessProposal_ProposalStatus
		err                  error
		panics               bool
	}{
		{name: "accepted", method: "process_proposal", status: "Success", verdict: cmtabci.ResponseProcessProposal_ACCEPT},
		{name: "rejected without error", method: "process_proposal", status: "Rejected", verdict: cmtabci.ResponseProcessProposal_REJECT},
		{name: "error outranks rejection", method: "process_proposal", status: "WrappedHandlerError", verdict: cmtabci.ResponseProcessProposal_REJECT, err: errors.New("failure")},
		{name: "process panic", method: "process_proposal", status: "Panic", panics: true},
		{name: "prepare panic", method: "prepare_proposal", status: "Panic", panics: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := count(tc.method, tc.status)
			beforeSuccess := count(tc.method, "Success")
			h := NewHandler(func(sdk.Context, *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				panic(cause)
			},
				func(sdk.Context, *cmtabci.RequestProcessProposal) (*cmtabci.ResponseProcessProposal, error) {
					if tc.panics {
						panic(cause)
					}
					return &cmtabci.ResponseProcessProposal{Status: tc.verdict}, tc.err
				}, nil)
			invoke := func() {
				ctx := abcitestutil.NewSDKContext(1, 2)
				if tc.method == "prepare_proposal" {
					_, _ = h.PrepareProposalHandler()(ctx, &cmtabci.RequestPrepareProposal{Height: 1})
				} else {
					_, _ = h.ProcessProposalHandler()(ctx, &cmtabci.RequestProcessProposal{Height: 1})
				}
			}
			if tc.panics {
				require.PanicsWithValue(t, cause, invoke)
			} else {
				require.NotPanics(t, invoke)
			}
			require.Equal(t, before+1, count(tc.method, tc.status))
			if tc.status != "Success" {
				require.Equal(t, beforeSuccess, count(tc.method, "Success"))
			}
		})
	}
}
