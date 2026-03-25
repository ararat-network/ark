package keeper_test

import (
	"errors"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestRewardBallotWinners() {
	tests := []struct {
		name      string
		setup     func()
		winners   map[string]types.Claim
		expectErr string
	}{
		{
			name:      "empty ballot returns error",
			winners:   map[string]types.Claim{},
			expectErr: "empty ballot",
		},
		{
			name: "zero reward pool returns error",
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(mockModuleAccount{addr: sdk.AccAddress{1}})
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(sdk.NewCoins())
			},
			winners: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
			},
			expectErr: "no rewards to give out",
		},
		{
			name: "distributes proportional rewards",
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(mockModuleAccount{addr: sdk.AccAddress{1}})
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(400))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(makeValidator(valAddr1, stakingtypes.Bonded, 10))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(makeValidator(valAddr2, stakingtypes.Bonded, 10))
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					makeValidator(valAddr1, stakingtypes.Bonded, 10),
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10))),
				)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					makeValidator(valAddr2, stakingtypes.Bonded, 10),
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(30))),
				)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(40))),
				).Return(nil)
			},
			winners: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
				operStr(valAddr2): types.NewClaim(10, 30, 1, valAddr2),
			},
		},
		{
			name: "send failure is returned",
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(mockModuleAccount{addr: sdk.AccAddress{1}})
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(100))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(makeValidator(valAddr1, stakingtypes.Bonded, 10))
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					makeValidator(valAddr1, stakingtypes.Bonded, 10),
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10))),
				)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10))),
				).Return(errors.New("send failed"))
			},
			winners: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
			},
			expectErr: "Failed to send coins",
		},
		{
			name: "nil validator skips allocation and sends zero coins",
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(mockModuleAccount{addr: sdk.AccAddress{1}})
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(100))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.Coins(nil),
				).Return(nil)
			},
			winners: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 10, 1, valAddr1),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			err := s.keeper.RewardBallotWinners(s.ctx, 10, 100, tc.winners)
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}

			s.Require().NoError(err)
		})
	}
}
