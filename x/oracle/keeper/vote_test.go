package keeper_test

import (
	"context"
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestBuildValidatorScoreMap() {
	powerReduction := math.NewInt(1_000_000)

	tests := []struct {
		name          string
		validators    []stakingtypes.Validator
		iterateErr    error
		expectedPower map[string]int64
		expectErr     string
	}{
		{
			name: "two bonded validators",
			validators: []stakingtypes.Validator{
				{
					OperatorAddress: valAddr1.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          powerReduction.MulRaw(10),
				},
				{
					OperatorAddress: valAddr2.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          powerReduction.MulRaw(20),
				},
			},
			expectedPower: map[string]int64{
				valAddr1.String(): 10,
				valAddr2.String(): 20,
			},
		},
		{
			name:          "no validators",
			expectedPower: map[string]int64{},
		},
		{
			name:       "iterator error is returned",
			iterateErr: errors.New("iterator failed"),
			expectErr:  "iterator failed",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(powerReduction)
			s.stakingKeeper.EXPECT().
				IterateBondedValidatorsByPower(s.ctx, gomock.Any()).
				DoAndReturn(func(_ context.Context, fn func(int64, stakingtypes.ValidatorI) bool) error {
					for i, validator := range tc.validators {
						if fn(int64(i), validator) {
							break
						}
					}
					return tc.iterateErr
				})
			if tc.iterateErr == nil && len(tc.validators) > 0 {
				s.stakingKeeper.EXPECT().ValidatorAddressCodec().Return(address.NewBech32Codec("cosmosvaloper")).AnyTimes()
			}

			claims, err := s.keeper.BuildValidatorScoreMap(s.ctx)
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}
			s.Require().NoError(err)
			s.Require().Len(claims, len(tc.expectedPower))

			for operator, power := range tc.expectedPower {
				claim, ok := claims[operator]
				s.Require().True(ok)
				s.Require().Equal(power, claim.Power)
				s.Require().Equal(int64(0), claim.Weight)
				s.Require().Equal(int64(0), claim.WinCount)
			}
		})
	}
}

func (s *KeeperTestSuite) TestCountMisses() {
	type missCount struct {
		operator sdk.ValAddress
		count    uint64
	}

	tests := []struct {
		name           string
		initialMisses  []missCount
		voteTargets    map[string]math.LegacyDec
		claimMap       map[string]types.ValidatorScore
		expectedMisses []missCount
	}{
		{
			name: "increments only validators missing a passing denom",
			initialMisses: []missCount{
				{operator: valAddr1, count: 2},
				{operator: valAddr2, count: 7},
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDec(1),
				core.MicroUSDDenom: math.LegacyNewDec(1),
			},
			claimMap: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 0, 1, valAddr1),
				valAddr2.String(): types.NewValidatorScore(10, 0, 2, valAddr2),
			},
			expectedMisses: []missCount{
				{operator: valAddr1, count: 3},
				{operator: valAddr2, count: 7},
			},
		},
		{
			name: "creates counter for first miss",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDec(1),
			},
			claimMap: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 0, 0, valAddr1),
			},
			expectedMisses: []missCount{
				{operator: valAddr1, count: 1},
			},
		},
		{
			name:        "empty vote targets means no misses",
			voteTargets: map[string]math.LegacyDec{},
			claimMap: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 0, 0, valAddr1),
			},
		},
		{
			name: "all validators voted all targets",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDec(1),
				core.MicroUSDDenom: math.LegacyNewDec(1),
			},
			claimMap: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 0, 2, valAddr1),
				valAddr2.String(): types.NewValidatorScore(10, 0, 2, valAddr2),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, miss := range tc.initialMisses {
				s.Require().NoError(s.keeper.MissCount.Set(s.ctx, miss.operator, miss.count))
			}

			err := s.keeper.CountMisses(s.ctx, tc.voteTargets, tc.claimMap)
			s.Require().NoError(err)

			expectedOperators := map[string]bool{}
			for _, expected := range tc.expectedMisses {
				expectedOperators[expected.operator.String()] = true
				missCount, err := s.keeper.MissCount.Get(s.ctx, expected.operator)
				s.Require().NoError(err)
				s.Require().Equal(expected.count, missCount)
			}

			for _, operator := range []sdk.ValAddress{valAddr1, valAddr2} {
				if expectedOperators[operator.String()] {
					continue
				}

				_, err := s.keeper.MissCount.Get(s.ctx, operator)
				s.Require().True(errors.Is(err, collections.ErrNotFound), "expected no miss count for %s, got %v", operator.String(), err)
			}
		})
	}
}
