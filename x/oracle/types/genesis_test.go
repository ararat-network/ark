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
		// FeederDelegations
		{
			name: "feeder delegation empty validator address",
			mutate: func(gs *types.GenesisState) {
				gs.FeederDelegations = []types.FeederDelegation{
					{ValidatorAddress: "", FeederAddress: "noah1abc"},
				}
			},
			expectErr: "validator address must not be empty",
		},
		{
			name: "feeder delegation empty feeder address",
			mutate: func(gs *types.GenesisState) {
				gs.FeederDelegations = []types.FeederDelegation{
					{ValidatorAddress: "noahvaloper1abc", FeederAddress: ""},
				}
			},
			expectErr: "feeder address must not be empty",
		},
		{
			name: "duplicate feeder delegation",
			mutate: func(gs *types.GenesisState) {
				gs.FeederDelegations = []types.FeederDelegation{
					{ValidatorAddress: "noahvaloper1abc", FeederAddress: "noah1abc"},
					{ValidatorAddress: "noahvaloper1abc", FeederAddress: "noah1def"},
				}
			},
			expectErr: "duplicate feeder delegation for validator",
		},
		// MissCounts
		{
			name: "miss counter empty validator address",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: "", MissCount: 1},
				}
			},
			expectErr: "validator address must not be empty",
		},
		{
			name: "duplicate miss counter",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: "noahvaloper1abc", MissCount: 1},
					{ValidatorAddress: "noahvaloper1abc", MissCount: 2},
				}
			},
			expectErr: "duplicate miss counter for validator",
		},
		// AggregateExchangeRatePrevotes
		{
			name: "prevote empty voter",
			mutate: func(gs *types.GenesisState) {
				gs.Prevotes = []types.Prevote{
					{Voter: "", Hash: "abc", SubmitBlock: 1},
				}
			},
			expectErr: "prevote voter must not be empty",
		},
		{
			name: "duplicate prevote voter",
			mutate: func(gs *types.GenesisState) {
				gs.Prevotes = []types.Prevote{
					{Voter: "noahvaloper1abc", Hash: "abc", SubmitBlock: 1},
					{Voter: "noahvaloper1abc", Hash: "def", SubmitBlock: 2},
				}
			},
			expectErr: "duplicate aggregate prevote for voter",
		},
		// AggregateExchangeRateVotes
		{
			name: "vote empty voter",
			mutate: func(gs *types.GenesisState) {
				gs.Votes = []types.Vote{
					{Voter: ""},
				}
			},
			expectErr: "vote voter must not be empty",
		},
		{
			name: "duplicate vote voter",
			mutate: func(gs *types.GenesisState) {
				gs.Votes = []types.Vote{
					{Voter: "noahvaloper1abc"},
					{Voter: "noahvaloper1abc"},
				}
			},
			expectErr: "duplicate aggregate vote for voter",
		},
		// TobinTaxes
		{
			name: "tobin tax empty denom",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				}
			},
			expectErr: "tobin tax denom must not be empty",
		},
		{
			name: "tobin tax negative",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "uusd", TobinTax: math.LegacyNewDec(-1)},
				}
			},
			expectErr: "tobin tax for uusd must be between [0, 1]",
		},
		{
			name: "tobin tax above one",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(101, 2)},
				}
			},
			expectErr: "tobin tax for uusd must be between [0, 1]",
		},
		{
			name: "duplicate tobin tax denom",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(50, 4)},
				}
			},
			expectErr: "duplicate tobin tax for denom uusd",
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
					[]types.FeederDelegation{
						{ValidatorAddress: "noahvaloper1abc", FeederAddress: "noah1abc"},
					},
					[]types.MissCount{
						{ValidatorAddress: "noahvaloper1abc", MissCount: 0},
					},
					[]types.Prevote{},
					[]types.Vote{},
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
