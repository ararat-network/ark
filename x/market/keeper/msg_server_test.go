package keeper_test

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"go.uber.org/mock/gomock"

	core "noah/types"
	"noah/x/market/types"
)

// setupSwapMocks configures oracle and bank mocks for a Noah→Noah (uusd→ukrw) swap.
// This is a helper to avoid repeating mock setup across swap tests.
func (s *KeeperTestSuite) setupNoahToNoahSwapMocks() {
	// Oracle rates
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "ukrw").
		Return(math.LegacyNewDec(1300), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	// Tobin taxes for noah→noah
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "uusd").
		Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "ukrw").
		Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes()

	// Bank operations - accept any calls
	s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(gomock.Any(), gomock.Any(), types.ModuleName, gomock.Any()).
		Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().BurnCoins(gomock.Any(), types.ModuleName, gomock.Any()).
		Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().MintCoins(gomock.Any(), types.ModuleName, gomock.Any()).
		Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(gomock.Any(), types.ModuleName, gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), types.ModuleName, gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()
}

func (s *KeeperTestSuite) TestMsgSwap() {
	s.setupNoahToNoahSwapMocks()

	trader := sdk.AccAddress([]byte("trader______________"))
	msg := &types.MsgSwap{
		Trader:    trader.String(),
		OfferCoin: sdk.NewCoin("uusd", math.NewInt(1000000)),
		AskDenom:  "ukrw",
	}

	res, err := s.msgServer.Swap(s.ctx, msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal("ukrw", res.SwapCoin.Denom)
	s.Require().True(res.SwapCoin.Amount.IsPositive())
}

func (s *KeeperTestSuite) TestMsgSwap_RecursiveSwap() {
	trader := sdk.AccAddress([]byte("trader______________"))
	msg := &types.MsgSwap{
		Trader:    trader.String(),
		OfferCoin: sdk.NewCoin("uusd", math.NewInt(1000000)),
		AskDenom:  "uusd",
	}

	_, err := s.msgServer.Swap(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorIs(err, types.ErrRecursiveSwap)
}

func (s *KeeperTestSuite) TestMsgSwap_ZeroAmount() {
	// Zero amount is caught by ValidateBasic, not the handler
	trader := sdk.AccAddress([]byte("trader______________"))
	msg := &types.MsgSwap{
		Trader:    trader.String(),
		OfferCoin: sdk.NewCoin("uusd", math.ZeroInt()),
		AskDenom:  "ukrw",
	}

	s.Require().Error(msg.ValidateBasic())
}

func (s *KeeperTestSuite) TestMsgSwap_InvalidAddress() {
	msg := &types.MsgSwap{
		Trader:    "invalid",
		OfferCoin: sdk.NewCoin("uusd", math.NewInt(1000000)),
		AskDenom:  "ukrw",
	}

	_, err := s.msgServer.Swap(s.ctx, msg)
	s.Require().Error(err)
}

func (s *KeeperTestSuite) TestMsgSwapSend() {
	s.setupNoahToNoahSwapMocks()

	fromAddr := sdk.AccAddress([]byte("from________________"))
	toAddr := sdk.AccAddress([]byte("to__________________"))

	msg := &types.MsgSwapSend{
		FromAddress: fromAddr.String(),
		ToAddress:   toAddr.String(),
		OfferCoin:   sdk.NewCoin("uusd", math.NewInt(1000000)),
		AskDenom:    "ukrw",
	}

	res, err := s.msgServer.SwapSend(s.ctx, msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal("ukrw", res.SwapCoin.Denom)
	s.Require().True(res.SwapCoin.Amount.IsPositive())
}

func (s *KeeperTestSuite) TestMsgSwapSend_InvalidFromAddress() {
	toAddr := sdk.AccAddress([]byte("to__________________"))
	msg := &types.MsgSwapSend{
		FromAddress: "invalid",
		ToAddress:   toAddr.String(),
		OfferCoin:   sdk.NewCoin("uusd", math.NewInt(1000000)),
		AskDenom:    "ukrw",
	}

	_, err := s.msgServer.SwapSend(s.ctx, msg)
	s.Require().Error(err)
}

func (s *KeeperTestSuite) TestMsgSwapSend_InvalidToAddress() {
	fromAddr := sdk.AccAddress([]byte("from________________"))
	msg := &types.MsgSwapSend{
		FromAddress: fromAddr.String(),
		ToAddress:   "invalid",
		OfferCoin:   sdk.NewCoin("uusd", math.NewInt(1000000)),
		AskDenom:    "ukrw",
	}

	_, err := s.msgServer.SwapSend(s.ctx, msg)
	s.Require().Error(err)
}

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	newParams := types.Params{
		BasePool:           math.LegacyNewDec(2000000000000),
		PoolRecoveryPeriod: 28800,
		MinStabilitySpread: math.LegacyNewDecWithPrec(5, 2),
	}

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    newParams,
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().NoError(err)

	// Verify params were updated
	params, err := s.marketKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(newParams.BasePool.Equal(params.BasePool))
	s.Require().Equal(newParams.PoolRecoveryPeriod, params.PoolRecoveryPeriod)
	s.Require().True(newParams.MinStabilitySpread.Equal(params.MinStabilitySpread))
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidAuthority() {
	msg := &types.MsgUpdateParams{
		Authority: "invalid_authority",
		Params:    types.DefaultParams(),
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "invalid authority")
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// Negative base pool
	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params: types.Params{
			BasePool:           math.LegacyNewDec(-1),
			PoolRecoveryPeriod: 14400,
			MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
		},
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
}
