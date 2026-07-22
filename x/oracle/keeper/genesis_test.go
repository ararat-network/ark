package keeper_test

import (
	"time"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestInitGenesis() {
	blockTime := time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name                string
		genesis             func() *types.GenesisState
		expected            func() *types.GenesisState
		setup               func()
		expectErr           string
		expectNoRate        string
		expectAccountingOld bool
	}{
		{
			name: "full genesis stores all collections",
			genesis: func() *types.GenesisState {
				params := types.DefaultParams()
				params.TobinTaxes = []types.TobinTax{
					{Denom: chain.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(1, 2)},
				}
				return &types.GenesisState{
					Params:     params,
					Accounting: types.NewAccounting(params),
					ExchangeRates: []types.ExchangeRate{
						{Denom: chain.MicroKRWDenom, Rate: math.LegacyNewDec(1000), BlockTimestamp: blockTime, BlockHeight: 10},
						{Denom: chain.MicroUSDDenom, Rate: math.LegacyNewDecWithPrec(123, 2), BlockTimestamp: blockTime, BlockHeight: 11},
					},
					RewardWeights: []types.RewardWeight{
						{ValidatorAddress: valAddr1.String(), RewardWeight: math.NewInt(5)},
						{ValidatorAddress: valAddr2.String(), RewardWeight: math.ZeroInt()},
					},
					MissCounts: []types.MissCount{
						{ValidatorAddress: valAddr1.String(), MissCount: 5},
						{ValidatorAddress: valAddr2.String(), MissCount: 0},
					},
					VoteTargets: types.VoteTargets{
						Denoms: []string{
							chain.MicroKRWDenom,
							chain.MicroUSDDenom,
						},
						Version: types.InitialVoteTargetVersion,
					},
				}
			},
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					authtypes.NewEmptyModuleAccount(types.ModuleName),
				)
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, chain.MicroKRWDenom).Return(banktypes.Metadata{}, false)
				s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any())
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, chain.MicroUSDDenom).Return(banktypes.Metadata{}, false)
				s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any())
			},
		},
		{
			name: "invalid validator address in reward weight",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Accounting.RewardWindow = 100
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: "invalid", RewardWeight: math.NewInt(5)},
				}
				return gs
			},
			expectErr:           "invalid oracle genesis state: reward weight validator address is invalid",
			expectAccountingOld: true,
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
			expectErr: "invalid oracle genesis state: miss count validator address is invalid",
		},
		{
			name:      "nil genesis returns error",
			genesis:   func() *types.GenesisState { return nil },
			expectErr: "oracle genesis state is nil",
		},
		{
			name: "unsorted vote targets return error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.VoteTargets.Denoms = []string{chain.MicroUSDDenom, chain.MicroKRWDenom}
				return gs
			},
			expectErr: "active vote targets must be sorted",
		},
		{
			name: "future exchange rate timestamp returns error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.ExchangeRates = []types.ExchangeRate{
					{
						Denom:          chain.MicroUSDDenom,
						Rate:           math.LegacyOneDec(),
						BlockTimestamp: oracleTestBlockTime.Add(time.Second),
					},
				}
				return gs
			},
			expectErr:    "timestamp 2026-07-12 12:00:01 +0000 UTC after genesis block time",
			expectNoRate: chain.MicroUSDDenom,
		},
		{
			name: "nil module account returns error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.ExchangeRates = []types.ExchangeRate{
					{
						Denom:          chain.MicroUSDDenom,
						Rate:           math.LegacyOneDec(),
						BlockTimestamp: oracleTestBlockTime,
					},
				}
				return gs
			},
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
			},
			expectErr:    "module account has not been set",
			expectNoRate: chain.MicroUSDDenom,
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
				if tc.expectNoRate != "" {
					hasRate, hasErr := s.keeper.ExchangeRate.Has(s.ctx, tc.expectNoRate)
					s.Require().NoError(hasErr)
					s.Require().False(hasRate)
				}
				if tc.expectAccountingOld {
					accounting, getErr := s.keeper.Accounting.Get(s.ctx)
					s.Require().NoError(getErr)
					s.Require().Equal(types.NewAccounting(types.DefaultParams()), accounting)
				}
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
	accounting, err := s.keeper.Accounting.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected.Accounting, accounting)

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

	// Reward weights are keyed by validator address.
	rewardWeightCount := 0
	err = s.keeper.RewardWeight.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ math.Int) (bool, error) {
		rewardWeightCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.RewardWeights, rewardWeightCount)

	for _, item := range expected.RewardWeights {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().True(item.RewardWeight.Equal(rewardWeight))
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

	// Tobin taxes are read directly from params.
	tobinTaxes, err := s.keeper.GetTobinTaxes(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected.Params.TobinTaxes, tobinTaxes)

	voteTargets, err := s.keeper.VoteTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected.VoteTargets, voteTargets)
}

func (s *KeeperTestSuite) TestExportGenesis() {
	blockTime := time.Unix(1_700_000_000, 0).UTC()

	params := types.DefaultParams()
	expected := &types.GenesisState{
		Params: params,
		Accounting: types.Accounting{
			RewardWindow:             10,
			RewardDistributionWindow: 100,
			RewardWindowStartHeight:  7,
			SlashWindow:              20,
			SlashWindowStartHeight:   11,
		},
		ExchangeRates: []types.ExchangeRate{
			{Denom: chain.MicroKRWDenom, Rate: math.LegacyNewDec(1000), BlockTimestamp: blockTime, BlockHeight: 10},
			{Denom: chain.MicroUSDDenom, Rate: math.LegacyNewDecWithPrec(123, 2), BlockTimestamp: blockTime, BlockHeight: 11},
		},
		RewardWeights: []types.RewardWeight{
			{ValidatorAddress: valAddr1.String(), RewardWeight: math.NewInt(5)},
			{ValidatorAddress: valAddr2.String(), RewardWeight: math.ZeroInt()},
		},
		MissCounts: []types.MissCount{
			{ValidatorAddress: valAddr1.String(), MissCount: 5},
			{ValidatorAddress: valAddr2.String(), MissCount: 0},
		},
		VoteTargets: types.VoteTargets{Denoms: []string{
			chain.MicroKRWDenom,
			chain.MicroUSDDenom,
		}, Version: types.InitialVoteTargetVersion},
	}
	expected.Params.RewardWindow = 10
	expected.Params.VoteThreshold = math.LegacyNewDecWithPrec(6, 1)

	s.Require().NoError(s.keeper.Params.Set(s.ctx, expected.Params))
	s.Require().NoError(s.keeper.Accounting.Set(s.ctx, expected.Accounting))
	for _, item := range expected.ExchangeRates {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, item.Denom, item))
	}
	for _, item := range expected.RewardWeights {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr, item.RewardWeight))
	}
	for _, item := range expected.MissCounts {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr, item.MissCount))
	}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, expected.VoteTargets))

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
	s.Require().Equal(expected.VoteTargets, gs.VoteTargets)
	s.Require().Equal(expected.Accounting, gs.Accounting)

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

	// Reward weights are exported by validator address.
	s.Require().Len(gs.RewardWeights, len(expected.RewardWeights))
	rewardWeights := make(map[string]math.Int)
	for _, item := range gs.RewardWeights {
		rewardWeights[item.ValidatorAddress] = item.RewardWeight
	}
	for _, item := range expected.RewardWeights {
		s.Require().True(item.RewardWeight.Equal(rewardWeights[item.ValidatorAddress]))
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
