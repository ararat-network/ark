package keeper_test

import (
	"context"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	s.Run("updates params", func() {
		params := types.DefaultParams()
		params.RewardWindow = 100
		params.RewardDistributionWindow = 1_000
		params.SlashWindow = 200
		s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(7)))
		s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, 3))

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().NoError(err)

		stored, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(uint64(100), stored.RewardWindow)

		accounting, err := s.keeper.Accounting.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(types.DefaultRewardWindow, accounting.RewardWindow)
		s.Require().Equal(types.DefaultRewardDistributionWindow, accounting.RewardDistributionWindow)
		s.Require().Equal(types.DefaultSlashWindow, accounting.SlashWindow)

		rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().True(math.NewInt(7).Equal(rewardWeight))
		missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().Equal(uint64(3), missCount)
	})

	s.Run("rejects invalid authority", func() {
		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: sdk.AccAddress("not-gov").String(),
			Params:    types.DefaultParams(),
		})
		s.Require().Error(err)
	})

	s.Run("rejects invalid params", func() {
		params := types.DefaultParams()
		params.RewardWindow = 0

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().ErrorContains(err, "RewardWindow must be > 0")
	})

	for _, tc := range []struct {
		name   string
		mutate func(*types.Params)
	}{
		{
			name: "rejects removed vote target",
			mutate: func(params *types.Params) {
				params.TobinTaxes = params.TobinTaxes[1:]
			},
		},
		{
			name: "rejects empty vote targets",
			mutate: func(params *types.Params) {
				params.TobinTaxes = nil
			},
		},
	} {
		s.Run(tc.name, func() {
			currentParams, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			currentVoteTargets := types.NewVoteTargets(currentParams)
			s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, currentVoteTargets))

			params := currentParams
			tc.mutate(&params)
			_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
				Authority: authority,
				Params:    params,
			})
			s.Require().ErrorIs(err, types.ErrVoteTargetRemoval)

			storedParams, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(currentParams, storedParams)
			storedVoteTargets, err := s.keeper.VoteTargets.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(currentVoteTargets, storedVoteTargets)
		})
	}

	s.Run("updates market taxes and schedules added vote targets", func() {
		oldVoteTargets := []string{chain.MicroUSDDenom}
		currentParams := types.DefaultParams()
		currentParams.TobinTaxes = []types.TobinTax{
			{Denom: chain.MicroUSDDenom, TobinTax: types.DefaultTobinTax},
		}
		s.Require().NoError(s.keeper.Params.Set(s.ctx, currentParams))
		s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargets{
			Denoms:  oldVoteTargets,
			Version: types.InitialVoteTargetVersion,
		}))
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, newStoredExchangeRate(chain.MicroUSDDenom, math.LegacyOneDec())))

		const newDenom = "uaud"
		params := types.DefaultParams()
		params.TobinTaxes = []types.TobinTax{
			{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			{Denom: newDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
		}
		s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, newDenom).Return(banktypes.Metadata{}, false)
		s.bankKeeper.EXPECT().
			SetDenomMetaData(s.ctx, gomock.Any()).
			Do(func(_ context.Context, metadata banktypes.Metadata) {
				s.Require().Equal(newDenom, metadata.Base)
			})

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().NoError(err)

		hasUSD, err := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroUSDDenom)
		s.Require().NoError(err)
		s.Require().True(hasUSD)

		usdTax, err := s.keeper.GetTobinTax(s.ctx, chain.MicroUSDDenom)
		s.Require().NoError(err)
		s.Require().True(math.LegacyNewDecWithPrec(25, 4).Equal(usdTax))

		audTax, err := s.keeper.GetTobinTax(s.ctx, newDenom)
		s.Require().NoError(err)
		s.Require().True(math.LegacyNewDecWithPrec(50, 4).Equal(audTax))

		state, err := s.keeper.VoteTargets.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(oldVoteTargets, state.Denoms)
		s.Require().NotNil(state.Pending)
		s.Require().Equal(types.InitialVoteTargetVersion+1, state.Pending.Version)
		s.Require().Equal(
			sdk.UnwrapSDKContext(s.ctx).BlockHeight()+types.VoteTargetActivationDelayBlocks,
			state.Pending.ActivationVoteHeight,
		)
		s.Require().Equal([]string{newDenom, chain.MicroUSDDenom}, state.Pending.Denoms)
	})
}
