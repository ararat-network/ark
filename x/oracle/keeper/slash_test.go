package keeper_test

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func makeValidatorWithConsKey(valAddr sdk.ValAddress, status stakingtypes.BondStatus, power int64) stakingtypes.Validator {
	validator := makeValidator(valAddr, status, power)
	privKey := ed25519.GenPrivKey()
	pubKey, _ := codectypes.NewAnyWithValue(privKey.PubKey())
	validator.ConsensusPubkey = pubKey
	return validator
}

func (s *KeeperTestSuite) TestSlashAndResetMissCounters() {
	tests := []struct {
		name             string
		setup            func()
		expectedCounters []sdk.ValAddress
	}{
		{
			name: "slashes bonded validator below min valid rate",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				params.SlashWindow = 100
				params.MinValidPerWindow = math.LegacyNewDecWithPrec(90, 2)
				params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 5))

				validator := makeValidatorWithConsKey(valAddr1, stakingtypes.Bonded, 10)
				consAddr, err := validator.GetConsAddr()
				s.Require().NoError(err)

				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator)
				s.stakingKeeper.EXPECT().Slash(
					s.ctx,
					consAddr,
					int64(100-sdk.ValidatorUpdateDelay-1),
					int64(10),
					params.SlashFraction,
				)
				s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)
			},
			expectedCounters: []sdk.ValAddress{valAddr1},
		},
		{
			name: "keeps bonded validator above min valid rate",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				params.SlashWindow = 100
				params.MinValidPerWindow = math.LegacyNewDecWithPrec(50, 2)
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 4))

				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			expectedCounters: []sdk.ValAddress{valAddr1},
		},
		{
			name: "boundary valid vote rate is not slashed",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				params.SlashWindow = 100
				params.MinValidPerWindow = math.LegacyNewDecWithPrec(50, 2)
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 5))

				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			expectedCounters: []sdk.ValAddress{valAddr1},
		},
		{
			name: "unbonded validator is not slashed",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				params.SlashWindow = 100
				params.MinValidPerWindow = math.LegacyNewDecWithPrec(90, 2)
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 5))

				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(makeValidator(valAddr1, stakingtypes.Unbonded, 10))
			},
			expectedCounters: []sdk.ValAddress{valAddr1},
		},
		{
			name: "jailed validator is not slashed",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				params.SlashWindow = 100
				params.MinValidPerWindow = math.LegacyNewDecWithPrec(90, 2)
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 5))

				validator := makeValidatorWithConsKey(valAddr1, stakingtypes.Bonded, 10)
				validator.Jailed = true

				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator)
			},
			expectedCounters: []sdk.ValAddress{valAddr1},
		},
		{
			name: "clears all counters across multiple validators",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				params.SlashWindow = 100
				params.MinValidPerWindow = math.LegacyNewDecWithPrec(50, 2)
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 4))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr2, 1))

				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			expectedCounters: []sdk.ValAddress{valAddr1, valAddr2},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			err := s.keeper.SlashAndResetMissCounters(s.ctx)
			s.Require().NoError(err)

			for _, operator := range tc.expectedCounters {
				_, err := s.keeper.MissCounter.Get(s.ctx, operator)
				s.Require().True(errors.Is(err, collections.ErrNotFound), "expected miss counter to be removed, got %v", err)
			}
		})
	}
}
