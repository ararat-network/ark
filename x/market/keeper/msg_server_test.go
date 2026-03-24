package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	core "noah/types"
	"noah/x/market/types"
)

// setupNoahToNoahSwapMocks configures oracle and bank mocks for a Noah→Noah (uusd→ukrw) swap.
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

	// Verify swap event emitted with correct attributes
	var swapEvent sdk.Event
	sdkCtx := sdk.UnwrapSDKContext(s.ctx)
	for _, e := range sdkCtx.EventManager().Events() {
		if e.Type == types.EventSwap {
			swapEvent = e
			break
		}
	}
	s.Require().Equal(types.EventSwap, swapEvent.Type, "swap event not emitted")
	attrMap := make(map[string]string)
	for _, attr := range swapEvent.Attributes {
		attrMap[attr.Key] = attr.Value
	}
	s.Require().Equal(msg.OfferCoin.String(), attrMap[types.AttributeKeyOffer])
	s.Require().Equal(trader.String(), attrMap[types.AttributeKeyTrader])
	s.Require().Equal(trader.String(), attrMap[types.AttributeKeyRecipient])
	s.Require().Equal(res.SwapCoin.String(), attrMap[types.AttributeKeySwapCoin])
	s.Require().Equal(res.SwapFee.String(), attrMap[types.AttributeKeySwapFee])
}

func (s *KeeperTestSuite) TestMsgSwap_Errors() {
	trader := sdk.AccAddress([]byte("trader______________"))

	tests := []struct {
		name      string
		msg       *types.MsgSwap
		expectErr error
		errMsg    string
	}{
		{
			name: "recursive swap",
			msg: &types.MsgSwap{
				Trader:    trader.String(),
				OfferCoin: sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:  "uusd",
			},
			expectErr: types.ErrRecursiveSwap,
		},
		{
			name: "zero amount",
			msg: &types.MsgSwap{
				Trader:    trader.String(),
				OfferCoin: sdk.NewCoin("uusd", math.ZeroInt()),
				AskDenom:  "ukrw",
			},
			expectErr: errortypes.ErrInvalidCoins,
		},
		{
			name: "invalid trader address",
			msg: &types.MsgSwap{
				Trader:    "invalid",
				OfferCoin: sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:  "ukrw",
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "invalid trader address",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.msgServer.Swap(s.ctx, tc.msg)
			s.Require().Error(err)
			s.Require().ErrorIs(err, tc.expectErr)
			if tc.errMsg != "" {
				s.Require().ErrorContains(err, tc.errMsg)
			}
		})
	}
}

func (s *KeeperTestSuite) TestMsgSwap_FeeDeduction() {
	// Unit rates (1:1:1) with a small base pool so CP spread is significant.
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(gomock.Any(), gomock.Any(), types.ModuleName, gomock.Any()).
		Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().BurnCoins(gomock.Any(), types.ModuleName, gomock.Any()).
		Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().MintCoins(gomock.Any(), types.ModuleName, gomock.Any()).
		Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(gomock.Any(), types.ModuleName, gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()

	// BasePool=400 with offer=100 gives CP spread = 100/500 = 0.2
	err := s.keeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(400),
		PoolRecoveryPeriod: 14400,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
	})
	s.Require().NoError(err)

	trader := sdk.AccAddress([]byte("trader______________"))
	msg := &types.MsgSwap{
		Trader:    trader.String(),
		OfferCoin: sdk.NewCoin("uusd", math.NewInt(100)),
		AskDenom:  core.MicroArkDenom,
	}

	res, err := s.msgServer.Swap(s.ctx, msg)
	s.Require().NoError(err)

	// Oracle return: 100 uark. Spread: 0.2.
	// swapFee = 0.2 * 100 = 20 uark
	// swapCoin = 100 - 20 = 80 uark
	s.Require().Equal(core.MicroArkDenom, res.SwapCoin.Denom)
	s.Require().Equal(math.NewInt(80), res.SwapCoin.Amount)
	s.Require().Equal(core.MicroArkDenom, res.SwapFee.Denom)
	s.Require().True(res.SwapFee.Amount.Equal(math.LegacyNewDec(20)))

	// SwapCoin + SwapFee = oracle return amount (100)
	total := math.LegacyNewDecFromInt(res.SwapCoin.Amount).Add(res.SwapFee.Amount)
	s.Require().True(total.Equal(math.LegacyNewDec(100)))
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

	// Verify swap event: trader=from, recipient=to
	var swapEvent sdk.Event
	sdkCtx := sdk.UnwrapSDKContext(s.ctx)
	for _, e := range sdkCtx.EventManager().Events() {
		if e.Type == types.EventSwap {
			swapEvent = e
			break
		}
	}
	s.Require().Equal(types.EventSwap, swapEvent.Type, "swap event not emitted")
	attrMap := make(map[string]string)
	for _, attr := range swapEvent.Attributes {
		attrMap[attr.Key] = attr.Value
	}
	s.Require().Equal(fromAddr.String(), attrMap[types.AttributeKeyTrader])
	s.Require().Equal(toAddr.String(), attrMap[types.AttributeKeyRecipient])
}

func (s *KeeperTestSuite) TestMsgSwapSend_Errors() {
	fromAddr := sdk.AccAddress([]byte("from________________"))
	toAddr := sdk.AccAddress([]byte("to__________________"))

	tests := []struct {
		name   string
		msg    *types.MsgSwapSend
		errMsg string
	}{
		{
			name: "invalid from address",
			msg: &types.MsgSwapSend{
				FromAddress: "invalid",
				ToAddress:   toAddr.String(),
				OfferCoin:   sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:    "ukrw",
			},
			errMsg: "invalid from address",
		},
		{
			name: "invalid to address",
			msg: &types.MsgSwapSend{
				FromAddress: fromAddr.String(),
				ToAddress:   "invalid",
				OfferCoin:   sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:    "ukrw",
			},
			errMsg: "invalid to address",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.msgServer.SwapSend(s.ctx, tc.msg)
			s.Require().Error(err)
			s.Require().ErrorIs(err, errortypes.ErrInvalidAddress)
			s.Require().ErrorContains(err, tc.errMsg)
		})
	}
}

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	tests := []struct {
		name      string
		msg       *types.MsgUpdateParams
		expectErr string
	}{
		{
			name: "valid params",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           math.LegacyNewDec(2000000000000),
					PoolRecoveryPeriod: 28800,
					MinStabilitySpread: math.LegacyNewDecWithPrec(5, 2),
				},
			},
		},
		{
			name: "zero base pool is valid",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           math.LegacyZeroDec(),
					PoolRecoveryPeriod: 14400,
					MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
				},
			},
		},
		{
			name: "invalid authority",
			msg: &types.MsgUpdateParams{
				Authority: "invalid_authority",
				Params:    types.DefaultParams(),
			},
			expectErr: "invalid authority",
		},
		{
			name: "negative base pool",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           math.LegacyNewDec(-1),
					PoolRecoveryPeriod: 14400,
					MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
				},
			},
			expectErr: "base pool must be positive or zero",
		},
		{
			name: "zero recovery period",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           math.LegacyNewDec(1000000000000),
					PoolRecoveryPeriod: 0,
					MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
				},
			},
			expectErr: "pool recovery period must be positive",
		},
		{
			name: "negative min stability spread",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           math.LegacyNewDec(1000000000000),
					PoolRecoveryPeriod: 14400,
					MinStabilitySpread: math.LegacyNewDec(-1),
				},
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name: "min stability spread greater than 1",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           math.LegacyNewDec(1000000000000),
					PoolRecoveryPeriod: 14400,
					MinStabilitySpread: math.LegacyNewDecWithPrec(11, 1), // 1.1
				},
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.msgServer.UpdateParams(s.ctx, tc.msg)
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
			} else {
				s.Require().NoError(err)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				s.Require().Equal(tc.msg.Params, params)
			}
		})
	}
}
