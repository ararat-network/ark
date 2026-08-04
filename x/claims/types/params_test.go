package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ark/x/claims/types"
)

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
			expectErr: "ClaimCancellationPeriodBlocks must be positive",
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
