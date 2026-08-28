package keeper_test

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestEndBlocker() {
	s.Run("non settlement block leaves accounting state unchanged", func() {
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(8)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.RewardWindow = 10
		params.AttendanceWindow = 20
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		accounting := types.NewAccounting(params)
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, accounting))

		s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(7)))
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 5, AttendedBlocks: 3}))

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		updatedAccounting, err := s.keeper.Accounting.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(accounting, updatedAccounting)

		rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().True(math.NewInt(7).Equal(rewardWeight), "expected %s, got %s", math.NewInt(7), rewardWeight)

		attendance, err := s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().Equal(types.Attendance{EligibleBlocks: 5, AttendedBlocks: 3}, attendance)
	})

	s.Run("reward window settles rewards and clears reward weights", func() {
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(9)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.RewardWindow = 10
		params.RewardDistributionWindow = 100
		params.AttendanceWindow = 20
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))
		s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))
		s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr2, math.ZeroInt()))

		rewardCoin := sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(100))
		distributedCoins := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(10)))
		validator := stakingtypes.Validator{
			OperatorAddress: valAddr1.String(),
			Status:          stakingtypes.Bonded,
			Tokens:          math.NewInt(10),
		}

		s.bankKeeper.EXPECT().
			GetAllBalances(s.ctx, sdk.AccAddress{1}).
			Return(sdk.NewCoins(rewardCoin))
		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)
		s.distrKeeper.EXPECT().
			AllocateTokensToValidator(s.ctx, validator, sdk.NewDecCoinsFromCoins(distributedCoins...)).
			Return(nil)
		s.bankKeeper.EXPECT().
			SendCoinsFromModuleToModule(s.ctx, types.ModuleName, "distribution", distributedCoins).
			Return(nil)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		_, err = s.keeper.RewardWeight.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected reward weight to be cleared, got %v", err)
		_, err = s.keeper.RewardWeight.Get(s.ctx, valAddr2)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected second reward weight to be cleared, got %v", err)
	})

	s.Run("attendance window jails absentees and clears records", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.RewardWindow = 30
		params.AttendanceWindow = 20
		params.MinAttendancePerWindow = math.LegacyNewDecWithPrec(90, 2)
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))
		// valAddr1: 0/20 attended -> jailed.
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))
		// valAddr2: 20/20 attended -> untouched.
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr2, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 20}))
		// valAddr3: sparse but perfect on the blocks it holds -> judged and kept.
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr3, types.Attendance{EligibleBlocks: 9, AttendedBlocks: 9}))

		consAddr, validator := s.newBondedValidator(valAddr1)

		// Only valAddr1 falls below the ratio, so exactly one Validator lookup
		// and one Jail are expected; gomock fails the test if the walk ever
		// calls either for valAddr2 or valAddr3.
		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)
		s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr).Return(nil)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		_, err = s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected attendance to be cleared, got %v", err)
		_, err = s.keeper.Attendance.Get(s.ctx, valAddr2)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected second attendance to be cleared, got %v", err)
		_, err = s.keeper.Attendance.Get(s.ctx, valAddr3)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected third attendance to be cleared, got %v", err)
	})

	s.Run("zero minimum attendance disables jailing", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.AttendanceWindow = 20
		params.MinAttendancePerWindow = math.LegacyZeroDec()
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		_, err = s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected attendance to be cleared, got %v", err)
	})

	s.Run("already jailed validators are not re-jailed", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		params, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		params.AttendanceWindow = 20
		params.MinAttendancePerWindow = math.LegacyNewDecWithPrec(90, 2)
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))

		_, validator := s.newBondedValidator(valAddr1)
		validator.Jailed = true

		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		_, err = s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected attendance to be cleared, got %v", err)
	})

	s.Run("window changes activate after the current period settles", func() {
		height := int64(19)
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)

		activeParams := types.DefaultParams()
		activeParams.RewardWindow = 20
		activeParams.RewardDistributionWindow = 200
		activeParams.AttendanceWindow = 20
		accounting := types.NewAccounting(activeParams)
		s.Require().NoError(s.keeper.Accounting.Set(s.ctx, accounting))

		desiredParams := activeParams
		desiredParams.RewardWindow = 4
		desiredParams.RewardDistributionWindow = 40
		desiredParams.AttendanceWindow = 100
		desiredParams.MinAttendancePerWindow = math.LegacyNewDecWithPrec(90, 2)
		s.Require().NoError(s.keeper.Params.Set(s.ctx, desiredParams))
		// This case is about window activation, not jailing: a perfect record
		// clears the 90% ratio, so the settlement fires no staking calls and the
		// staking mock stays unarmed.
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 4, AttendedBlocks: 4}))

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		updated, err := s.keeper.Accounting.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(uint64(4), updated.RewardWindow)
		s.Require().Equal(uint64(40), updated.RewardDistributionWindow)
		s.Require().Equal(uint64(20), updated.RewardWindowStartHeight)
		s.Require().Equal(uint64(100), updated.AttendanceWindow)
		s.Require().Equal(uint64(20), updated.AttendanceWindowStartHeight)

		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(22)
		s.Require().False(chain.IsPeriodLastBlockFrom(s.ctx, updated.RewardWindowStartHeight, updated.RewardWindow))
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(23)
		s.Require().True(chain.IsPeriodLastBlockFrom(s.ctx, updated.RewardWindowStartHeight, updated.RewardWindow))
	})
}
