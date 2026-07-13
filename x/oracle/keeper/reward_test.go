package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestSettleRewards() {
	maxUint64 := ^uint64(0)
	largeScore := math.NewIntFromUint64(maxUint64).AddRaw(1)
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
		name                     string
		setup                    func()
		rewardWindow             uint64
		rewardDistributionWindow uint64
		expectErr                string
	}{
		{
			name: "empty scores return without distributing",
		},
		{
			name: "zero reward pool returns without distributing",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(sdk.Coin{Denom: chain.MicroNoahDenom, Amount: math.ZeroInt()})
			},
		},
		{
			name: "distributes proportional rewards",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr2, math.NewInt(30)))

				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(400)))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(validator2, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator1,
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(10))),
				).Return(nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator2,
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(30))),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(40))),
				).Return(nil)
			},
		},
		{
			name: "supports score above maximum uint64 and maximum distribution window",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, largeScore))

				rewardCoin := sdk.NewCoin(
					chain.MicroNoahDenom,
					largeScore,
				)
				expectedRewards := sdk.NewCoins(rewardCoin)
				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(rewardCoin)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator1,
					sdk.NewDecCoinsFromCoins(expectedRewards...),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					expectedRewards,
				).Return(nil)
			},
			rewardWindow:             maxUint64,
			rewardDistributionWindow: maxUint64,
		},
		{
			name: "send failure is returned",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(100)))
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
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(100)))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, nil)
			},
		},
		{
			name: "missing validator error is skipped",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(100)))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, stakingtypes.ErrNoValidatorFound)
			},
		},
		{
			name: "only transfers distributed rewards",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr2, math.NewInt(30)))

				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(400)))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, stakingtypes.ErrNoValidatorFound)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(validator2, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator2,
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(30))),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(30))),
				).Return(nil)
			},
		},
		{
			name: "allocation failure returns error",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetBalance(s.ctx, sdk.AccAddress{1}, chain.MicroNoahDenom).
					Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(100)))
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

			rewardWindow := tc.rewardWindow
			if rewardWindow == 0 {
				rewardWindow = 10
			}
			rewardDistributionWindow := tc.rewardDistributionWindow
			if rewardDistributionWindow == 0 {
				rewardDistributionWindow = 100
			}
			err := s.keeper.SettleRewards(s.ctx, rewardWindow, rewardDistributionWindow)
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}

			s.Require().NoError(err)
		})
	}
}
