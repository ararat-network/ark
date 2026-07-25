package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
)

func TestVoteTargetsAtHeight(t *testing.T) {
	targets := oracletypes.VoteTargets{
		Denoms:  []string{"akrw", "ausd"},
		Version: oracletypes.InitialVoteTargetVersion,
		Pending: &oracletypes.PendingVoteTargets{
			Denoms:               []string{"aeur", "ausd"},
			Version:              oracletypes.InitialVoteTargetVersion + 1,
			ActivationVoteHeight: 12,
		},
	}

	testCases := []struct {
		name            string
		voteHeight      int64
		expectedVersion uint64
		expectedDenoms  []string
	}{
		{
			name:            "height before activation uses active epoch",
			voteHeight:      11,
			expectedVersion: oracletypes.InitialVoteTargetVersion,
			expectedDenoms:  []string{"akrw", "ausd"},
		},
		{
			name:            "activation height uses pending epoch",
			voteHeight:      12,
			expectedVersion: oracletypes.InitialVoteTargetVersion + 1,
			expectedDenoms:  []string{"aeur", "ausd"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := targets.AtHeight(tc.voteHeight)
			require.Equal(t, tc.expectedVersion, actual.Version)
			require.Equal(t, tc.expectedDenoms, actual.Denoms)

			actual.Denoms[0] = "amutated"
			require.NotContains(t, targets.Denoms, "amutated")
			require.NotContains(t, targets.Pending.Denoms, "amutated")
		})
	}
}

func TestVoteTargetsValidate(t *testing.T) {
	valid := func() oracletypes.VoteTargets {
		return oracletypes.VoteTargets{
			Denoms:  []string{"ausd"},
			Version: oracletypes.InitialVoteTargetVersion,
			Pending: &oracletypes.PendingVoteTargets{
				Denoms:               []string{"akrw", "ausd"},
				Version:              oracletypes.InitialVoteTargetVersion + 1,
				ActivationVoteHeight: 12,
			},
		}
	}

	testCases := []struct {
		name          string
		mutate        func(*oracletypes.VoteTargets)
		expectedError string
	}{
		{name: "valid transition"},
		{
			name: "zero active version is rejected",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Version = 0
			},
			expectedError: "version must be positive",
		},
		{
			name: "pending version must follow active version",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Pending.Version++
			},
			expectedError: "must follow active version",
		},
		{
			name: "activation height must be positive",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Pending.ActivationVoteHeight = 0
			},
			expectedError: "activation height must be positive",
		},
		{
			name: "pending targets must change",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Pending.Denoms = []string{"ausd"}
			},
			expectedError: "must differ",
		},
		{
			name: "unsorted active targets are rejected",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Denoms = []string{"ausd", "akrw"}
			},
			expectedError: "active vote targets must be sorted",
		},
		{
			name: "unsorted pending targets are rejected",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Pending.Denoms = []string{"ausd", "akrw"}
			},
			expectedError: "pending vote targets must be sorted",
		},
		{
			name: "native active target is rejected",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Denoms = []string{chain.NoahBaseDenom}
			},
			expectedError: "active vote targets must not contain native denom anoah",
		},
		{
			name: "native pending target is rejected",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Pending.Denoms = []string{chain.NoahBaseDenom}
			},
			expectedError: "pending vote targets must not contain native denom anoah",
		},
		{
			name: "duplicate pending denom is rejected",
			mutate: func(targets *oracletypes.VoteTargets) {
				targets.Pending.Denoms = []string{"akrw", "akrw"}
			},
			expectedError: "duplicate denom",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			targets := valid()
			if tc.mutate != nil {
				tc.mutate(&targets)
			}

			err := targets.Validate()
			if tc.expectedError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectedError)
		})
	}
}
