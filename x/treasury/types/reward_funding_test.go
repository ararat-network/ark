package types_test

import (
	"testing"

	"cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	"ark/x/treasury/types"
)

func TestGenesisRewardFundingValidation(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.RewardFundingState)
		expectErr string
	}{
		{
			name: "valid active window",
			mutate: func(funding *types.RewardFundingState) {
				funding.BlocksRemaining = 2
			},
		},
		{
			name: "unset validator target",
			mutate: func(funding *types.RewardFundingState) {
				funding.ValidatorTarget = math.Int{}
			},
			expectErr: "validator target must be set",
		},
		{
			name: "negative validator fee value",
			mutate: func(funding *types.RewardFundingState) {
				funding.BlocksRemaining = 1
				funding.ValidatorFeeValue = math.NewInt(-1)
			},
			expectErr: "validator fee value must be zero or positive",
		},
		{
			name: "completed window",
			mutate: func(funding *types.RewardFundingState) {
				funding.ValidatorTarget = math.OneInt()
			},
			expectErr: "empty reward funding window must use the default state",
		},
		{
			name: "noncanonical empty window",
			mutate: func(funding *types.RewardFundingState) {
				funding.ValuationComplete = false
			},
			expectErr: "empty reward funding window must use the default state",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := types.DefaultGenesisState()
			tc.mutate(&genesis.RewardFunding)
			err := genesis.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}
