package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestInitGenesis() {
	tests := []struct {
		name      string
		genesis   func() *types.GenesisState
		expected  func() *types.GenesisState
		setup     func()
		expectErr string
	}{
		{
			name:    "default genesis seeds tobin taxes from params",
			genesis: types.DefaultGenesisState,
			expected: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.TobinTaxes = gs.Params.TobinTaxes
				return gs
			},
		},
		{
			name: "explicit tobin taxes override",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "ucustom", TobinTax: math.LegacyNewDecWithPrec(5, 2)},
				}
				return gs
			},
		},
		{
			name: "empty genesis and params tobin taxes stores no tobin taxes",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Params.TobinTaxes = types.TobinTaxes{}
				gs.TobinTaxes = []types.TobinTax{}
				return gs
			},
		},
		{
			name: "full genesis stores all collections",
			genesis: func() *types.GenesisState {
				return &types.GenesisState{
					Params: types.DefaultParams(),
					FeederDelegations: []types.FeederDelegation{
						{ValidatorAddress: valAddr1.String(), FeederAddress: accAddr1.String()},
						{ValidatorAddress: valAddr2.String(), FeederAddress: accAddr2.String()},
					},
					ExchangeRates: []types.ExchangeRate{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
						{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDecWithPrec(123, 2)},
					},
					MissCounts: []types.MissCount{
						{ValidatorAddress: valAddr1.String(), MissCount: 5},
						{ValidatorAddress: valAddr2.String(), MissCount: 0},
					},
					Prevotes: []types.Prevote{
						{Hash: "abc123", Voter: valAddr1.String(), SubmitBlock: 100},
					},
					Votes: []types.Vote{
						{
							ExchangeRates: types.ExchangeRates{
								{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
							},
							Voter: valAddr1.String(),
						},
					},
					TobinTaxes: []types.TobinTax{
						{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					},
				}
			},
		},
		{
			name: "invalid validator address in feeder delegation",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.FeederDelegations = []types.FeederDelegation{
					{ValidatorAddress: "invalid", FeederAddress: accAddr1.String()},
				}
				return gs
			},
			expectErr: "parsing feeder delegation validator address",
		},
		{
			name: "invalid feeder address in feeder delegation",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.FeederDelegations = []types.FeederDelegation{
					{ValidatorAddress: valAddr1.String(), FeederAddress: "invalid"},
				}
				return gs
			},
			expectErr: "parsing feeder delegation feeder address",
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
			name: "invalid voter address in prevote",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Prevotes = []types.Prevote{
					{Hash: "abc", Voter: "invalid", SubmitBlock: 100},
				}
				return gs
			},
			expectErr: "parsing prevote voter address",
		},
		{
			name: "invalid voter address in vote",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Votes = []types.Vote{
					{
						ExchangeRates: types.ExchangeRates{
							{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
						},
						Voter: "invalid",
					},
				}
				return gs
			},
			expectErr: "parsing vote voter address",
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
	s.Require().Equal(expected.Params.VotePeriod, params.VotePeriod)
	s.Require().True(expected.Params.VoteThreshold.Equal(params.VoteThreshold))
	s.Require().True(expected.Params.RewardBand.Equal(params.RewardBand))
	s.Require().Equal(expected.Params.RewardDistributionWindow, params.RewardDistributionWindow)
	s.Require().Equal(expected.Params.SlashWindow, params.SlashWindow)
	s.Require().True(expected.Params.SlashFraction.Equal(params.SlashFraction))
	s.Require().True(expected.Params.MinValidPerWindow.Equal(params.MinValidPerWindow))
	s.Require().Len(params.TobinTaxes, len(expected.Params.TobinTaxes))
	for i, item := range expected.Params.TobinTaxes {
		s.Require().Equal(item.Denom, params.TobinTaxes[i].Denom)
		s.Require().True(item.TobinTax.Equal(params.TobinTaxes[i].TobinTax))
	}

	// Feeder delegations are keyed by validator address.
	feederDelegationCount := 0
	err = s.keeper.FeederDelegation.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ sdk.AccAddress) (bool, error) {
		feederDelegationCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.FeederDelegations, feederDelegationCount)

	for _, item := range expected.FeederDelegations {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)
		expectedFeeder, err := sdk.AccAddressFromBech32(item.FeederAddress)
		s.Require().NoError(err)

		feeder, err := s.keeper.FeederDelegation.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().Equal(expectedFeeder, feeder)
	}

	// Exchange rates are keyed by denom.
	exchangeRateCount := 0
	err = s.keeper.ExchangeRate.Walk(s.ctx, nil, func(_ string, _ math.LegacyDec) (bool, error) {
		exchangeRateCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.ExchangeRates, exchangeRateCount)

	for _, item := range expected.ExchangeRates {
		rate, err := s.keeper.ExchangeRate.Get(s.ctx, item.Denom)
		s.Require().NoError(err)
		s.Require().True(item.Rate.Equal(rate), "expected %s for %s, got %s", item.Rate, item.Denom, rate)
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

	// Prevotes are keyed by voter address.
	prevoteCount := 0
	err = s.keeper.Prevote.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.Prevote) (bool, error) {
		prevoteCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.Prevotes, prevoteCount)

	for _, item := range expected.Prevotes {
		valAddr, err := sdk.ValAddressFromBech32(item.Voter)
		s.Require().NoError(err)

		prevote, err := s.keeper.Prevote.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().Equal(item, prevote)
	}

	// Votes are keyed by voter address.
	voteCount := 0
	err = s.keeper.Vote.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.Vote) (bool, error) {
		voteCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.Votes, voteCount)

	for _, item := range expected.Votes {
		valAddr, err := sdk.ValAddressFromBech32(item.Voter)
		s.Require().NoError(err)

		vote, err := s.keeper.Vote.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().Equal(item, vote)
	}

	// Tobin taxes are keyed by denom; default genesis seeds these from params when explicit genesis taxes are empty.
	tobinTaxes := map[string]math.LegacyDec{}
	err = s.keeper.TobinTax.Walk(s.ctx, nil, func(denom string, tobinTax math.LegacyDec) (bool, error) {
		tobinTaxes[denom] = tobinTax
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(tobinTaxes, len(expected.TobinTaxes))

	for _, item := range expected.TobinTaxes {
		tax, ok := tobinTaxes[item.Denom]
		s.Require().True(ok, "expected tobin tax for %s", item.Denom)
		s.Require().True(item.TobinTax.Equal(tax), "expected %s for %s, got %s", item.TobinTax, item.Denom, tax)
	}
}

func (s *KeeperTestSuite) TestExportGenesis() {
	expected := &types.GenesisState{
		Params: types.DefaultParams(),
		FeederDelegations: []types.FeederDelegation{
			{ValidatorAddress: valAddr1.String(), FeederAddress: accAddr1.String()},
			{ValidatorAddress: valAddr2.String(), FeederAddress: accAddr2.String()},
		},
		ExchangeRates: []types.ExchangeRate{
			{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
			{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDecWithPrec(123, 2)},
		},
		MissCounts: []types.MissCount{
			{ValidatorAddress: valAddr1.String(), MissCount: 5},
			{ValidatorAddress: valAddr2.String(), MissCount: 0},
		},
		Prevotes: []types.Prevote{
			{Hash: "abc123", Voter: valAddr1.String(), SubmitBlock: 100},
		},
		Votes: []types.Vote{
			{
				ExchangeRates: types.ExchangeRates{
					{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
				},
				Voter: valAddr1.String(),
			},
		},
		TobinTaxes: []types.TobinTax{
			{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(10, 4)},
		},
	}
	expected.Params.VotePeriod = 10
	expected.Params.VoteThreshold = math.LegacyNewDecWithPrec(6, 1)

	s.Require().NoError(s.keeper.Params.Set(s.ctx, expected.Params))
	for _, item := range expected.FeederDelegations {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)
		feederAddr, err := sdk.AccAddressFromBech32(item.FeederAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr, feederAddr))
	}
	for _, item := range expected.ExchangeRates {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, item.Denom, item.Rate))
	}
	for _, item := range expected.MissCounts {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr, item.MissCount))
	}
	for _, item := range expected.Prevotes {
		valAddr, err := sdk.ValAddressFromBech32(item.Voter)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.Prevote.Set(s.ctx, valAddr, item))
	}
	for _, item := range expected.Votes {
		valAddr, err := sdk.ValAddressFromBech32(item.Voter)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.Vote.Set(s.ctx, valAddr, item))
	}
	for _, item := range expected.TobinTaxes {
		s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, item.Denom, item.TobinTax))
	}

	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NotNil(gs)

	// Params are exported from the Params collection.
	s.Require().Equal(expected.Params.VotePeriod, gs.Params.VotePeriod)
	s.Require().True(expected.Params.VoteThreshold.Equal(gs.Params.VoteThreshold))
	s.Require().True(expected.Params.RewardBand.Equal(gs.Params.RewardBand))
	s.Require().Equal(expected.Params.RewardDistributionWindow, gs.Params.RewardDistributionWindow)
	s.Require().Equal(expected.Params.SlashWindow, gs.Params.SlashWindow)
	s.Require().True(expected.Params.SlashFraction.Equal(gs.Params.SlashFraction))
	s.Require().True(expected.Params.MinValidPerWindow.Equal(gs.Params.MinValidPerWindow))
	s.Require().Equal(expected.Params.TobinTaxes, gs.Params.TobinTaxes)

	// Feeder delegations are exported by validator address.
	s.Require().Len(gs.FeederDelegations, len(expected.FeederDelegations))
	feederDelegations := make(map[string]string)
	for _, item := range gs.FeederDelegations {
		feederDelegations[item.ValidatorAddress] = item.FeederAddress
	}
	for _, item := range expected.FeederDelegations {
		s.Require().Equal(item.FeederAddress, feederDelegations[item.ValidatorAddress])
	}

	// Exchange rates are exported by denom.
	s.Require().Len(gs.ExchangeRates, len(expected.ExchangeRates))
	exchangeRates := make(map[string]math.LegacyDec)
	for _, item := range gs.ExchangeRates {
		exchangeRates[item.Denom] = item.Rate
	}
	for _, item := range expected.ExchangeRates {
		s.Require().True(item.Rate.Equal(exchangeRates[item.Denom]))
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

	// Prevotes are exported by voter address.
	s.Require().Len(gs.Prevotes, len(expected.Prevotes))
	prevotes := make(map[string]types.Prevote)
	for _, item := range gs.Prevotes {
		prevotes[item.Voter] = item
	}
	for _, item := range expected.Prevotes {
		s.Require().Equal(item, prevotes[item.Voter])
	}

	// Votes are exported by voter address.
	s.Require().Len(gs.Votes, len(expected.Votes))
	votes := make(map[string]types.Vote)
	for _, item := range gs.Votes {
		votes[item.Voter] = item
	}
	for _, item := range expected.Votes {
		s.Require().Equal(item, votes[item.Voter])
	}

	// Tobin taxes are exported by denom.
	s.Require().Len(gs.TobinTaxes, len(expected.TobinTaxes))
	tobinTaxes := make(map[string]math.LegacyDec)
	for _, item := range gs.TobinTaxes {
		tobinTaxes[item.Denom] = item.TobinTax
	}
	for _, item := range expected.TobinTaxes {
		s.Require().True(item.TobinTax.Equal(tobinTaxes[item.Denom]))
	}
}
