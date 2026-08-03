package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := types.DefaultGenesisState()
	require.Equal(t, types.DefaultParams(), genesis.Params)
	require.True(t, types.DefaultMonetaryPolicy().Equal(genesis.MonetaryPolicy))
	require.Empty(t, genesis.TaxCaps)
	require.Equal(t, types.DefaultRewardFundingState(), genesis.RewardFunding)
	require.Equal(t, types.DefaultMonetaryMandate(), genesis.MonetaryMandate)
	require.NoError(t, genesis.Validate())
}

func TestNewGenesisStateCopiesSlices(t *testing.T) {
	taxCaps := []types.TaxCap{{Denom: chain.USDBaseDenom, TaxCap: math.OneInt()}}
	genesis := types.NewGenesisState(
		types.DefaultParams(),
		taxCaps,
		types.DefaultRewardFundingState(),
		types.DefaultMonetaryMandate(),
		types.DefaultMonetaryPolicy(),
		false,
	)
	taxCaps[0].Denom = "mutated"
	require.Equal(t, chain.USDBaseDenom, genesis.TaxCaps[0].Denom)
}

func TestGenesisTaxCapValidation(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name: "positive reference cap requires positive derived caps",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.OneInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.OneInt()}}
			},
		},
		// A cap that disagrees with the reference is the expected shape of a
		// kept cap, not a corrupt one: it was derived under whatever the
		// reference was at the time, and nothing re-derives it afterwards.
		{
			name: "positive reference cap permits uncapped sentinel",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.OneInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.ZeroInt()}}
			},
		},
		{
			name: "zero reference cap permits uncapped sentinel",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.ZeroInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.ZeroInt()}}
			},
		},
		{
			name: "zero reference cap permits positive kept cap",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.ZeroInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.OneInt()}}
			},
		},
		{
			name: "sorted tax caps are valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.TaxCaps = []types.TaxCap{
					{Denom: chain.KRWBaseDenom, TaxCap: math.ZeroInt()},
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
				}
			},
		},
		{
			name: "unsorted tax caps",
			mutate: func(genesis *types.GenesisState) {
				genesis.TaxCaps = []types.TaxCap{
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
					{Denom: chain.KRWBaseDenom, TaxCap: math.ZeroInt()},
				}
			},
			expectErr: "genesis tax caps must be sorted by unique denom",
		},
		{
			name: "duplicate tax cap denom",
			mutate: func(genesis *types.GenesisState) {
				genesis.TaxCaps = []types.TaxCap{
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
				}
			},
			expectErr: "genesis tax caps must be sorted by unique denom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := types.DefaultGenesisState()
			tc.mutate(genesis)

			err := genesis.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}
