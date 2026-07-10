package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

func TestValidateGenesis(t *testing.T) {
	validatorAddress := sdk.ValAddress([]byte("validator-1-address")).String()
	otherValidatorAddress := sdk.ValAddress([]byte("validator-2-address")).String()

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
			name: "exchange rate is nil",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = types.ExchangeRates{
					{Denom: "uusd", Rate: math.LegacyDec{}},
				}
			},
			expectErr: "exchange rate for uusd must be set",
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
					{ValidatorAddress: validatorAddress, ScoreWeight: 1},
					{ValidatorAddress: validatorAddress, ScoreWeight: 2},
				}
			},
			expectErr: "duplicate score weight for validator " + validatorAddress,
		},
		{
			name: "score weight invalid validator address",
			mutate: func(gs *types.GenesisState) {
				gs.ScoreWeights = []types.ScoreWeight{
					{ValidatorAddress: "not-a-validator-address", ScoreWeight: 1},
				}
			},
			expectErr: "score weight validator address is invalid",
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
					{ValidatorAddress: validatorAddress, MissCount: 1},
					{ValidatorAddress: validatorAddress, MissCount: 2},
				}
			},
			expectErr: "duplicate miss count for validator " + validatorAddress,
		},
		{
			name: "miss count invalid validator address",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: "not-a-validator-address", MissCount: 1},
				}
			},
			expectErr: "miss count validator address is invalid",
		},
		// TobinTaxes
		{
			name: "tobin tax denom must be micro denom",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "u", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				}
			},
			expectErr: "tobin tax denom must be a micro denom beginning with u: u",
		},
		{
			name: "tobin tax denom must be canonical lowercase",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "uUSD", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				}
			},
			expectErr: "canonical lowercase micro denom",
		},
		{
			name: "tobin tax denom cannot contain path separators",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "ufoo/bar", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				}
			},
			expectErr: "canonical lowercase micro denom",
		},
		{
			name: "tobin tax outside valid range",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(101, 2)},
				}
			},
			expectErr: "tobin tax for uusd must be between [0, 1]",
		},
		{
			name: "tobin tax is nil",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "uusd", TobinTax: math.LegacyDec{}},
				}
			},
			expectErr: "tobin tax for uusd must be set",
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
						{ValidatorAddress: validatorAddress, ScoreWeight: 1},
					},
					[]types.MissCount{
						{ValidatorAddress: otherValidatorAddress, MissCount: 0},
					},
					[]types.TobinTax{
						{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
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
