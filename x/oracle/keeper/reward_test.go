package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	chain "noah/pkg/chain"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestSettleRewards() {
	validator1 := stakingtypes.Validator{
		OperatorAddress: valAddr1.String(),
		Status:          stakingtypes.Bonded,
		Tokens:          math.NewInt(10),
	}
	validator2 := stakingtypes.Validator{
		OperatorAddress: valAddr2.String(),
		Status:          stakingtypes.Bonded,
		Tokens:          math.NewInt(10),
	}

	tests := []struct {
		name      string
		setup     func()
		expectErr string
	}{
		{
			name: "empty scores return without distributing",
		},
		{
			name: "zero reward pool returns without distributing",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 10))

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(sdk.NewCoins())
			},
		},
		{
			name: "distributes proportional rewards",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 10))
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr2, 30))

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(400))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(validator2, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator1,
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(10))),
				).Return(nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator2,
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(30))),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(40))),
				).Return(nil)
			},
		},
		{
			name: "send failure is returned",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 10))

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(100))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.distrKeeper.EXPECT().
					AllocateTokensToValidator(s.ctx, validator1, gomock.Any()).
					Return(nil)
				s.bankKeeper.EXPECT().
					SendCoinsFromModuleToModule(s.ctx, types.ModuleName, "distribution", gomock.Any()).
					Return(errors.New("send failed"))
			},
			expectErr: "sending coins to distribution module",
		},
		{
			name: "missing validator is skipped",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 10))

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(100))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, nil)
			},
		},
		{
			name: "missing validator error is skipped",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 10))

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(100))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, stakingtypes.ErrNoValidatorFound)
			},
		},
		{
			name: "only transfers distributed rewards",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 10))
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr2, 30))

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(400))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, stakingtypes.ErrNoValidatorFound)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(validator2, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator2,
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(30))),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(30))),
				).Return(nil)
			},
		},
		{
			name: "allocation failure returns error",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 10))

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(100))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.distrKeeper.EXPECT().
					AllocateTokensToValidator(s.ctx, validator1, gomock.Any()).
					Return(errors.New("allocation failed"))
			},
			expectErr: "allocating oracle rewards",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			err := s.keeper.SettleRewards(s.ctx, 10, 100)
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}

			s.Require().NoError(err)
		})
	}
}
