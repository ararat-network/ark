package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
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

	s.Run("stores tobin tax params without syncing active targets", func() {
		s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, chain.MicroKRWDenom, math.LegacyNewDecWithPrec(25, 4)))
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, newStoredExchangeRate(chain.MicroUSDDenom, math.LegacyOneDec())))
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroKRWDenom, newStoredExchangeRate(chain.MicroKRWDenom, math.LegacyOneDec())))

		params := types.DefaultParams()
		params.TobinTaxes = types.TobinTaxes{
			{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		}

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().NoError(err)

		hasUSD, err := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroUSDDenom)
		s.Require().NoError(err)
		s.Require().True(hasUSD)

		hasKRW, err := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroKRWDenom)
		s.Require().NoError(err)
		s.Require().True(hasKRW)

		hasUSDTarget, err := s.keeper.TobinTax.Has(s.ctx, chain.MicroUSDDenom)
		s.Require().NoError(err)
		s.Require().False(hasUSDTarget)

		hasKRWTarget, err := s.keeper.TobinTax.Has(s.ctx, chain.MicroKRWDenom)
		s.Require().NoError(err)
		s.Require().True(hasKRWTarget)
	})
}
