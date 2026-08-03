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
			// Zero would divide by zero in the modular cadence check.
			name:      "zero tax cap refresh period",
			mutate:    func(p *types.Params) { p.TaxCapRefreshPeriodBlocks = 0 },
			expectErr: "TaxCapRefreshPeriodBlocks must be positive",
		},
		{
			name:   "single-block tax cap refresh period is valid",
			mutate: func(p *types.Params) { p.TaxCapRefreshPeriodBlocks = 1 },
		},
		{
			name:      "reference cap denom must be canonical micro denom",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Denom = "USDR" },
			expectErr: "ReferenceTaxCap denom is invalid",
		},
		{
			name:      "reference cap denom cannot be ibc path",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Denom = "afoo/bar" },
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
			name:   "zero reference cap is uncapped",
			mutate: func(p *types.Params) { p.ReferenceTaxCap.Amount = math.ZeroInt() },
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
