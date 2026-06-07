package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"noah/x/oracle/types"
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
		// ExchangeRates
		{
			name: "exchange rate empty denom",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = types.ExchangeRates{
					{Denom: "", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate denom must not be empty",
		},
		{
			name: "exchange rate not positive",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = types.ExchangeRates{
					{Denom: "uusd", Rate: math.LegacyZeroDec()},
				}
			},
			expectErr: "exchange rate for uusd must be positive",
		},
		{
			name: "duplicate exchange rate denom",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = types.ExchangeRates{
					{Denom: "uusd", Rate: math.LegacyOneDec()},
					{Denom: "uusd", Rate: math.LegacyNewDec(2)},
				}
			},
			expectErr: "duplicate exchange rate for denom uusd",
		},
		// ScoreWeights
		{
			name: "score weight empty validator address",
			mutate: func(gs *types.GenesisState) {
				gs.ScoreWeights = []types.ScoreWeight{
					{ValidatorAddress: "", ScoreWeight: 1},
				}
			},
			expectErr: "score weight validator address must not be empty",
		},
		{
			name: "duplicate score weight",
			mutate: func(gs *types.GenesisState) {
				gs.ScoreWeights = []types.ScoreWeight{
					{ValidatorAddress: "noahvaloper1abc", ScoreWeight: 1},
					{ValidatorAddress: "noahvaloper1abc", ScoreWeight: 2},
				}
			},
			expectErr: "duplicate score weight for validator noahvaloper1abc",
		},
		// MissCounts
		{
			name: "miss counter empty validator address",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: "", MissCount: 1},
				}
			},
			expectErr: "miss count validator address must not be empty",
		},
		{
			name: "duplicate miss counter",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: "noahvaloper1abc", MissCount: 1},
					{ValidatorAddress: "noahvaloper1abc", MissCount: 2},
				}
			},
			expectErr: "duplicate miss count for validator noahvaloper1abc",
		},
		// Valid custom genesis
		{
			name: "custom valid genesis",
			mutate: func(gs *types.GenesisState) {
				*gs = *types.NewGenesisState(
					types.DefaultParams(),
					types.ExchangeRates{
						{Denom: "uusd", Rate: math.LegacyOneDec()},
					},
					[]types.ScoreWeight{
						{ValidatorAddress: "noahvaloper1abc", ScoreWeight: 1},
					},
					[]types.MissCount{
						{ValidatorAddress: "noahvaloper1abc", MissCount: 0},
					},
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
