package keeper_test

import (
	"context"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestEndBlocker() {
	tests := []struct {
		name        string
		blockHeight int64
		votePeriod  uint64
		slashWindow uint64
	}{
		{
			name:        "mid-period no tally or slash",
			blockHeight: 3,
			votePeriod:  5,
			slashWindow: 10,
		},
		{
			name:        "last block of vote period runs tally",
			blockHeight: 4,
			votePeriod:  5,
			slashWindow: 1000,
		},
		{
			name:        "last block of slash window runs tally and slash",
			blockHeight: 9,
			votePeriod:  5,
			slashWindow: 10,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(tc.blockHeight)

			tobinTax := types.TobinTax{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)}
			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			params.VotePeriod = tc.votePeriod
			params.SlashWindow = tc.slashWindow
			params.TobinTaxes = types.TobinTaxes{tobinTax}
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, tobinTax.Denom, tobinTax.TobinTax))

			if core.IsPeriodLastBlock(s.ctx, tc.votePeriod) {
				powerReduction := math.NewInt(1_000_000)
				validators := []stakingtypes.Validator{
					{
						OperatorAddress: valAddr1.String(),
						Status:          stakingtypes.Bonded,
						Tokens:          powerReduction.MulRaw(10),
					},
					{
						OperatorAddress: valAddr2.String(),
						Status:          stakingtypes.Bonded,
						Tokens:          powerReduction.MulRaw(10),
					},
				}

				for _, valAddr := range []sdk.ValAddress{valAddr1, valAddr2} {
					s.Require().NoError(s.keeper.Vote.Set(s.ctx, valAddr, types.Vote{
						ExchangeRates: types.ExchangeRates{{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)}},
						Voter:         valAddr.String(),
					}))
				}

				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(powerReduction).AnyTimes()
				s.stakingKeeper.EXPECT().
					IterateBondedValidatorsByPower(s.ctx, gomock.Any()).
					DoAndReturn(func(_ context.Context, fn func(int64, stakingtypes.ValidatorI) bool) error {
						for i, validator := range validators {
							if fn(int64(i), validator) {
								break
							}
						}
						return nil
					})
				s.stakingKeeper.EXPECT().
					ValidatorAddressCodec().
					Return(address.NewBech32Codec("cosmosvaloper")).
					AnyTimes()
				s.stakingKeeper.EXPECT().TotalValidatorPower(s.ctx).Return(math.NewInt(2_000_000), nil)

				rewardAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(rewardAcc)
				s.bankKeeper.EXPECT().GetAllBalances(s.ctx, rewardAcc.GetAddress()).Return(
					sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1_000_000))),
				)
				s.stakingKeeper.EXPECT().Validator(s.ctx, gomock.Any()).Return(validators[0], nil).AnyTimes()
				s.distrKeeper.EXPECT().AllocateTokensToValidator(s.ctx, gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				s.bankKeeper.EXPECT().
					SendCoinsFromModuleToModule(s.ctx, types.ModuleName, "distribution", gomock.Any()).
					Return(nil)
			}

			err = s.keeper.EndBlocker(s.ctx)
			s.Require().NoError(err)
		})
	}
}
