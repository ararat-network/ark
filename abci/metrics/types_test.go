package metrics_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"ark/abci/metrics"
)

func TestStatusFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "success",
			want: "Success",
		},
		{
			name: "generic failure",
			err:  errors.New("failure"),
			want: "Failure",
		},
		{
			name: "labelled failure",
			err:  labelledError{},
			want: "LabelledError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, metrics.StatusFromError(tt.err).Label())
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

type labelledError struct{}

func (labelledError) Error() string {
	return "labelled error"
}

func (labelledError) Label() string {
	return "LabelledError"
}
