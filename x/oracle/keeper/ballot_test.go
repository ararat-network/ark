package keeper_test

import (
	"sort"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestOrganizeBallotByDenom() {
	defaultClaimMap := func() map[string]types.Claim {
		return map[string]types.Claim{
			operStr(valAddr1): types.NewClaim(10, 0, 0, valAddr1),
			operStr(valAddr2): types.NewClaim(20, 0, 0, valAddr2),
		}
	}

	tests := []struct {
		name           string
		setup          func()
		claimMap       map[string]types.Claim
		expectedDenoms []string
		check          func(result map[string]types.ExchangeRateBallot)
	}{
		{
			name:           "no votes — empty result",
			setup:          func() {},
			claimMap:       defaultClaimMap(),
			expectedDenoms: nil,
		},
		{
			name: "single validator, single denom",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					Voter:              operStr(valAddr1),
				}))
			},
			claimMap:       defaultClaimMap(),
			expectedDenoms: []string{core.MicroKRWDenom},
			check: func(result map[string]types.ExchangeRateBallot) {
				ballot := result[core.MicroKRWDenom]
				s.Require().Len(ballot, 1)
				s.Require().Equal(int64(10), ballot[0].Power)
				s.Require().True(math.LegacyNewDec(1000).Equal(ballot[0].ExchangeRate))
				s.Require().Equal(valAddr1.Bytes(), ballot[0].Voter.Bytes())
			},
		},
		{
			name: "single validator, multiple denoms — separate ballots",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{
						{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)},
						{Denom: core.MicroUSDDenom, ExchangeRate: math.LegacyNewDec(1)},
					},
					Voter: operStr(valAddr1),
				}))
			},
			claimMap:       defaultClaimMap(),
			expectedDenoms: []string{core.MicroKRWDenom, core.MicroUSDDenom},
			check: func(result map[string]types.ExchangeRateBallot) {
				s.Require().Len(result[core.MicroKRWDenom], 1)
				s.Require().Len(result[core.MicroUSDDenom], 1)
				s.Require().True(math.LegacyNewDec(1000).Equal(result[core.MicroKRWDenom][0].ExchangeRate))
				s.Require().True(math.LegacyNewDec(1).Equal(result[core.MicroUSDDenom][0].ExchangeRate))
			},
		},
		{
			name: "multiple validators — aggregated and sorted by rate",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(2000)}},
					Voter:              operStr(valAddr1),
				}))
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr2, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					Voter:              operStr(valAddr2),
				}))
			},
			claimMap:       defaultClaimMap(),
			expectedDenoms: []string{core.MicroKRWDenom},
			check: func(result map[string]types.ExchangeRateBallot) {
				ballot := result[core.MicroKRWDenom]
				s.Require().Len(ballot, 2)
				// Sorted by rate: 1000 before 2000
				s.Require().True(math.LegacyNewDec(1000).Equal(ballot[0].ExchangeRate))
				s.Require().True(math.LegacyNewDec(2000).Equal(ballot[1].ExchangeRate))
				// Power matches claim map
				s.Require().Equal(int64(20), ballot[0].Power) // valAddr2 voted 1000
				s.Require().Equal(int64(10), ballot[1].Power) // valAddr1 voted 2000
			},
		},
		{
			name: "validator not in claim map — filtered out",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					Voter:              operStr(valAddr1),
				}))
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr2, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					Voter:              operStr(valAddr2),
				}))
			},
			claimMap: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 0, 0, valAddr1),
			},
			expectedDenoms: []string{core.MicroKRWDenom},
			check: func(result map[string]types.ExchangeRateBallot) {
				ballot := result[core.MicroKRWDenom]
				s.Require().Len(ballot, 1)
				s.Require().Equal(valAddr1.Bytes(), ballot[0].Voter.Bytes())
			},
		},
		{
			name: "all validators filtered — empty result",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					Voter:              operStr(valAddr1),
				}))
			},
			claimMap:       map[string]types.Claim{},
			expectedDenoms: nil,
		},
		{
			name: "abstain vote (zero rate) — power zeroed",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyZeroDec()}},
					Voter:              operStr(valAddr1),
				}))
			},
			claimMap:       defaultClaimMap(),
			expectedDenoms: []string{core.MicroKRWDenom},
			check: func(result map[string]types.ExchangeRateBallot) {
				s.Require().Equal(int64(0), result[core.MicroKRWDenom][0].Power, "abstain vote should have zero power")
			},
		},
		{
			name: "abstain vote (negative rate) — power zeroed",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(-1)}},
					Voter:              operStr(valAddr1),
				}))
			},
			claimMap:       defaultClaimMap(),
			expectedDenoms: []string{core.MicroKRWDenom},
			check: func(result map[string]types.ExchangeRateBallot) {
				s.Require().Equal(int64(0), result[core.MicroKRWDenom][0].Power, "negative rate should have zero power")
			},
		},
		{
			name: "positive vote retains claim power",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					Voter:              operStr(valAddr1),
				}))
			},
			claimMap: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(42, 0, 0, valAddr1),
			},
			expectedDenoms: []string{core.MicroKRWDenom},
			check: func(result map[string]types.ExchangeRateBallot) {
				s.Require().Equal(int64(42), result[core.MicroKRWDenom][0].Power)
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			result, err := s.keeper.OrganizeBallotByDenom(s.ctx, tc.claimMap)
			s.Require().NoError(err)
			s.Require().Len(result, len(tc.expectedDenoms))

			for _, denom := range tc.expectedDenoms {
				ballot, ok := result[denom]
				s.Require().True(ok, "missing denom %s", denom)
				s.Require().True(sort.IsSorted(ballot), "ballot for %s not sorted", denom)
			}

			if tc.check != nil {
				tc.check(result)
			}
		})
	}
}

func (s *KeeperTestSuite) TestClearBallots() {
	tests := []struct {
		name                 string
		setup                func()
		blockHeight          int64
		votePeriod           uint64
		expectedVoteCount    int
		expectedPrevoteCount int
		check                func()
	}{
		{
			name:                 "no votes or prevotes — no-op",
			setup:                func() {},
			blockHeight:          10,
			votePeriod:           5,
			expectedVoteCount:    0,
			expectedPrevoteCount: 0,
		},
		{
			name: "all votes cleared unconditionally",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					Voter: operStr(valAddr1),
				}))
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr2, types.AggregateExchangeRateVote{
					Voter: operStr(valAddr2),
				}))
			},
			blockHeight:          10,
			votePeriod:           5,
			expectedVoteCount:    0,
			expectedPrevoteCount: 0,
		},
		{
			name: "expired prevote cleared",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, types.AggregateExchangeRatePrevote{
					SubmitBlock: 2,
				}))
			},
			blockHeight:          10, // 10 > 2 + 5 = 7 → expired
			votePeriod:           5,
			expectedPrevoteCount: 0,
		},
		{
			name: "prevote just past expiry — cleared",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, types.AggregateExchangeRatePrevote{
					SubmitBlock: 4,
				}))
			},
			blockHeight:          10, // 10 > 4 + 5 = 9 → expired by exactly 1
			votePeriod:           5,
			expectedPrevoteCount: 0,
		},
		{
			name: "prevote at exact expiry boundary — kept",
			setup: func() {
				// blockHeight == submitBlock + votePeriod → NOT expired (condition is strictly >)
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, types.AggregateExchangeRatePrevote{
					SubmitBlock: 5,
				}))
			},
			blockHeight:          10, // 10 > 5 + 5 = 10? No → kept
			votePeriod:           5,
			expectedPrevoteCount: 1,
		},
		{
			name: "fresh prevote kept",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, types.AggregateExchangeRatePrevote{
					SubmitBlock: 8,
				}))
			},
			blockHeight:          10, // 10 > 8 + 5 = 13? No → kept
			votePeriod:           5,
			expectedPrevoteCount: 1,
		},
		{
			name: "mix — expired prevote cleared, fresh kept, all votes cleared",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, types.AggregateExchangeRatePrevote{
					SubmitBlock: 1, // 10 > 1 + 5 = 6 → expired
				}))
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr2, types.AggregateExchangeRatePrevote{
					SubmitBlock: 8, // 10 > 8 + 5 = 13? No → kept
				}))
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					Voter: operStr(valAddr1),
				}))
			},
			blockHeight:          10,
			votePeriod:           5,
			expectedVoteCount:    0,
			expectedPrevoteCount: 1,
			check: func() {
				// Verify the correct prevote survived
				_, err := s.keeper.AggregateExchangeRatePrevote.Get(s.ctx, valAddr1)
				s.Require().Error(err, "val1 expired prevote should be cleared")
				_, err = s.keeper.AggregateExchangeRatePrevote.Get(s.ctx, valAddr2)
				s.Require().NoError(err, "val2 fresh prevote should be kept")
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(tc.blockHeight)
			s.ctx = sdkCtx

			tc.setup()

			err := s.keeper.ClearBallots(s.ctx, tc.votePeriod)
			s.Require().NoError(err)

			voteCount := 0
			_ = s.keeper.AggregateExchangeRateVote.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.AggregateExchangeRateVote) (bool, error) {
				voteCount++
				return false, nil
			})
			s.Require().Equal(tc.expectedVoteCount, voteCount, "unexpected vote count")

			prevoteCount := 0
			_ = s.keeper.AggregateExchangeRatePrevote.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.AggregateExchangeRatePrevote) (bool, error) {
				prevoteCount++
				return false, nil
			})
			s.Require().Equal(tc.expectedPrevoteCount, prevoteCount, "unexpected prevote count")

			if tc.check != nil {
				tc.check()
			}
		})
	}
}

func (s *KeeperTestSuite) TestApplyWhitelist() {
	tests := []struct {
		name             string
		setup            func()
		whitelist        types.DenomList
		voteTargets      map[string]math.LegacyDec
		expectedTobinTax map[string]math.LegacyDec
		check            func()
	}{
		{
			name: "no change — whitelist matches voteTargets",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
			},
			whitelist: types.DenomList{
				{Name: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			expectedTobinTax: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
		{
			name: "new denom added — metadata correctly constructed",
			setup: func() {
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, false)
				s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any()).Do(
					func(_ interface{}, meta banktypes.Metadata) {
						s.Require().Equal("uusd", meta.Base)
						s.Require().Equal("usd", meta.Display)
						s.Require().Equal("USD NOAH", meta.Name)
						s.Require().Equal("USN", meta.Symbol)
						s.Require().Equal("The native stable token of the Noah Icarus.", meta.Description)
						s.Require().Len(meta.DenomUnits, 3)
						s.Require().Equal("uusd", meta.DenomUnits[0].Denom)
						s.Require().Equal(uint32(0), meta.DenomUnits[0].Exponent)
						s.Require().Equal("musd", meta.DenomUnits[1].Denom)
						s.Require().Equal(uint32(3), meta.DenomUnits[1].Exponent)
						s.Require().Equal("usd", meta.DenomUnits[2].Denom)
						s.Require().Equal(uint32(6), meta.DenomUnits[2].Exponent)
					},
				)
			},
			whitelist: types.DenomList{
				{Name: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			voteTargets: map[string]math.LegacyDec{},
			expectedTobinTax: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
		{
			name: "denom removed from whitelist — old cleared",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, true)
			},
			whitelist: types.DenomList{
				{Name: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			expectedTobinTax: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
		{
			name: "tobin tax value changed — triggers update",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, true)
			},
			whitelist: types.DenomList{
				{Name: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			expectedTobinTax: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(50, 4),
			},
		},
		{
			name: "empty whitelist, non-empty voteTargets — clears all",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
			},
			whitelist: types.DenomList{},
			voteTargets: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			expectedTobinTax: map[string]math.LegacyDec{},
		},
		{
			name:             "both empty — no update needed",
			setup:            func() {},
			whitelist:        types.DenomList{},
			voteTargets:      map[string]math.LegacyDec{},
			expectedTobinTax: map[string]math.LegacyDec{},
		},
		{
			name: "metadata already exists — no SetDenomMetaData",
			setup: func() {
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, true)
				// No SetDenomMetaData expectation — gomock fails if called
			},
			whitelist: types.DenomList{
				{Name: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			voteTargets: map[string]math.LegacyDec{},
			expectedTobinTax: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
		{
			name: "multiple new denoms — all registered",
			setup: func() {
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, false)
				s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any())
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroKRWDenom).Return(banktypes.Metadata{}, false)
				s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any())
			},
			whitelist: types.DenomList{
				{Name: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				{Name: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(125, 5)},
			},
			voteTargets: map[string]math.LegacyDec{},
			expectedTobinTax: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(125, 5),
			},
		},
		{
			name: "denom in whitelist not in voteTargets — triggers update",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroKRWDenom).Return(banktypes.Metadata{}, false)
				s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any())
			},
			whitelist: types.DenomList{
				{Name: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			expectedTobinTax: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			err := s.keeper.ApplyWhitelist(s.ctx, tc.whitelist, tc.voteTargets)
			s.Require().NoError(err)

			actualTobinTax := make(map[string]math.LegacyDec)
			_ = s.keeper.TobinTax.Walk(s.ctx, nil, func(denom string, tax math.LegacyDec) (bool, error) {
				actualTobinTax[denom] = tax
				return false, nil
			})

			s.Require().Equal(len(tc.expectedTobinTax), len(actualTobinTax), "tobin tax count mismatch")
			for denom, expectedTax := range tc.expectedTobinTax {
				actualTax, ok := actualTobinTax[denom]
				s.Require().True(ok, "missing tobin tax for %s", denom)
				s.Require().True(expectedTax.Equal(actualTax), "tobin tax for %s: expected %s, got %s", denom, expectedTax, actualTax)
			}

			if tc.check != nil {
				tc.check()
			}
		})
	}
}
