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
			name: "tax rate is nil",
			mutate: func(gs *types.GenesisState) {
				gs.TaxRate = math.LegacyDec{}
			},
			expectErr: "tax_rate must be set",
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
			name: "reward weight is nil",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeight = math.LegacyDec{}
			},
			expectErr: "reward_weight must be set",
		},
		{
			name: "invalid tax cap denom",
			mutate: func(gs *types.GenesisState) {
				gs.TaxCaps = []types.TaxCap{{Denom: "", TaxCap: math.NewInt(100)}}
			},
			expectErr: "tax cap denom is invalid",
		},
		{
			name: "duplicate tax cap denom",
			mutate: func(gs *types.GenesisState) {
				gs.TaxCaps = []types.TaxCap{
					{Denom: "uusd", TaxCap: math.NewInt(100)},
					{Denom: "uusd", TaxCap: math.NewInt(200)},
				}
			},
			expectErr: "duplicate tax cap for denom uusd",
		},
		{
			name: "tax cap is nil",
			mutate: func(gs *types.GenesisState) {
				gs.TaxCaps = []types.TaxCap{{Denom: "uusd", TaxCap: math.Int{}}}
			},
			expectErr: "tax cap for uusd must be set",
		},
		{
			name: "tax cap is negative",
			mutate: func(gs *types.GenesisState) {
				gs.TaxCaps = []types.TaxCap{{Denom: "uusd", TaxCap: math.NewInt(-1)}}
			},
			expectErr: "tax cap for uusd must be zero or positive",
		},
		{
			name: "invalid epoch tax proceeds",
			mutate: func(gs *types.GenesisState) {
				gs.EpochTaxProceeds = sdk.Coins{{Denom: "uusd", Amount: math.NewInt(-1)}}
			},
			expectErr: "epoch_tax_proceeds must be valid",
		},
		{
			name: "invalid epoch initial issuance",
			mutate: func(gs *types.GenesisState) {
				gs.EpochInitialIssuance = sdk.Coins{{Denom: "uusd", Amount: math.Int{}}}
			},
			expectErr: "epoch_initial_issuance must be valid",
		},
		{
			name: "duplicate epoch state",
			mutate: func(gs *types.GenesisState) {
				gs.EpochStates = []types.EpochState{
					{
						Epoch:             1,
						TaxReward:         math.LegacyOneDec(),
						SeigniorageReward: math.LegacyOneDec(),
						TotalStakedArk:    math.OneInt(),
					},
					{
						Epoch:             1,
						TaxReward:         math.LegacyOneDec(),
						SeigniorageReward: math.LegacyOneDec(),
						TotalStakedArk:    math.OneInt(),
					},
				}
			},
			expectErr: "duplicate epoch state for epoch 1",
		},
		{
			name: "epoch tax reward is nil",
			mutate: func(gs *types.GenesisState) {
				gs.EpochStates = []types.EpochState{{
					Epoch:             1,
					TaxReward:         math.LegacyDec{},
					SeigniorageReward: math.LegacyOneDec(),
					TotalStakedArk:    math.OneInt(),
				}}
			},
			expectErr: "epoch state 1 tax_reward must be set",
		},
		{
			name: "epoch tax reward is negative",
			mutate: func(gs *types.GenesisState) {
				gs.EpochStates = []types.EpochState{{
					Epoch:             1,
					TaxReward:         math.LegacyNewDec(-1),
					SeigniorageReward: math.LegacyOneDec(),
					TotalStakedArk:    math.OneInt(),
				}}
			},
			expectErr: "epoch state 1 tax_reward must be zero or positive",
		},
		{
			name: "epoch seigniorage reward is nil",
			mutate: func(gs *types.GenesisState) {
				gs.EpochStates = []types.EpochState{{
					Epoch:             1,
					TaxReward:         math.LegacyOneDec(),
					SeigniorageReward: math.LegacyDec{},
					TotalStakedArk:    math.OneInt(),
				}}
			},
			expectErr: "epoch state 1 seigniorage_reward must be set",
		},
		{
			name: "epoch seigniorage reward is negative",
			mutate: func(gs *types.GenesisState) {
				gs.EpochStates = []types.EpochState{{
					Epoch:             1,
					TaxReward:         math.LegacyOneDec(),
					SeigniorageReward: math.LegacyNewDec(-1),
					TotalStakedArk:    math.OneInt(),
				}}
			},
			expectErr: "epoch state 1 seigniorage_reward must be zero or positive",
		},
		{
			name: "epoch total staked ark is nil",
			mutate: func(gs *types.GenesisState) {
				gs.EpochStates = []types.EpochState{{
					Epoch:             1,
					TaxReward:         math.LegacyOneDec(),
					SeigniorageReward: math.LegacyOneDec(),
					TotalStakedArk:    math.Int{},
				}}
			},
			expectErr: "epoch state 1 total_staked_ark must be set",
		},
		{
			name: "epoch total staked ark is negative",
			mutate: func(gs *types.GenesisState) {
				gs.EpochStates = []types.EpochState{{
					Epoch:             1,
					TaxReward:         math.LegacyOneDec(),
					SeigniorageReward: math.LegacyOneDec(),
					TotalStakedArk:    math.NewInt(-1),
				}}
			},
			expectErr: "epoch state 1 total_staked_ark must be zero or positive",
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
					[]types.EpochState{{
						Epoch:             1,
						TaxReward:         math.LegacyOneDec(),
						SeigniorageReward: math.LegacyOneDec(),
						TotalStakedArk:    math.OneInt(),
					}},
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
