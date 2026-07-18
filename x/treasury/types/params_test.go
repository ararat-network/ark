package types_test

import (
	"testing"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"ark/x/treasury/types"
)

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{name: "default is valid", mutate: func(*types.Params) {}},
		{
			name:      "zero reward funding window",
			mutate:    func(p *types.Params) { p.RewardFundingWindow = 0 },
			expectErr: "RewardFundingWindow must be positive",
		},
		{
			name:      "reference cap denom must be canonical micro denom",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Denom = "USDR" },
			expectErr: "ReferenceTaxCap denom is invalid",
		},
		{
			name:      "reference cap denom cannot be ibc path",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Denom = "ufoo/bar" },
			expectErr: "ReferenceTaxCap denom is invalid",
		},
		{
			name:      "reference cap must be set",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap = sdk.Coin{} },
			expectErr: "ReferenceTaxCap is invalid",
		},
		{
			name:      "reference cap cannot be negative",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Amount = math.NewInt(-1) },
			expectErr: "ReferenceTaxCap is invalid",
		},
		{
			name:      "reference cap must be positive",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Amount = math.ZeroInt() },
			expectErr: "ReferenceTaxCap must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			tc.mutate(&params)

			err := params.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}
