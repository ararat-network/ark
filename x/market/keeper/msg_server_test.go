package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	core "noah/types"
	"noah/x/market/types"
	oracletypes "noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestMsgSwap() {
	tests := []struct {
		name      string
		msg       *types.MsgSwap
		expectErr error
		errMsg    string
	}{
		{
			name:      "empty address",
			msg:       &types.MsgSwap{},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "invalid trader address",
		},
		{
			name: "malformed address",
			msg: &types.MsgSwap{
				Trader: "address",
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
			s.Require().ErrorContains(err, tc.errMsg)
		})
	}
}

func (s *KeeperTestSuite) TestMsgSwapSend() {
	fromAddr := sdk.AccAddress([]byte("from________________"))

	tests := []struct {
		name      string
		msg       *types.MsgSwapSend
		expectErr error
		errMsg    string
	}{
		{
			name:      "empty from address",
			msg:       &types.MsgSwapSend{},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "invalid from address",
		},
		{
			name: "malformed from address",
			msg: &types.MsgSwapSend{
				FromAddress: "invalid",
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "invalid from address",
		},
		{
			name: "empty to address",
			msg: &types.MsgSwapSend{
				FromAddress: fromAddr.String(),
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "invalid to address",
		},
		{
			name: "malformed to address",
			msg: &types.MsgSwapSend{
				FromAddress: fromAddr.String(),
				ToAddress:   "invalid",
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "invalid to address",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.msgServer.SwapSend(s.ctx, tc.msg)
			s.Require().Error(err)
			s.Require().ErrorIs(err, tc.expectErr)
			s.Require().ErrorContains(err, tc.errMsg)
		})
	}
}

func (s *KeeperTestSuite) TestMsgSwapSend_Success() {
	fromAddr := sdk.AccAddress([]byte("from________________"))
	toAddr := sdk.AccAddress([]byte("to__________________"))
	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000000))
	expectedSwapCoin := sdk.NewCoin("ukrw", math.NewInt(1296750000))
	expectedSwapFee := sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(3250000))

	s.setupNoahToNoahSwapMocks(fromAddr, toAddr, offerCoin, expectedSwapCoin)

	res, err := s.msgServer.SwapSend(s.ctx, &types.MsgSwapSend{
		FromAddress: fromAddr.String(),
		ToAddress:   toAddr.String(),
		OfferCoin:   offerCoin,
		AskDenom:    "ukrw",
	})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(expectedSwapCoin, res.SwapCoin)
	s.Require().True(expectedSwapFee.Amount.Equal(res.SwapFee.Amount))
	s.requireSwapEvent(fromAddr.String(), toAddr.String(), offerCoin, res.SwapCoin, res.SwapFee.String())
}

func (s *KeeperTestSuite) TestMsgSwap_ComputeSwapErrorIncludesContext() {
	tests := []struct {
		name string
		msg  sdk.Msg
	}{
		{
			name: "swap",
			msg: &types.MsgSwap{
				Trader:    sdk.AccAddress([]byte("trader_______________")).String(),
				OfferCoin: sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:  "ufoo",
			},
		},
		{
			name: "swap send",
			msg: &types.MsgSwapSend{
				FromAddress: sdk.AccAddress([]byte("from________________")).String(),
				ToAddress:   sdk.AccAddress([]byte("to__________________")).String(),
				OfferCoin:   sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:    "ufoo",
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "uusd").Return(math.LegacyOneDec(), nil)
			s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, core.MicroSDRDenom).
				Return(math.LegacyMustNewDecFromStr("1.7"), nil).Times(2)
			s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "ufoo").
				Return(math.LegacyZeroDec(), oracletypes.ErrUnknownDenom)

			var err error
			switch msg := tc.msg.(type) {
			case *types.MsgSwap:
				_, err = s.msgServer.Swap(s.ctx, msg)
			case *types.MsgSwapSend:
				_, err = s.msgServer.SwapSend(s.ctx, msg)
			}

			s.Require().Error(err)
			s.Require().ErrorIs(err, types.ErrNoEffectivePrice)
			s.Require().ErrorContains(err, "computing swap from 1000000uusd to ufoo")
		})
	}
}

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	consensusAuthority := authtypes.NewModuleAddress("consensus").String()

	tests := []struct {
		name        string
		setup       func()
		msg         *types.MsgUpdateParams
		expectErr   string
		expectErrIs error
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
			name: "consensus params authority overrides keeper authority",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
					Authority: &cmtproto.AuthorityParams{Authority: consensusAuthority},
				})
			},
			msg: &types.MsgUpdateParams{
				Authority: consensusAuthority,
				Params: types.Params{
					BasePool:           math.LegacyNewDec(3000000000000),
					PoolRecoveryPeriod: 28800,
					MinStabilitySpread: math.LegacyNewDecWithPrec(5, 2),
				},
			},
		},
		{
			name: "invalid authority",
			msg: &types.MsgUpdateParams{
				Authority: "invalid_authority",
				Params:    types.DefaultParams(),
			},
			expectErr:   "invalid authority",
			expectErrIs: errortypes.ErrUnauthorized,
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
			ctx := s.ctx
			defer func() { s.ctx = ctx }()
			if tc.setup != nil {
				tc.setup()
			}

			_, err := s.msgServer.UpdateParams(s.ctx, tc.msg)
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
				if tc.expectErrIs != nil {
					s.Require().ErrorIs(err, tc.expectErrIs)
				}
			} else {
				s.Require().NoError(err)

				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				s.Require().Equal(tc.msg.Params, params)
			}
		})
	}
}

func (s *KeeperTestSuite) setupNoahToNoahSwapMocks(trader sdk.AccAddress, receiver sdk.AccAddress, offerCoin sdk.Coin, swapCoin sdk.Coin) {
	s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "uusd").
		Return(math.LegacyOneDec(), nil)
	s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, core.MicroSDRDenom).
		Return(math.LegacyMustNewDecFromStr("1.7"), nil).Times(2)
	s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "ukrw").
		Return(math.LegacyNewDec(1300), nil)
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, "uusd").
		Return(math.LegacyMustNewDecFromStr("0.0025"), nil)
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, "ukrw").
		Return(math.LegacyMustNewDecFromStr("0.0025"), nil)

	gomock.InOrder(
		s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(swapCoin)).Return(nil),
		s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, receiver, sdk.NewCoins(swapCoin)).Return(nil),
	)
}

func (s *KeeperTestSuite) requireSwapEvent(trader string, recipient string, offer sdk.Coin, swapCoin sdk.Coin, swapFee string) {
	var swapEvent sdk.Event
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		if event.Type == types.EventSwap {
			swapEvent = event
			break
		}
	}

	s.Require().Equal(types.EventSwap, swapEvent.Type, "swap event not emitted")
	attrs := make(map[string]string, len(swapEvent.Attributes))
	for _, attr := range swapEvent.Attributes {
		attrs[attr.Key] = attr.Value
	}

	s.Require().Equal(offer.String(), attrs[types.AttributeKeyOffer])
	s.Require().Equal(trader, attrs[types.AttributeKeyTrader])
	s.Require().Equal(recipient, attrs[types.AttributeKeyRecipient])
	s.Require().Equal(swapCoin.String(), attrs[types.AttributeKeySwapCoin])
	s.Require().Equal(swapFee, attrs[types.AttributeKeySwapFee])
}
