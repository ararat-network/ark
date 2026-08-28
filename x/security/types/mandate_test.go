package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/x/security/types"
)

// Fixtures shared across the types tests.
const testUpgradeName = "v2-emergency"

func testSecurityCommittee() string {
	return authtypes.NewModuleAddress("security-committee").String()
}

// enabledSecurityMandate returns a live appointment.
func enabledSecurityMandate() types.SecurityMandate {
	appointment := types.NewDisabledSecurityMandate(1)
	appointment.Committee = testSecurityCommittee()
	appointment.ActivationHeight = 10
	appointment.ExpiryHeight = 20

	return appointment
}

func TestDefaultSecurityMandate(t *testing.T) {
	appointment := types.DefaultSecurityMandate()

	require.True(t, appointment.IsDisabled())
	require.Zero(t, appointment.Term)
	require.NoError(t, appointment.Validate())
}

func TestSecurityMandateValidate(t *testing.T) {
	testCases := []struct {
		name     string
		mutate   func(*types.SecurityMandate)
		expected string
	}{
		{
			name:   "enabled appointment",
			mutate: func(*types.SecurityMandate) {},
		},
		{
			name: "envelope fault is labelled",
			mutate: func(appointment *types.SecurityMandate) {
				appointment.ExpiryHeight = appointment.ActivationHeight
			},
			expected: "security mandate: activation height must precede expiry height",
		},
		{
			name: "non-canonical committee is labelled",
			mutate: func(appointment *types.SecurityMandate) {
				appointment.Committee = "not-an-address"
			},
			expected: "security mandate: committee is invalid",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			appointment := enabledSecurityMandate()
			testCase.mutate(&appointment)

			err := appointment.Validate()
			if testCase.expected == "" {
				require.NoError(t, err)

				return
			}
			require.ErrorContains(t, err, testCase.expected)
		})
	}
}

func TestSecurityMandateValidateDisabled(t *testing.T) {
	testCases := []struct {
		name     string
		mutate   func(*types.SecurityMandate)
		expected string
	}{
		{
			name:   "canonical disabled",
			mutate: func(*types.SecurityMandate) {},
		},
		{
			name: "disabled at a later term",
			mutate: func(appointment *types.SecurityMandate) {
				appointment.Term = 7
			},
		},
		{
			name: "disabled with a window",
			mutate: func(appointment *types.SecurityMandate) {
				appointment.ActivationHeight = 10
				appointment.ExpiryHeight = 20
			},
			expected: "security mandate: disabled envelope must not have an activation or expiry height",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			appointment := types.DefaultSecurityMandate()
			testCase.mutate(&appointment)

			err := appointment.Validate()
			if testCase.expected == "" {
				require.NoError(t, err)

				return
			}
			require.ErrorContains(t, err, testCase.expected)
		})
	}
}

func TestCommitteePlanMatches(t *testing.T) {
	record := types.CommitteePlan{Name: testUpgradeName, Height: 500, Term: 3}

	require.True(t, record.Matches(testUpgradeName, 500))
	require.False(t, record.Matches(testUpgradeName, 501))
	require.False(t, record.Matches("v2-planned", 500))
	require.False(t, types.CommitteePlan{}.Matches("", 0))
}

func TestCommitteePlanValidate(t *testing.T) {
	testCases := []struct {
		name     string
		record   types.CommitteePlan
		expected string
	}{
		{
			name:   "empty record",
			record: types.CommitteePlan{},
		},
		{
			name:   "populated record",
			record: types.CommitteePlan{Name: testUpgradeName, Height: 500, Term: 3},
		},
		{
			name:     "empty record carrying a term",
			record:   types.CommitteePlan{Term: 3},
			expected: "empty committee plan must not carry a term",
		},
		{
			name:     "height without a name",
			record:   types.CommitteePlan{Height: 500},
			expected: "committee plan must have a name",
		},
		{
			name:     "name without a height",
			record:   types.CommitteePlan{Name: testUpgradeName},
			expected: "committee plan height must be positive",
		},
		{
			name:     "negative height",
			record:   types.CommitteePlan{Name: testUpgradeName, Height: -1},
			expected: "committee plan height must be positive",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.record.Validate()
			if testCase.expected == "" {
				require.NoError(t, err)

				return
			}
			require.ErrorContains(t, err, testCase.expected)
		})
	}
}
