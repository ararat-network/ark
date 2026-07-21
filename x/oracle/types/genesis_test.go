package types_test

import (
	"fmt"
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
		{
			name: "accounting reward window must be positive",
			mutate: func(gs *types.GenesisState) {
				gs.Accounting.RewardWindow = 0
			},
			expectErr: "accounting reward window must be greater than zero",
		},
		{
			name: "accounting reward distribution window cannot be shorter than reward window",
			mutate: func(gs *types.GenesisState) {
				gs.Accounting.RewardDistributionWindow = gs.Accounting.RewardWindow - 1
			},
			expectErr: "accounting reward distribution window must be greater than or equal to reward window",
		},
		{
			name: "accounting slash window must be positive",
			mutate: func(gs *types.GenesisState) {
				gs.Accounting.SlashWindow = 0
			},
			expectErr: "accounting slash window must be greater than zero",
		},
		// ExchangeRates
		{
			name: "exchange rate empty denom",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = types.ExchangeRates{
					{Denom: "", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate denom must be a micro denom beginning with u",
		},
		{
			name: "exchange rate denom must be canonical lowercase",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = types.ExchangeRates{
					{Denom: "uUSD", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "canonical lowercase micro denom",
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
		{
			name: "exchange rate denom must be a vote target",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = types.ExchangeRates{
					{Denom: "ufoo", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate denom ufoo is not a vote target",
		},
		// RewardWeights
		{
			name: "reward weight must be set",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.Int{}},
				}
			},
			expectErr: "reward weight must be set",
		},
		{
			name: "reward weight must not be negative",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(-1)},
				}
			},
			expectErr: "reward weight must not be negative",
		},
		{
			name: "reward weight empty validator address",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: "", RewardWeight: math.NewInt(1)},
				}
			},
			expectErr: "reward weight validator address must not be empty",
		},
		{
			name: "duplicate reward weight",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(2)},
				}
			},
			expectErr: "duplicate reward weight for validator " + validatorAddress,
		},
		{
			name: "reward weight invalid validator address",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: "not-a-validator-address", RewardWeight: math.NewInt(1)},
				}
			},
			expectErr: "reward weight validator address is invalid",
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
		// VoteTargets
		{
			name: "vote target denom must be micro denom",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"u"}
			},
			expectErr: "vote target denom must be a micro denom beginning with u: u",
		},
		{
			name: "vote target denom must be canonical lowercase",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"uUSD"}
			},
			expectErr: "canonical lowercase micro denom",
		},
		{
			name: "vote target denom cannot contain path separators",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"ufoo/bar"}
			},
			expectErr: "canonical lowercase micro denom",
		},
		{
			name: "duplicate vote target",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"uusd", "uusd"}
			},
			expectErr: "duplicate vote target denom uusd",
		},
		{
			name: "maximum vote targets is valid",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = makeTestVoteTargets(types.MaxVoteTargets)
			},
		},
		{
			name: "too many vote targets",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = makeTestVoteTargets(types.MaxVoteTargets + 1)
			},
			expectErr: "exceeds maximum vote targets",
		},
		// Valid custom genesis
		{
			name: "custom valid genesis",
			mutate: func(gs *types.GenesisState) {
				params := types.DefaultParams()
				*gs = *types.NewGenesisState(
					params,
					types.NewAccounting(params),
					types.ExchangeRates{
						{Denom: "uusd", Rate: math.LegacyOneDec()},
					},
					[]types.RewardWeight{
						{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					},
					[]types.MissCount{
						{ValidatorAddress: otherValidatorAddress, MissCount: 0},
					},
					types.VoteTargets{Denoms: []string{"uusd"}},
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

func makeTestVoteTargets(count int) []string {
	denoms := make([]string, count)
	for i := range count {
		denoms[i] = fmt.Sprintf("u%03d", i)
	}
	return denoms
}
