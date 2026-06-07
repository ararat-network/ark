package keeper_test

import (
	"time"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestInitGenesis() {
	blockTime := time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name      string
		genesis   func() *types.GenesisState
		expected  func() *types.GenesisState
		setup     func()
		expectErr string
	}{
		{
			name: "full genesis stores all collections",
			genesis: func() *types.GenesisState {
				return &types.GenesisState{
					Params: types.DefaultParams(),
					ExchangeRates: []types.ExchangeRate{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000), BlockTimestamp: blockTime, BlockHeight: 10},
						{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDecWithPrec(123, 2), BlockTimestamp: blockTime, BlockHeight: 11},
					},
					ScoreWeights: []types.ScoreWeight{
						{ValidatorAddress: valAddr1.String(), ScoreWeight: 5},
						{ValidatorAddress: valAddr2.String(), ScoreWeight: 0},
					},
					MissCounts: []types.MissCount{
						{ValidatorAddress: valAddr1.String(), MissCount: 5},
						{ValidatorAddress: valAddr2.String(), MissCount: 0},
					},
				}
			},
		},
		{
			name: "invalid validator address in score weight",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.ScoreWeights = []types.ScoreWeight{
					{ValidatorAddress: "invalid", ScoreWeight: 5},
				}
				return gs
			},
			expectErr: "parsing score weight validator address",
		},
		{
			name: "invalid validator address in miss count",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.MissCounts = []types.MissCount{
					{ValidatorAddress: "invalid", MissCount: 5},
				}
				return gs
			},
			expectErr: "parsing miss count validator address",
		},
		{
			name:    "nil module account returns error",
			genesis: types.DefaultGenesisState,
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
			},
			expectErr: "module account has not been set",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			} else if tc.expectErr == "" {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					authtypes.NewEmptyModuleAccount(types.ModuleName),
				)
			}

			genesis := tc.genesis()
			expected := genesis
			if tc.expected != nil {
				expected = tc.expected()
			}

			err := s.keeper.InitGenesis(s.ctx, genesis)
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
			} else {
				s.Require().NoError(err)
				s.requireGenesisState(expected)
			}
		})
	}
}

func (s *KeeperTestSuite) requireGenesisState(expected *types.GenesisState) {
	// Params are stored as module params; compare TobinTaxes by contents so nil and empty slices are equivalent.
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(expected.Params.VoteThreshold.Equal(params.VoteThreshold))
	s.Require().True(expected.Params.RewardBand.Equal(params.RewardBand))
	s.Require().Equal(expected.Params.RewardWindow, params.RewardWindow)
	s.Require().Equal(expected.Params.RewardDistributionWindow, params.RewardDistributionWindow)
	s.Require().Equal(expected.Params.SlashWindow, params.SlashWindow)
	s.Require().True(expected.Params.SlashFraction.Equal(params.SlashFraction))
	s.Require().True(expected.Params.MinValidPerWindow.Equal(params.MinValidPerWindow))
	s.Require().Equal(expected.Params.MaxExchangeRateAge, params.MaxExchangeRateAge)
	s.Require().Len(params.TobinTaxes, len(expected.Params.TobinTaxes))
	for i, item := range expected.Params.TobinTaxes {
		s.Require().Equal(item.Denom, params.TobinTaxes[i].Denom)
		s.Require().True(item.TobinTax.Equal(params.TobinTaxes[i].TobinTax))
	}

	// Exchange rates are keyed by denom.
	exchangeRateCount := 0
	err = s.keeper.ExchangeRate.Walk(s.ctx, nil, func(_ string, _ types.ExchangeRate) (bool, error) {
		exchangeRateCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.ExchangeRates, exchangeRateCount)

	for _, item := range expected.ExchangeRates {
		exchangeRate, err := s.keeper.ExchangeRate.Get(s.ctx, item.Denom)
		s.Require().NoError(err)
		s.Require().Equal(item.Denom, exchangeRate.Denom)
		s.Require().True(item.Rate.Equal(exchangeRate.Rate), "expected %s for %s, got %s", item.Rate, item.Denom, exchangeRate.Rate)
		s.Require().True(item.BlockTimestamp.Equal(exchangeRate.BlockTimestamp))
		s.Require().Equal(item.BlockHeight, exchangeRate.BlockHeight)
	}

	// Score weights are keyed by validator address.
	scoreWeightCount := 0
	err = s.keeper.ScoreWeight.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ uint64) (bool, error) {
		scoreWeightCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.ScoreWeights, scoreWeightCount)

	for _, item := range expected.ScoreWeights {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		scoreWeight, err := s.keeper.ScoreWeight.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().Equal(item.ScoreWeight, scoreWeight)
	}

	// Miss counts are keyed by validator address.
	missCountCount := 0
	err = s.keeper.MissCount.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ uint64) (bool, error) {
		missCountCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.MissCounts, missCountCount)

	for _, item := range expected.MissCounts {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().Equal(item.MissCount, missCount)
	}
}

func (s *KeeperTestSuite) TestExportGenesis() {
	blockTime := time.Unix(1_700_000_000, 0).UTC()

	expected := &types.GenesisState{
		Params: types.DefaultParams(),
		ExchangeRates: []types.ExchangeRate{
			{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000), BlockTimestamp: blockTime, BlockHeight: 10},
			{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDecWithPrec(123, 2), BlockTimestamp: blockTime, BlockHeight: 11},
		},
		ScoreWeights: []types.ScoreWeight{
			{ValidatorAddress: valAddr1.String(), ScoreWeight: 5},
			{ValidatorAddress: valAddr2.String(), ScoreWeight: 0},
		},
		MissCounts: []types.MissCount{
			{ValidatorAddress: valAddr1.String(), MissCount: 5},
			{ValidatorAddress: valAddr2.String(), MissCount: 0},
		},
	}
	expected.Params.RewardWindow = 10
	expected.Params.VoteThreshold = math.LegacyNewDecWithPrec(6, 1)

	s.Require().NoError(s.keeper.Params.Set(s.ctx, expected.Params))
	for _, item := range expected.ExchangeRates {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, item.Denom, item))
	}
	for _, item := range expected.ScoreWeights {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr, item.ScoreWeight))
	}
	for _, item := range expected.MissCounts {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr, item.MissCount))
	}

	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NotNil(gs)

	// Params are exported from the Params collection.
	s.Require().True(expected.Params.VoteThreshold.Equal(gs.Params.VoteThreshold))
	s.Require().True(expected.Params.RewardBand.Equal(gs.Params.RewardBand))
	s.Require().Equal(expected.Params.RewardWindow, gs.Params.RewardWindow)
	s.Require().Equal(expected.Params.RewardDistributionWindow, gs.Params.RewardDistributionWindow)
	s.Require().Equal(expected.Params.SlashWindow, gs.Params.SlashWindow)
	s.Require().True(expected.Params.SlashFraction.Equal(gs.Params.SlashFraction))
	s.Require().True(expected.Params.MinValidPerWindow.Equal(gs.Params.MinValidPerWindow))
	s.Require().Equal(expected.Params.MaxExchangeRateAge, gs.Params.MaxExchangeRateAge)
	s.Require().Equal(expected.Params.TobinTaxes, gs.Params.TobinTaxes)

	// Exchange rates are exported by denom.
	s.Require().Len(gs.ExchangeRates, len(expected.ExchangeRates))
	exchangeRates := make(map[string]types.ExchangeRate)
	for _, item := range gs.ExchangeRates {
		exchangeRates[item.Denom] = item
	}
	for _, item := range expected.ExchangeRates {
		exchangeRate := exchangeRates[item.Denom]
		s.Require().True(item.Rate.Equal(exchangeRate.Rate))
		s.Require().True(item.BlockTimestamp.Equal(exchangeRate.BlockTimestamp))
		s.Require().Equal(item.BlockHeight, exchangeRate.BlockHeight)
	}

	// Score weights are exported by validator address.
	s.Require().Len(gs.ScoreWeights, len(expected.ScoreWeights))
	scoreWeights := make(map[string]uint64)
	for _, item := range gs.ScoreWeights {
		scoreWeights[item.ValidatorAddress] = item.ScoreWeight
	}
	for _, item := range expected.ScoreWeights {
		s.Require().Equal(item.ScoreWeight, scoreWeights[item.ValidatorAddress])
	}

	// Miss counts are exported by validator address.
	s.Require().Len(gs.MissCounts, len(expected.MissCounts))
	missCounts := make(map[string]uint64)
	for _, item := range gs.MissCounts {
		missCounts[item.ValidatorAddress] = item.MissCount
	}
	for _, item := range expected.MissCounts {
		s.Require().Equal(item.MissCount, missCounts[item.ValidatorAddress])
	}
}
