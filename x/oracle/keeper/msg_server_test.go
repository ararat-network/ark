package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	s.Run("updates params", func() {
		params := types.DefaultParams()
		params.RewardWindow = 100

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().NoError(err)

		stored, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(uint64(100), stored.RewardWindow)
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

	s.Run("applies tobin tax changes", func() {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, newStoredExchangeRate(core.MicroUSDDenom, math.LegacyOneDec())))
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, newStoredExchangeRate(core.MicroKRWDenom, math.LegacyOneDec())))

		params := types.DefaultParams()
		params.TobinTaxes = types.TobinTaxes{
			{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		}

		s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, true)

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().NoError(err)

		hasUSD, err := s.keeper.ExchangeRate.Has(s.ctx, core.MicroUSDDenom)
		s.Require().NoError(err)
		s.Require().True(hasUSD)

		hasKRW, err := s.keeper.ExchangeRate.Has(s.ctx, core.MicroKRWDenom)
		s.Require().NoError(err)
		s.Require().False(hasKRW)
	})
}
