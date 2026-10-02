package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// TestQuoteBuyFindsTheLeastSufficientOffer runs the reverse quote against the constant-product setup
// the forward quote is pinned on: unit rates and a 400-unit pool, where 100 NOAH buys 80 USD at a
// 20% spread and 99 buys 79, so the pool term of the inverse, not the floor term, decides.
func (s *KeeperTestSuite) TestQuoteBuyFindsTheLeastSufficientOffer() {
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(oracletypes.RateSet{
			"ausd":              math.LegacyOneDec(),
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.XDRBaseDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()
	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = xdrBasePool(math.LegacyNewDec(400))
	capacity.MinStabilitySpread = math.LegacyNewDecWithPrec(2, 2)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))

	offer, output, fee, err := s.keeper.QuoteBuy(s.ctx, sdk.NewInt64Coin("ausd", 80), chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), offer)
	s.Require().Equal(sdk.NewInt64Coin("ausd", 80), output)
	s.Require().True(math.LegacyNewDec(20).Equal(fee.Amount), "fee %s", fee)

	response, err := s.queryClient.Buy(s.ctx, &types.QueryBuyRequest{AskCoin: "80ausd", OfferDenom: chain.NoahBaseDenom})
	s.Require().NoError(err)
	s.Require().Equal(offer, response.OfferCoin)
	s.Require().Equal(output, response.SwapCoin)

	// The pool cannot hand over its whole depth, and an ask in the offer denomination is recursive.
	_, _, _, err = s.keeper.QuoteBuy(s.ctx, sdk.NewInt64Coin("ausd", 400), chain.NoahBaseDenom)
	s.Require().ErrorContains(err, "out of range")
	_, _, _, err = s.keeper.QuoteBuy(s.ctx, sdk.NewInt64Coin(chain.NoahBaseDenom, 1), chain.NoahBaseDenom)
	s.Require().Error(err)
}

// TestBuyTakesTheLeastSufficientOffer runs a buy on the same setup: the trader pays exactly the
// least sufficient offer, a cap below it refuses the buy before any coin moves, a cap in the wrong
// denomination is invalid, and a buy without a cap is refused.
func (s *KeeperTestSuite) TestBuyTakesTheLeastSufficientOffer() {
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(oracletypes.RateSet{
			"ausd":              math.LegacyOneDec(),
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.XDRBaseDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()
	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = xdrBasePool(math.LegacyNewDec(400))
	capacity.MinStabilitySpread = math.LegacyNewDecWithPrec(2, 2)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))
	trader := sdk.AccAddress([]byte("trader_______________"))
	ask := sdk.NewInt64Coin("ausd", 80)

	_, err := s.msgServer.Buy(s.ctx, &types.MsgBuy{Trader: trader.String(), AskCoin: ask, OfferDenom: chain.NoahBaseDenom, MaximumOffer: sdk.NewInt64Coin(chain.NoahBaseDenom, 99)})
	s.Require().ErrorIs(err, types.ErrMaximumOfferExceeded)
	_, err = s.msgServer.Buy(s.ctx, &types.MsgBuy{Trader: trader.String(), AskCoin: ask, OfferDenom: chain.NoahBaseDenom, MaximumOffer: sdk.NewInt64Coin("ausd", 100)})
	s.Require().Error(err)
	_, err = s.msgServer.Buy(s.ctx, &types.MsgBuy{Trader: trader.String(), AskCoin: ask, OfferDenom: chain.NoahBaseDenom})
	s.Require().ErrorContains(err, "maximum offer: not stated")

	s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 100))).Return(nil)
	s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(ask)).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, trader, sdk.NewCoins(ask)).Return(nil)
	res, err := s.msgServer.Buy(s.ctx, &types.MsgBuy{Trader: trader.String(), AskCoin: ask, OfferDenom: chain.NoahBaseDenom, MaximumOffer: sdk.NewInt64Coin(chain.NoahBaseDenom, 100)})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), res.OfferCoin)
	s.Require().Equal(ask, res.SwapCoin)
}
