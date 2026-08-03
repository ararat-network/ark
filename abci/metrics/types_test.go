package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ark/abci/metrics"
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
		{name: "oracle client", got: metrics.StatusOracleClient.String(), want: "OracleClientError"},
		{name: "invalid oracle prices", got: metrics.StatusInvalidOraclePrices.String(), want: "InvalidOraclePricesError"},
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
		{name: "extended commit", got: metrics.ExtendedCommit.String(), want: "extended_commit"},
		{name: "vote extension", got: metrics.VoteExtension.String(), want: "vote_extension"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}
