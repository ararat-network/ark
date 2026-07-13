package keeper_test

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestEndBlocker() {
	s.Run("non settlement block leaves accounting state unchanged", func() {
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(8)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.RewardWindow = 10
		params.SlashWindow = 20
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccountingState(params)))

		s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(7)))
		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, 3))

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		scoreWeight, err := s.keeper.ScoreWeight.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().True(math.NewInt(7).Equal(scoreWeight))

		missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().Equal(uint64(3), missCount)
	})

	s.Run("reward window settles rewards and clears score weights", func() {
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(9)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.RewardWindow = 10
		params.RewardDistributionWindow = 100
		params.SlashWindow = 20
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccountingState(params)))
		s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))
		s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr2, math.ZeroInt()))

		rewardCoin := sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(100))
		distributedCoins := sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(10)))
		validator := stakingtypes.Validator{
			OperatorAddress: valAddr1.String(),
			Status:          stakingtypes.Bonded,
			Tokens:          math.NewInt(10),
		}

		s.bankKeeper.EXPECT().
			GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
			Return(rewardCoin)
		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)
		s.distrKeeper.EXPECT().
			AllocateTokensToValidator(s.ctx, validator, sdk.NewDecCoinsFromCoins(distributedCoins...)).
			Return(nil)
		s.bankKeeper.EXPECT().
			SendCoinsFromModuleToModule(s.ctx, types.ModuleName, "distribution", distributedCoins).
			Return(nil)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		_, err = s.keeper.ScoreWeight.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected score weight to be cleared, got %v", err)
		_, err = s.keeper.ScoreWeight.Get(s.ctx, valAddr2)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected second score weight to be cleared, got %v", err)
	})

	s.Run("slash window settles slash and clears miss counts", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.RewardWindow = 30
		params.SlashWindow = 20
		params.MinValidPerWindow = math.LegacyNewDecWithPrec(90, 2)
		params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccountingState(params)))
		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, 20))
		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr2, 0))

		powerReduction := math.NewInt(1_000_000)
		pubKey := ed25519.GenPrivKey().PubKey()
		validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
		s.Require().NoError(err)
		validator.Status = stakingtypes.Bonded
		validator.Tokens = powerReduction.MulRaw(10)
		consAddr, err := validator.GetConsAddr()
		s.Require().NoError(err)

		s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(powerReduction)
		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)
		s.stakingKeeper.EXPECT().
			Slash(
				s.ctx,
				consAddr,
				height-sdk.ValidatorUpdateDelay-1,
				int64(10),
				params.SlashFraction,
			).
			Return(math.NewInt(1), nil)
		s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		_, err = s.keeper.MissCount.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected miss count to be cleared, got %v", err)
		_, err = s.keeper.MissCount.Get(s.ctx, valAddr2)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected second miss count to be cleared, got %v", err)
	})

	s.Run("window changes activate after the current period settles", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		activeParams := types.DefaultParams()
		activeParams.RewardWindow = 20
		activeParams.RewardDistributionWindow = 200
		activeParams.SlashWindow = 20
		accounting := types.NewAccountingState(activeParams)
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, accounting))

		desiredParams := activeParams
		desiredParams.RewardWindow = 4
		desiredParams.RewardDistributionWindow = 40
		desiredParams.SlashWindow = 100
		desiredParams.MinValidPerWindow = math.LegacyNewDecWithPrec(90, 2)
		s.Require().NoError(s.keeper.Params.Set(s.ctx, desiredParams))
		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, 4))

		powerReduction := math.NewInt(1_000_000)
		pubKey := ed25519.GenPrivKey().PubKey()
		validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
		s.Require().NoError(err)
		validator.Status = stakingtypes.Bonded
		validator.Tokens = powerReduction.MulRaw(10)
		consAddr, err := validator.GetConsAddr()
		s.Require().NoError(err)

		s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(powerReduction)
		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)
		s.stakingKeeper.EXPECT().Slash(
			s.ctx,
			consAddr,
			height-sdk.ValidatorUpdateDelay-1,
			int64(10),
			desiredParams.SlashFraction,
		).Return(math.NewInt(1), nil)
		s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		updated, err := s.keeper.Accounting.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(uint64(4), updated.RewardWindow)
		s.Require().Equal(uint64(40), updated.RewardDistributionWindow)
		s.Require().Equal(uint64(20), updated.RewardWindowStartHeight)
		s.Require().Equal(uint64(100), updated.SlashWindow)
		s.Require().Equal(uint64(20), updated.SlashWindowStartHeight)

		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(22)
		s.Require().False(chain.IsPeriodLastBlockFrom(s.ctx, updated.RewardWindowStartHeight, updated.RewardWindow))
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(23)
		s.Require().True(chain.IsPeriodLastBlockFrom(s.ctx, updated.RewardWindowStartHeight, updated.RewardWindow))
	})
}
