package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ark/x/claims/types"
)

// Combining the period bound into one condition means its lower and upper cases
// assert the same message, so the string lives once for the package.
const cancellationPeriodOutOfRange = "ClaimCancellationPeriodBlocks must be between one and"

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{name: "defaults", mutate: func(*types.Params) {}},
		{
			name:   "minimum positive period",
			mutate: func(params *types.Params) { params.ClaimCancellationPeriodBlocks = 1 },
		},
		{
			name:      "zero cancellation period",
			mutate:    func(params *types.Params) { params.ClaimCancellationPeriodBlocks = 0 },
			expectErr: cancellationPeriodOutOfRange,
		},
		{
			name: "cancellation period at the domain cap",
			mutate: func(p *types.Params) {
				p.ClaimCancellationPeriodBlocks = types.MaxClaimCancellationPeriodBlocks
			},
		},
		{
			// Unreachable freezes Insurance: no claim ever leaves the veto
			// window, while the mandate still reads as configured.
			name: "cancellation period above the domain cap",
			mutate: func(p *types.Params) {
				p.ClaimCancellationPeriodBlocks = types.MaxClaimCancellationPeriodBlocks + 1
			},
			expectErr: cancellationPeriodOutOfRange,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			tc.mutate(&params)
			err := params.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}
