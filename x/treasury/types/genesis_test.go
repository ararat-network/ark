package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/treasury/types"
)

func TestValidateGenesis(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name:   "default is valid",
			mutate: func(gs *types.GenesisState) {},
		},
		{
			name: "tax rate below min",
			mutate: func(gs *types.GenesisState) {
				gs.TaxRate = math.LegacyNewDecWithPrec(1, 5) // 0.001%
			},
			expectErr: "tax_rate must be less than RateMax",
		},
		{
			name: "tax rate above max",
			mutate: func(gs *types.GenesisState) {
				gs.TaxRate = math.LegacyNewDecWithPrec(5, 1) // 50%
			},
			expectErr: "tax_rate must be less than RateMax",
		},
		{
			name: "reward weight below min",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeight = math.LegacyNewDecWithPrec(1, 2) // 1%
			},
			expectErr: "reward_weight must be less than WeightMax",
		},
		{
			name: "reward weight above max",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeight = math.LegacyNewDecWithPrec(60, 2) // 60%
			},
			expectErr: "reward_weight must be less than WeightMax",
		},
		{
			name: "custom valid genesis",
			mutate: func(gs *types.GenesisState) {
				*gs = *types.NewGenesisState(
					types.DefaultParams(),
					math.LegacyNewDecWithPrec(5, 3),  // 0.5%
					math.LegacyNewDecWithPrec(10, 2), // 10%
					[]types.TaxCap{{Denom: "uusd", TaxCap: math.NewInt(1000000)}},
					sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(5000))),
					sdk.NewCoins(),
					[]types.EpochState{},
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := types.DefaultGenesisState()
			tc.mutate(gs)
			err := gs.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}
