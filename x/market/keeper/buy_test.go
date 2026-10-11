package keeper_test

import (
	"context"
	"fmt"
	"math/rand"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// TestQueryBuyFindsTheLeastSufficientOffer runs the reverse quote against the constant-product setup
// the forward quote is pinned on: unit rates and a 400-unit pool, where 100 NOAH buys 80 USD at a
// 20% spread and 99 buys 79, so the pool term of the inverse, not the floor term, decides.
func (s *KeeperTestSuite) TestQueryBuyFindsTheLeastSufficientOffer() {
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

	response, err := s.queryClient.Buy(s.ctx, &types.QueryBuyRequest{AskCoin: "80ausd", OfferDenom: chain.NoahBaseDenom})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), response.OfferCoin)
	s.Require().Equal(sdk.NewInt64Coin("ausd", 80), response.SwapCoin)
	s.Require().True(math.LegacyNewDec(20).Equal(response.SwapFee.Amount), "fee %s", response.SwapFee)

	// The pool cannot hand over its whole depth, and an ask in the offer denomination is recursive.
	_, err = s.queryClient.Buy(s.ctx, &types.QueryBuyRequest{AskCoin: "400ausd", OfferDenom: chain.NoahBaseDenom})
	s.Require().ErrorContains(err, "out of range")
	_, err = s.queryClient.Buy(s.ctx, &types.QueryBuyRequest{AskCoin: "1" + chain.NoahBaseDenom, OfferDenom: chain.NoahBaseDenom})
	s.Require().Error(err)
}

// TestQueryBuyIsTheLeastSufficientOfferAtAnyRate checks the inverse against the forward quote at rates
// and pools that do not divide: the sized offer buys the ask, and one base unit less does not.
func (s *KeeperTestSuite) TestQueryBuyIsTheLeastSufficientOfferAtAnyRate() {
	r := rand.New(rand.NewSource(7))
	var rates oracletypes.RateSet
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, ...string) (oracletypes.RateSet, error) { return rates, nil }).
		AnyTimes()
	rate := func() math.LegacyDec {
		return math.LegacyNewDecWithPrec(r.Int63n(999_999_999_999_999_999)+1, 18).MulInt64(r.Int63n(1_000) + 1)
	}
	sell := func(offer sdk.Coin, askDenom string) (math.Int, error) {
		res, err := s.queryClient.Sell(s.ctx, &types.QuerySellRequest{OfferCoin: offer.String(), AskDenom: askDenom})
		if err != nil {
			return math.Int{}, err
		}
		return res.SwapCoin.Amount, nil
	}
	pairs := [][2]string{
		{chain.NoahBaseDenom, chain.USDBaseDenom},
		{chain.USDBaseDenom, chain.NoahBaseDenom},
		{chain.USDBaseDenom, chain.KRWBaseDenom},
	}

	quoted := 0
	for range 600 {
		rates = oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  rate(),
			chain.KRWBaseDenom:  rate(),
			chain.XDRBaseDenom:  rate(),
		}
		depth := math.LegacyNewDec(r.Int63n(1_000_000_000) + 1_000)
		capacity := types.DefaultConversionPolicy()
		capacity.BasePool = xdrBasePool(depth)
		capacity.MinStabilitySpread = math.LegacyNewDecWithPrec(r.Int63n(50), 3)
		s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))
		s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, depth.MulInt64(r.Int63n(160)-80).QuoInt64(100)))
		pair := pairs[r.Intn(len(pairs))]
		ask := sdk.NewCoin(pair[1], math.NewInt(r.Int63n(1_000_000)+1))

		res, err := s.queryClient.Buy(s.ctx, &types.QueryBuyRequest{AskCoin: ask.String(), OfferDenom: pair[0]})
		if err != nil {
			// An ask the pool cannot fill or a quote out of range is refused, not sized.
			continue
		}
		quoted++
		describe := fmt.Sprintf("%s for %s at %v", ask, res.OfferCoin, rates)
		output, err := sell(res.OfferCoin, pair[1])
		s.Require().NoError(err, describe)
		s.Require().True(output.GTE(ask.Amount), "the offer falls short: %s", describe)
		if res.OfferCoin.Amount.GT(math.OneInt()) {
			less, err := sell(sdk.NewCoin(pair[0], res.OfferCoin.Amount.SubRaw(1)), pair[1])
			if err == nil {
				s.Require().True(less.LT(ask.Amount), "a smaller offer suffices: %s", describe)
			}
		}
	}
	s.Require().Greater(quoted, 500, "most draws must quote, or the property is vacuous")
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
