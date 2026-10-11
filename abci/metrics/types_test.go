package metrics_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/abci/metrics"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "unset defaults to failure", got: metrics.Status("").String(), want: "Failure"},
		{name: "success", got: metrics.StatusSuccess.String(), want: "Success"},
		{name: "nil request", got: metrics.StatusNilRequest.String(), want: "NilRequestError"},
		{name: "wrapped handler", got: metrics.StatusWrappedHandler.String(), want: "WrappedHandlerError"},
		{name: "codec", got: metrics.StatusCodec.String(), want: "CodecError"},
		{name: "missing commit info", got: metrics.StatusMissingCommitInfo.String(), want: "MissingCommitInfoError"},
		{name: "extended commit validation", got: metrics.StatusExtendedCommitValidation.String(), want: "ExtendedCommitValidationError"},
		{name: "panic", got: metrics.StatusPanic.String(), want: "Panic"},
		{name: "oracle client", got: metrics.StatusPriceFeedClient.String(), want: "PriceFeedClientError"},
		{name: "invalid oracle prices", got: metrics.StatusInvalidPrices.String(), want: "InvalidPricesError"},
		{name: "vote extension validation", got: metrics.StatusVoteExtensionValidation.String(), want: "VoteExtensionValidationError"},
		{name: "oracle keeper", got: metrics.StatusOracleKeeper.String(), want: "OracleKeeperError"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}

func TestTypesString(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "prepare proposal", got: metrics.PrepareProposal.String(), want: "prepare_proposal"},
		{name: "process proposal", got: metrics.ProcessProposal.String(), want: "process_proposal"},
		{name: "extend vote", got: metrics.ExtendVote.String(), want: "extend_vote"},
		{name: "verify vote extension", got: metrics.VerifyVoteExtension.String(), want: "verify_vote_extension"},
		{name: "pre block", got: metrics.PreBlock.String(), want: "pre_blocker"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}

func TestStatusOf(t *testing.T) {
	labelled := metrics.NewError(metrics.StatusCodec, "codec error")
	tests := []struct {
		name string
		err  error
		want metrics.Status
	}{
		{name: "nil is success", err: nil, want: metrics.StatusSuccess},
		{name: "plain error is failure", err: errors.New("boom"), want: metrics.StatusFailure},
		{name: "labelled error", err: labelled, want: metrics.StatusCodec},
		{name: "label survives wrapping", err: fmt.Errorf("decoding: %w", labelled), want: metrics.StatusCodec},
		// The outer error names the stage that failed, so it wins over the one it wraps.
		{name: "outer label wins", err: fmt.Errorf("%w: %w", metrics.NewError(metrics.StatusWrappedHandler, "wrapped"), labelled), want: metrics.StatusWrappedHandler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, metrics.StatusOf(tt.err))
		})
	}
}
