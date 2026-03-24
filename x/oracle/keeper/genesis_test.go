package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestInitGenesis() {
	tests := []struct {
		name      string
		genesis   func() *types.GenesisState
		mockSetup func()
		expectErr string
		verify    func()
	}{
		{
			name:    "default genesis — tobin taxes seeded from whitelist",
			genesis: types.DefaultGenesisState,
			mockSetup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					mockModuleAccount{addr: sdk.AccAddress{1}},
				)
			},
			verify: func() {
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				s.Require().Equal(types.DefaultParams(), params)

				// Tobin taxes seeded from whitelist fallback
				for _, item := range types.DefaultWhitelist {
					tax, err := s.keeper.TobinTax.Get(s.ctx, item.Name)
					s.Require().NoError(err, "expected tobin tax for %s", item.Name)
					s.Require().True(item.TobinTax.Equal(tax))
				}

				// No feeder delegations
				count := 0
				_ = s.keeper.FeederDelegation.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ sdk.AccAddress) (bool, error) {
					count++
					return false, nil
				})
				s.Require().Zero(count)
			},
		},
		{
			name: "explicit tobin taxes override whitelist",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.TobinTaxes = []types.TobinTax{
					{Denom: "ucustom", TobinTax: math.LegacyNewDecWithPrec(5, 2)},
				}
				return gs
			},
			mockSetup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					mockModuleAccount{addr: sdk.AccAddress{1}},
				)
			},
			verify: func() {
				tax, err := s.keeper.TobinTax.Get(s.ctx, "ucustom")
				s.Require().NoError(err)
				s.Require().True(math.LegacyNewDecWithPrec(5, 2).Equal(tax))

				// Whitelist denoms NOT set
				_, err = s.keeper.TobinTax.Get(s.ctx, core.MicroKRWDenom)
				s.Require().Error(err)

				// Exactly one tobin tax
				count := 0
				_ = s.keeper.TobinTax.Walk(s.ctx, nil, func(_ string, _ math.LegacyDec) (bool, error) {
					count++
					return false, nil
				})
				s.Require().Equal(1, count)
			},
		},
		{
			name: "empty tobin taxes and empty whitelist — no tobin taxes set",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Params.Whitelist = types.DenomList{}
				gs.TobinTaxes = []types.TobinTax{}
				return gs
			},
			mockSetup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					mockModuleAccount{addr: sdk.AccAddress{1}},
				)
			},
			verify: func() {
				count := 0
				_ = s.keeper.TobinTax.Walk(s.ctx, nil, func(_ string, _ math.LegacyDec) (bool, error) {
					count++
					return false, nil
				})
				s.Require().Zero(count)
			},
		},
		{
			name: "full genesis — all fields stored",
			genesis: func() *types.GenesisState {
				return &types.GenesisState{
					Params: types.DefaultParams(),
					FeederDelegations: []types.FeederDelegation{
						{ValidatorAddress: valAddr1.String(), FeederAddress: accAddr1.String()},
						{ValidatorAddress: valAddr2.String(), FeederAddress: accAddr2.String()},
					},
					ExchangeRates: []types.ExchangeRateTuple{
						{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)},
						{Denom: core.MicroUSDDenom, ExchangeRate: math.LegacyNewDecWithPrec(123, 2)},
					},
					MissCounters: []types.MissCounter{
						{ValidatorAddress: valAddr1.String(), MissCounter: 5},
						{ValidatorAddress: valAddr2.String(), MissCounter: 0},
					},
					AggregateExchangeRatePrevotes: []types.AggregateExchangeRatePrevote{
						{Hash: "abc123", Voter: valAddr1.String(), SubmitBlock: 100},
					},
					AggregateExchangeRateVotes: []types.AggregateExchangeRateVote{
						{
							ExchangeRateTuples: types.ExchangeRateTuples{
								{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)},
							},
							Voter: valAddr1.String(),
						},
					},
					TobinTaxes: []types.TobinTax{
						{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					},
				}
			},
			mockSetup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					mockModuleAccount{addr: sdk.AccAddress{1}},
				)
			},
			verify: func() {
				// Feeder delegations
				feeder, err := s.keeper.FeederDelegation.Get(s.ctx, valAddr1)
				s.Require().NoError(err)
				s.Require().Equal(accAddr1, feeder)

				feeder, err = s.keeper.FeederDelegation.Get(s.ctx, valAddr2)
				s.Require().NoError(err)
				s.Require().Equal(accAddr2, feeder)

				// Exchange rates
				rate, err := s.keeper.ExchangeRate.Get(s.ctx, core.MicroKRWDenom)
				s.Require().NoError(err)
				s.Require().True(math.LegacyNewDec(1000).Equal(rate))

				rate, err = s.keeper.ExchangeRate.Get(s.ctx, core.MicroUSDDenom)
				s.Require().NoError(err)
				s.Require().True(math.LegacyNewDecWithPrec(123, 2).Equal(rate))

				// Miss counters
				mc, err := s.keeper.MissCounter.Get(s.ctx, valAddr1)
				s.Require().NoError(err)
				s.Require().Equal(uint64(5), mc)

				mc, err = s.keeper.MissCounter.Get(s.ctx, valAddr2)
				s.Require().NoError(err)
				s.Require().Equal(uint64(0), mc)

				// Prevote
				pv, err := s.keeper.AggregateExchangeRatePrevote.Get(s.ctx, valAddr1)
				s.Require().NoError(err)
				s.Require().Equal("abc123", pv.Hash)
				s.Require().Equal(uint64(100), pv.SubmitBlock)

				// Vote
				vote, err := s.keeper.AggregateExchangeRateVote.Get(s.ctx, valAddr1)
				s.Require().NoError(err)
				s.Require().Len(vote.ExchangeRateTuples, 1)
				s.Require().Equal(core.MicroKRWDenom, vote.ExchangeRateTuples[0].Denom)
				s.Require().True(math.LegacyNewDec(1000).Equal(vote.ExchangeRateTuples[0].ExchangeRate))

				// Tobin tax
				tax, err := s.keeper.TobinTax.Get(s.ctx, core.MicroKRWDenom)
				s.Require().NoError(err)
				s.Require().True(math.LegacyNewDecWithPrec(25, 4).Equal(tax))
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
			expectErr: "invalid address",
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
			expectErr: "invalid address",
		},
		{
			name: "invalid validator address in miss counter",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.MissCounters = []types.MissCounter{
					{ValidatorAddress: "invalid", MissCounter: 5},
				}
				return gs
			},
			expectErr: "invalid address",
		},
		{
			name: "invalid voter address in prevote",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.AggregateExchangeRatePrevotes = []types.AggregateExchangeRatePrevote{
					{Hash: "abc", Voter: "invalid", SubmitBlock: 100},
				}
				return gs
			},
			expectErr: "invalid address",
		},
		{
			name: "invalid voter address in vote",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.AggregateExchangeRateVotes = []types.AggregateExchangeRateVote{
					{
						ExchangeRateTuples: types.ExchangeRateTuples{
							{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)},
						},
						Voter: "invalid",
					},
				}
				return gs
			},
			expectErr: "invalid address",
		},
		{
			name:    "nil module account — error",
			genesis: types.DefaultGenesisState,
			mockSetup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
			},
			expectErr: "module account has not been set",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.mockSetup != nil {
				tc.mockSetup()
			}

			err := s.keeper.InitGenesis(s.ctx, tc.genesis())
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
			} else {
				s.Require().NoError(err)
				if tc.verify != nil {
					tc.verify()
				}
			}
		})
	}
}

func (s *KeeperTestSuite) TestExportGenesis() {
	tests := []struct {
		name   string
		setup  func()
		verify func(gs *types.GenesisState)
	}{
		{
			name:  "empty state — only default params",
			setup: func() {},
			verify: func(gs *types.GenesisState) {
				s.Require().Equal(types.DefaultParams(), gs.Params)
				s.Require().Empty(gs.FeederDelegations)
				s.Require().Empty(gs.ExchangeRates)
				s.Require().Empty(gs.MissCounters)
				s.Require().Empty(gs.AggregateExchangeRatePrevotes)
				s.Require().Empty(gs.AggregateExchangeRateVotes)
				s.Require().Empty(gs.TobinTaxes)
			},
		},
		{
			name: "custom params exported",
			setup: func() {
				params := types.DefaultParams()
				params.VotePeriod = 10
				params.VoteThreshold = math.LegacyNewDecWithPrec(6, 1)
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			},
			verify: func(gs *types.GenesisState) {
				s.Require().Equal(uint64(10), gs.Params.VotePeriod)
				s.Require().True(math.LegacyNewDecWithPrec(6, 1).Equal(gs.Params.VoteThreshold))
			},
		},
		{
			name: "feeder delegations exported",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr2, accAddr2))
			},
			verify: func(gs *types.GenesisState) {
				s.Require().Len(gs.FeederDelegations, 2)

				found := make(map[string]string)
				for _, fd := range gs.FeederDelegations {
					found[fd.ValidatorAddress] = fd.FeederAddress
				}
				s.Require().Equal(accAddr1.String(), found[valAddr1.String()])
				s.Require().Equal(accAddr2.String(), found[valAddr2.String()])
			},
		},
		{
			name: "exchange rates exported",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDec(1000)))
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDec(1)))
			},
			verify: func(gs *types.GenesisState) {
				s.Require().Len(gs.ExchangeRates, 2)

				found := make(map[string]math.LegacyDec)
				for _, er := range gs.ExchangeRates {
					found[er.Denom] = er.ExchangeRate
				}
				s.Require().True(math.LegacyNewDec(1000).Equal(found[core.MicroKRWDenom]))
				s.Require().True(math.LegacyNewDec(1).Equal(found[core.MicroUSDDenom]))
			},
		},
		{
			name: "miss counters exported",
			setup: func() {
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, uint64(5)))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr2, uint64(0)))
			},
			verify: func(gs *types.GenesisState) {
				s.Require().Len(gs.MissCounters, 2)

				found := make(map[string]uint64)
				for _, mc := range gs.MissCounters {
					found[mc.ValidatorAddress] = mc.MissCounter
				}
				s.Require().Equal(uint64(5), found[valAddr1.String()])
				s.Require().Equal(uint64(0), found[valAddr2.String()])
			},
		},
		{
			name: "prevotes exported",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, types.AggregateExchangeRatePrevote{
					Hash: "abc123", Voter: valAddr1.String(), SubmitBlock: 100,
				}))
			},
			verify: func(gs *types.GenesisState) {
				s.Require().Len(gs.AggregateExchangeRatePrevotes, 1)
				s.Require().Equal("abc123", gs.AggregateExchangeRatePrevotes[0].Hash)
				s.Require().Equal(valAddr1.String(), gs.AggregateExchangeRatePrevotes[0].Voter)
				s.Require().Equal(uint64(100), gs.AggregateExchangeRatePrevotes[0].SubmitBlock)
			},
		},
		{
			name: "votes exported",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{
						{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)},
					},
					Voter: valAddr1.String(),
				}))
			},
			verify: func(gs *types.GenesisState) {
				s.Require().Len(gs.AggregateExchangeRateVotes, 1)
				s.Require().Equal(valAddr1.String(), gs.AggregateExchangeRateVotes[0].Voter)
				s.Require().Len(gs.AggregateExchangeRateVotes[0].ExchangeRateTuples, 1)
				s.Require().Equal(core.MicroKRWDenom, gs.AggregateExchangeRateVotes[0].ExchangeRateTuples[0].Denom)
				s.Require().True(math.LegacyNewDec(1000).Equal(gs.AggregateExchangeRateVotes[0].ExchangeRateTuples[0].ExchangeRate))
			},
		},
		{
			name: "tobin taxes exported",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(10, 4)))
			},
			verify: func(gs *types.GenesisState) {
				s.Require().Len(gs.TobinTaxes, 2)

				found := make(map[string]math.LegacyDec)
				for _, tt := range gs.TobinTaxes {
					found[tt.Denom] = tt.TobinTax
				}
				s.Require().True(math.LegacyNewDecWithPrec(25, 4).Equal(found[core.MicroKRWDenom]))
				s.Require().True(math.LegacyNewDecWithPrec(10, 4).Equal(found[core.MicroUSDDenom]))
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			gs, err := s.keeper.ExportGenesis(s.ctx)
			s.Require().NoError(err)
			s.Require().NotNil(gs)

			tc.verify(gs)
		})
	}
}

func (s *KeeperTestSuite) TestGenesisExportImportExport() {
	// 1. Seed state via keeper operations (not InitGenesis)
	params := types.DefaultParams()
	params.VotePeriod = 10
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDec(1000)))
	s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, uint64(10)))
	s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, types.AggregateExchangeRatePrevote{
		Hash: "abc123", Voter: valAddr1.String(), SubmitBlock: 100,
	}))
	s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
		ExchangeRateTuples: types.ExchangeRateTuples{
			{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)},
		},
		Voter: valAddr1.String(),
	}))
	s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDecWithPrec(25, 4)))
	s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))

	// 2. Export from seeded state
	genesis1, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)

	// 3. Fresh store + keeper (SetupTest gives new KV store, new mocks, new keeper)
	s.SetupTest()

	// 4. Import exported genesis into fresh store
	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
		mockModuleAccount{addr: sdk.AccAddress{1}},
	)
	err = s.keeper.InitGenesis(s.ctx, genesis1)
	s.Require().NoError(err)

	// 5. Export again
	genesis2, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)

	// 6. Compare — the two exports must be identical
	// Params
	s.Require().Equal(genesis1.Params.VotePeriod, genesis2.Params.VotePeriod)
	s.Require().True(genesis1.Params.VoteThreshold.Equal(genesis2.Params.VoteThreshold))
	s.Require().True(genesis1.Params.RewardBand.Equal(genesis2.Params.RewardBand))
	s.Require().True(genesis1.Params.SlashFraction.Equal(genesis2.Params.SlashFraction))

	// Feeder delegations
	s.Require().Len(genesis2.FeederDelegations, len(genesis1.FeederDelegations))
	for i := range genesis1.FeederDelegations {
		s.Require().Equal(genesis1.FeederDelegations[i].ValidatorAddress, genesis2.FeederDelegations[i].ValidatorAddress)
		s.Require().Equal(genesis1.FeederDelegations[i].FeederAddress, genesis2.FeederDelegations[i].FeederAddress)
	}

	// Exchange rates
	s.Require().Len(genesis2.ExchangeRates, len(genesis1.ExchangeRates))
	for i := range genesis1.ExchangeRates {
		s.Require().Equal(genesis1.ExchangeRates[i].Denom, genesis2.ExchangeRates[i].Denom)
		s.Require().True(genesis1.ExchangeRates[i].ExchangeRate.Equal(genesis2.ExchangeRates[i].ExchangeRate))
	}

	// Miss counters
	s.Require().Len(genesis2.MissCounters, len(genesis1.MissCounters))
	for i := range genesis1.MissCounters {
		s.Require().Equal(genesis1.MissCounters[i].ValidatorAddress, genesis2.MissCounters[i].ValidatorAddress)
		s.Require().Equal(genesis1.MissCounters[i].MissCounter, genesis2.MissCounters[i].MissCounter)
	}

	// Prevotes
	s.Require().Len(genesis2.AggregateExchangeRatePrevotes, len(genesis1.AggregateExchangeRatePrevotes))
	for i := range genesis1.AggregateExchangeRatePrevotes {
		s.Require().Equal(genesis1.AggregateExchangeRatePrevotes[i].Hash, genesis2.AggregateExchangeRatePrevotes[i].Hash)
		s.Require().Equal(genesis1.AggregateExchangeRatePrevotes[i].Voter, genesis2.AggregateExchangeRatePrevotes[i].Voter)
		s.Require().Equal(genesis1.AggregateExchangeRatePrevotes[i].SubmitBlock, genesis2.AggregateExchangeRatePrevotes[i].SubmitBlock)
	}

	// Votes
	s.Require().Len(genesis2.AggregateExchangeRateVotes, len(genesis1.AggregateExchangeRateVotes))
	for i := range genesis1.AggregateExchangeRateVotes {
		s.Require().Equal(genesis1.AggregateExchangeRateVotes[i].Voter, genesis2.AggregateExchangeRateVotes[i].Voter)
		s.Require().Len(genesis2.AggregateExchangeRateVotes[i].ExchangeRateTuples, len(genesis1.AggregateExchangeRateVotes[i].ExchangeRateTuples))
		for j := range genesis1.AggregateExchangeRateVotes[i].ExchangeRateTuples {
			s.Require().Equal(genesis1.AggregateExchangeRateVotes[i].ExchangeRateTuples[j].Denom, genesis2.AggregateExchangeRateVotes[i].ExchangeRateTuples[j].Denom)
			s.Require().True(genesis1.AggregateExchangeRateVotes[i].ExchangeRateTuples[j].ExchangeRate.Equal(
				genesis2.AggregateExchangeRateVotes[i].ExchangeRateTuples[j].ExchangeRate,
			))
		}
	}

	// Tobin taxes
	s.Require().Len(genesis2.TobinTaxes, len(genesis1.TobinTaxes))
	for i := range genesis1.TobinTaxes {
		s.Require().Equal(genesis1.TobinTaxes[i].Denom, genesis2.TobinTaxes[i].Denom)
		s.Require().True(genesis1.TobinTaxes[i].TobinTax.Equal(genesis2.TobinTaxes[i].TobinTax))
	}
}
