package types_test

import (
	"math/big"
	"testing"

	"cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	"ark/x/treasury/types"
)

func TestValidateRewardTargetCapacity(t *testing.T) {
	maxInt := maxRepresentableInt()
	halfMax := maxInt.QuoRaw(2)

	tests := []struct {
		name              string
		window            uint64
		validatorTarget   math.Int
		oracleTarget      math.Int
		blocksRemaining   uint64
		validatorPerBlock math.Int
		oraclePerBlock    math.Int
		expectErr         bool
	}{
		{
			name:              "projected total exactly fits",
			window:            2,
			validatorTarget:   math.OneInt(),
			oracleTarget:      math.ZeroInt(),
			blocksRemaining:   2,
			validatorPerBlock: halfMax,
			oraclePerBlock:    math.ZeroInt(),
		},
		{
			name:              "full window exceeds capacity",
			window:            2,
			validatorTarget:   math.ZeroInt(),
			oracleTarget:      math.ZeroInt(),
			blocksRemaining:   2,
			validatorPerBlock: halfMax.AddRaw(1),
			oraclePerBlock:    math.ZeroInt(),
			expectErr:         true,
		},
		{
			name:              "accumulated targets exceed capacity",
			window:            1,
			validatorTarget:   maxInt,
			oracleTarget:      math.OneInt(),
			validatorPerBlock: math.ZeroInt(),
			oraclePerBlock:    math.ZeroInt(),
			expectErr:         true,
		},
		{
			name:              "active window projection exceeds capacity",
			window:            1,
			validatorTarget:   halfMax,
			oracleTarget:      math.ZeroInt(),
			blocksRemaining:   1,
			validatorPerBlock: maxInt,
			oraclePerBlock:    math.ZeroInt(),
			expectErr:         true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			params.RewardFundingWindow = tc.window
			policy := types.DefaultMonetaryPolicy()
			policy.ValidatorBlockRewardTarget = tc.validatorPerBlock
			policy.OracleBlockRewardTarget = tc.oraclePerBlock

			funding := types.RewardFundingState{
				BlocksRemaining: tc.blocksRemaining,
				ValidatorTarget: tc.validatorTarget,
				OracleTarget:    tc.oracleTarget,
			}
			err := types.ValidateRewardTargetCapacity(params, funding, policy)
			if tc.expectErr {
				require.ErrorContains(t, err, "reward target capacity exceeded")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGenesisRewardTargetCapacity(t *testing.T) {
	maxInt := maxRepresentableInt()
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr bool
	}{
		{
			name: "complete window exactly fits",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.RewardFundingWindow = 1
				genesis.MonetaryPolicy.ValidatorBlockRewardTarget = maxInt
			},
		},
		{
			name: "complete window exceeds capacity",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.RewardFundingWindow = 2
				genesis.MonetaryPolicy.ValidatorBlockRewardTarget = maxInt
			},
			expectErr: true,
		},
		{
			name: "active window projection exceeds capacity",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.RewardFundingWindow = 1
				genesis.MonetaryPolicy.ValidatorBlockRewardTarget = maxInt
				genesis.RewardFunding.BlocksRemaining = 1
				genesis.RewardFunding.ValidatorTarget = math.OneInt()
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := types.DefaultGenesisState()
			tc.mutate(genesis)

			err := genesis.Validate()
			if tc.expectErr {
				require.ErrorContains(t, err, "reward target capacity exceeded")
				return
			}
			require.NoError(t, err)
		})
	}
}

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
				funding.ValidatorFeeValue = math.OneInt()
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

func maxRepresentableInt() math.Int {
	max := new(big.Int).Lsh(big.NewInt(1), math.MaxBitLen)
	return math.NewIntFromBigInt(max.Sub(max, big.NewInt(1)))
}
