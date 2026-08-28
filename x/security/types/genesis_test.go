package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/x/security/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesisState := types.DefaultGenesisState()

	require.True(t, genesisState.SecurityMandate.IsDisabled())
	require.True(t, genesisState.CommitteePlan.IsZero())
	require.NoError(t, genesisState.Validate())
}

func TestGenesisStateValidate(t *testing.T) {
	testCases := []struct {
		name     string
		mutate   func(*types.GenesisState)
		expected string
	}{
		{
			name:   "default",
			mutate: func(*types.GenesisState) {},
		},
		{
			name: "enabled mandate",
			mutate: func(genesisState *types.GenesisState) {
				genesisState.SecurityMandate = enabledSecurityMandate()
			},
		},
		{
			// The plan record describes state in x/upgrade, which outlives any
			// appointment, so a chain exported between a committee scheduling an
			// upgrade and its mandate lapsing carries exactly this pair.
			name: "recorded plan under a disabled mandate",
			mutate: func(genesisState *types.GenesisState) {
				genesisState.CommitteePlan = types.CommitteePlan{
					Name:   testUpgradeName,
					Height: 500,
					Term:   3,
				}
			},
		},
		{
			name: "invalid mandate",
			mutate: func(genesisState *types.GenesisState) {
				appointment := enabledSecurityMandate()
				appointment.ExpiryHeight = appointment.ActivationHeight
				genesisState.SecurityMandate = appointment
			},
			expected: "security mandate: activation height must precede expiry height",
		},
		{
			name: "half-populated plan record",
			mutate: func(genesisState *types.GenesisState) {
				genesisState.CommitteePlan = types.CommitteePlan{Height: 500}
			},
			expected: "invalid committee plan: committee plan must have a name",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			genesisState := types.DefaultGenesisState()
			testCase.mutate(genesisState)

			err := genesisState.Validate()
			if testCase.expected == "" {
				require.NoError(t, err)

				return
			}
			require.ErrorContains(t, err, testCase.expected)
		})
	}
}
