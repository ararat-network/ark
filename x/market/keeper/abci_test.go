package keeper_test

import (
	"context"
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

// TestEndBlockerIdleBlockHandsOverZeroTotals pins what an idle block sends:
// initialised zeros rather than nil, which is what lets Treasury decline before
// it values anything. IsZero is the assertion — a nil field panics there rather
// than reporting true, and that panic would be in an EndBlocker.
func (s *KeeperTestSuite) TestEndBlockerIdleBlockHandsOverZeroTotals() {
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))

	s.treasuryKeeper.EXPECT().
		SettleConversions(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, totals types.ConversionTotals) (math.Int, error) {
			s.Require().NoError(totals.Validate())
			s.Require().True(totals.IsZero())
			return math.ZeroInt(), nil
		})

	// Nothing is burned: the bank mock fails on any unexpected call.
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
}

// TestEndBlockerBurnsWhatSettlementReturns covers the half of the contract
// Market owns: Treasury places the principal and says what is left to destroy,
// and Market destroys exactly that from its own account.
func (s *KeeperTestSuite) TestEndBlockerBurnsWhatSettlementReturns() {
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
	s.recordExpansionForTest(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), sdk.NewInt64Coin(chain.USDBaseDenom, 80))

	gomock.InOrder(
		s.treasuryKeeper.EXPECT().
			SettleConversions(gomock.Any(), gomock.Any()).
			Return(math.NewInt(15), nil),
		s.bankKeeper.EXPECT().
			BurnCoins(s.ctx, types.ModuleName, chain.NoahCoins(math.NewInt(15))).
			Return(nil),
	)

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	s.endBlock()
}

// TestEndBlockerSkipsAZeroBurn covers the block whose principal filled every
// gap: settlement returns nothing to destroy, and Market must not ask Bank to
// burn a zero coin.
func (s *KeeperTestSuite) TestEndBlockerSkipsAZeroBurn() {
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
	s.recordExpansionForTest(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), sdk.NewInt64Coin(chain.USDBaseDenom, 80))

	s.treasuryKeeper.EXPECT().
		SettleConversions(gomock.Any(), gomock.Any()).
		Return(math.ZeroInt(), nil)

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	s.endBlock()
}

// TestEndBlockerPropagatesSettlementFailure pins the halt classification: there
// is no transaction left to abort at settlement, so a failure fails the block
// rather than being swallowed to keep producing.
func (s *KeeperTestSuite) TestEndBlockerPropagatesSettlementFailure() {
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
	s.recordExpansionForTest(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), sdk.NewInt64Coin(chain.USDBaseDenom, 80))

	s.treasuryKeeper.EXPECT().
		SettleConversions(gomock.Any(), gomock.Any()).
		Return(math.Int{}, errors.New("injected settlement failure"))

	err := s.keeper.EndBlocker(s.ctx)
	s.Require().ErrorContains(err, "settling the block's conversions")
	s.endBlock()
}

// recordExpansionForTest books one expansion through the ordinary swap path so
// the accumulators hold a block's worth of flow. The spread burn is stubbed
// because it is the conversion's own business, not the EndBlocker's.
func (s *KeeperTestSuite) recordExpansionForTest(offer sdk.Coin, output sdk.Coin) {
	trader := sdk.AccAddress([]byte("trader_______________"))
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.SDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	s.oracleKeeper.EXPECT().GetRateSet(
		s.ctx, offer.Denom, chain.SDRBaseDenom, output.Denom,
	).Return(rates, nil)
	s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
		s.ctx, trader, types.ModuleName, sdk.NewCoins(offer),
	).Return(nil)
	s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, gomock.Any()).Return(nil)
	s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, gomock.Any()).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
		s.ctx, types.ModuleName, trader, gomock.Any(),
	).Return(nil)

	_, err := s.msgServer.Swap(s.ctx, &types.MsgSwap{
		Trader:    trader.String(),
		OfferCoin: offer,
		AskDenom:  output.Denom,
	})
	s.Require().NoError(err)
}
