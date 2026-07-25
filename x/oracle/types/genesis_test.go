package types_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
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
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate denom must be an Ark-native base denom beginning with a",
		},
		{
			name: "exchange rate denom must be canonical lowercase",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "aUSD", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "exchange rate not positive",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyZeroDec()},
				}
			},
			expectErr: "exchange rate for ausd must be positive",
		},
		{
			name: "exchange rate is nil",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyDec{}},
				}
			},
			expectErr: "exchange rate for ausd must be set",
		},
		{
			name: "exchange rate is out of range",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{
						Denom: "ausd",
						Rate: math.LegacyNewDecFromBigInt(
							new(big.Int).Lsh(big.NewInt(1), 256),
						),
					},
				}
			},
			expectErr: "exchange rate for ausd must be representable",
		},
		{
			name: "sorted exchange rates are valid",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "akrw", Rate: math.LegacyOneDec()},
					{Denom: "ausd", Rate: math.LegacyNewDec(2)},
				}
			},
		},
		{
			name: "unsorted exchange rates",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyOneDec()},
					{Denom: "akrw", Rate: math.LegacyNewDec(2)},
				}
			},
			expectErr: "genesis exchange rates must be sorted by unique denom",
		},
		{
			name: "duplicate exchange rate denom",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyOneDec()},
					{Denom: "ausd", Rate: math.LegacyNewDec(2)},
				}
			},
			expectErr: "genesis exchange rates must be sorted by unique denom",
		},
		{
			name: "exchange rate denom must be a vote target",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "afoo", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate denom afoo is not a vote target",
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
			name: "sorted reward weights are valid",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					{ValidatorAddress: otherValidatorAddress, RewardWeight: math.NewInt(2)},
				}
			},
		},
		{
			name: "unsorted reward weights",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: otherValidatorAddress, RewardWeight: math.NewInt(1)},
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(2)},
				}
			},
			expectErr: "genesis reward weights must be sorted by unique validator address",
		},
		{
			name: "duplicate reward weight",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(2)},
				}
			},
			expectErr: "genesis reward weights must be sorted by unique validator address",
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
			name: "sorted miss counters are valid",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: validatorAddress, MissCount: 1},
					{ValidatorAddress: otherValidatorAddress, MissCount: 2},
				}
			},
		},
		{
			name: "unsorted miss counters",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: otherValidatorAddress, MissCount: 1},
					{ValidatorAddress: validatorAddress, MissCount: 2},
				}
			},
			expectErr: "genesis miss counts must be sorted by unique validator address",
		},
		{
			name: "duplicate miss counter",
			mutate: func(gs *types.GenesisState) {
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: validatorAddress, MissCount: 1},
					{ValidatorAddress: validatorAddress, MissCount: 2},
				}
			},
			expectErr: "genesis miss counts must be sorted by unique validator address",
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
			name: "vote target denom must be an Ark-native base denom",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"a"}
			},
			expectErr: "active vote targets denom must be an Ark-native base denom beginning with a: a",
		},
		{
			name: "vote target denom must be canonical lowercase",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"aUSD"}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "vote target denom cannot contain path separators",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"afoo/bar"}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "native denom cannot be configured as a vote target",
			mutate: func(gs *types.GenesisState) {
				gs.Params.TobinTaxes = []types.TobinTax{{
					Denom:    chain.NoahBaseDenom,
					TobinTax: math.LegacyNewDecWithPrec(25, 4),
				}}
				gs.VoteTargets = types.NewVoteTargets(gs.Params)
			},
			expectErr: "active vote targets must not contain native denom anoah",
		},
		{
			name: "duplicate vote target",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"ausd", "ausd"}
			},
			expectErr: "active vote targets contains duplicate denom ausd",
		},
		{
			name: "vote targets must be sorted",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = []string{"ausd", "akrw"}
			},
			expectErr: "active vote targets must be sorted",
		},
		{
			name: "maximum vote targets is valid",
			mutate: func(gs *types.GenesisState) {
				gs.VoteTargets.Denoms = makeTestVoteTargets(types.MaxVoteTargets)
				gs.Params.TobinTaxes = tobinTaxesForDenoms(gs.VoteTargets.Denoms)
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
				params.TobinTaxes = []types.TobinTax{{
					Denom:    "ausd",
					TobinTax: math.LegacyZeroDec(),
				}}
				*gs = *types.NewGenesisState(
					params,
					types.NewAccounting(params),
					[]types.ExchangeRate{
						{Denom: "ausd", Rate: math.LegacyOneDec()},
					},
					[]types.RewardWeight{
						{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					},
					[]types.MissCount{
						{ValidatorAddress: otherValidatorAddress, MissCount: 0},
					},
					types.VoteTargets{
						Denoms:  []string{"ausd"},
						Version: types.InitialVoteTargetVersion,
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

func makeTestVoteTargets(count int) []string {
	denoms := make([]string, count)
	for i := range count {
		denoms[i] = fmt.Sprintf("a%03d", i)
	}
	return denoms
}

func tobinTaxesForDenoms(denoms []string) []types.TobinTax {
	tobinTaxes := make([]types.TobinTax, len(denoms))
	for i, denom := range denoms {
		tobinTaxes[i] = types.TobinTax{Denom: denom, TobinTax: math.LegacyZeroDec()}
	}
	return tobinTaxes
}
