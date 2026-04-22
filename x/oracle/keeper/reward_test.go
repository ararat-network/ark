package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestRewardVoteWinners() {
	tests := []struct {
		name      string
		setup     func()
		scores    map[string]types.ValidatorScore
		expectErr string
	}{
		{
			name:      "empty ballot returns error",
			scores:    map[string]types.ValidatorScore{},
			expectErr: "no votes",
		},
		{
			name: "zero reward pool returns error",
			setup: func() {
				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(sdk.NewCoins())
			},
			scores: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 10, 1, valAddr1),
			},
			expectErr: "no rewards to give out",
		},
		{
			name: "distributes proportional rewards",
			setup: func() {
				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(400))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(stakingtypes.Validator{
					OperatorAddress: valAddr1.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(stakingtypes.Validator{
					OperatorAddress: valAddr2.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					stakingtypes.Validator{
						OperatorAddress: valAddr1.String(),
						Status:          stakingtypes.Bonded,
						Tokens:          math.NewInt(10),
					},
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10))),
				).Return(nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					stakingtypes.Validator{
						OperatorAddress: valAddr2.String(),
						Status:          stakingtypes.Bonded,
						Tokens:          math.NewInt(10),
					},
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(30))),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(40))),
				).Return(nil)
			},
			scores: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 10, 1, valAddr1),
				valAddr2.String(): types.NewValidatorScore(10, 30, 1, valAddr2),
			},
		},
		{
			name: "send failure is returned",
			setup: func() {
				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(100))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(stakingtypes.Validator{
					OperatorAddress: valAddr1.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					stakingtypes.Validator{
						OperatorAddress: valAddr1.String(),
						Status:          stakingtypes.Bonded,
						Tokens:          math.NewInt(10),
					},
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10))),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10))),
				).Return(errors.New("send failed"))
			},
			scores: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 10, 1, valAddr1),
			},
			expectErr: "Failed to send coins",
		},
		{
			name: "nil validator skips allocation and sends zero coins",
			setup: func() {
				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
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
			scores: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 10, 1, valAddr1),
			},
		},
		{
			name: "allocation failure skips distributed reward",
			setup: func() {
				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				rewardPool := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(100)))
				rewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10)))

				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(rewardPool)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(stakingtypes.Validator{
					OperatorAddress: valAddr1.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
				s.distrKeeper.EXPECT().
					AllocateTokensToValidator(s.ctx, gomock.Any(), sdk.NewDecCoinsFromCoins(rewardCoins...)).
					Return(errors.New("allocation failed"))
				s.bankKeeper.EXPECT().
					SendCoinsFromModuleToModule(s.ctx, types.ModuleName, "distribution", gomock.Any()).
					DoAndReturn(func(_ sdk.Context, _, _ string, amount sdk.Coins) error {
						s.Require().True(amount.IsZero(), "failed allocation must not be added to distributed rewards")
						return nil
					})
			},
			scores: map[string]types.ValidatorScore{
				valAddr1.String(): types.NewValidatorScore(10, 10, 1, valAddr1),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			err := s.keeper.RewardVoteWinners(s.ctx, 10, 100, tc.scores)
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}

			s.Require().NoError(err)
		})
	}
}
