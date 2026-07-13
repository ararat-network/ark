package keeper_test

import (
	"math/big"

	"go.uber.org/mock/gomock"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	chain "ark/pkg/chain"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestMsgSwap() {
	trader := sdk.AccAddress([]byte("trader_______________")).String()

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
		{
			name: "recursive swap",
			msg: &types.MsgSwap{
				Trader:    trader,
				OfferCoin: sdk.NewCoin("ukrw", math.NewInt(1)),
				AskDenom:  "ukrw",
			},
			expectErr: types.ErrRecursiveSwap,
			errMsg:    "recursive swap",
		},
		{
			name: "zero amount",
			msg: &types.MsgSwap{
				Trader: trader,
				OfferCoin: sdk.Coin{
					Denom:  "uusd",
					Amount: math.ZeroInt(),
				},
				AskDenom: "ukrw",
			},
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "negative amount",
			msg: &types.MsgSwap{
				Trader: trader,
				OfferCoin: sdk.Coin{
					Denom:  "uusd",
					Amount: math.NewInt(-1),
				},
				AskDenom: "ukrw",
			},
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "missing minimum receive",
			msg: &types.MsgSwap{
				Trader:    trader,
				OfferCoin: sdk.NewInt64Coin("uusd", 1000),
				AskDenom:  "ukrw",
			},
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid minimum receive",
		},
		{
			name: "zero minimum receive",
			msg: &types.MsgSwap{
				Trader:         trader,
				OfferCoin:      sdk.NewInt64Coin("uusd", 1000),
				AskDenom:       "ukrw",
				MinimumReceive: sdk.NewCoin("ukrw", math.ZeroInt()),
			},
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "minimum receive denom does not match ask denom",
			msg: &types.MsgSwap{
				Trader:         trader,
				OfferCoin:      sdk.NewInt64Coin("uusd", 1000),
				AskDenom:       "ukrw",
				MinimumReceive: sdk.NewInt64Coin("uusd", 1),
			},
			expectErr: errortypes.ErrInvalidRequest,
			errMsg:    "minimum receive denom",
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

	s.setupArkToArkSwapMocks(fromAddr, toAddr, offerCoin, expectedSwapCoin)

	res, err := s.msgServer.SwapSend(s.ctx, &types.MsgSwapSend{
		FromAddress:    fromAddr.String(),
		ToAddress:      toAddr.String(),
		OfferCoin:      offerCoin,
		AskDenom:       "ukrw",
		MinimumReceive: expectedSwapCoin,
	})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(expectedSwapCoin, res.SwapCoin)
	s.Require().True(expectedSwapFee.Amount.Equal(res.SwapFee.Amount))
	s.requireSwapEvent(fromAddr.String(), toAddr.String(), offerCoin, res.SwapCoin, res.SwapFee.String())
}

func (s *KeeperTestSuite) TestMsgSwapNativeSettlementUsesQuotedState() {
	params := types.DefaultParams()
	params.BasePool = math.LegacyNewDec(400)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tests := []struct {
		name          string
		offerCoin     sdk.Coin
		askDenom      string
		expectedSwap  sdk.Coin
		expectedDelta math.LegacyDec
	}{
		{
			name:          "stablecoin to noah",
			offerCoin:     sdk.NewInt64Coin("uusd", 100),
			askDenom:      chain.MicroNoahDenom,
			expectedSwap:  sdk.NewInt64Coin(chain.MicroNoahDenom, 80),
			expectedDelta: math.LegacyNewDec(100),
		},
		{
			name:          "noah to stablecoin",
			offerCoin:     sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
			askDenom:      "uusd",
			expectedSwap:  sdk.NewInt64Coin("uusd", 80),
			expectedDelta: math.LegacyNewDec(-80),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
			trader := sdk.AccAddress([]byte("trader_______________"))
			s.oracleKeeper.EXPECT().GetRateSnapshot(
				s.ctx,
				tc.offerCoin.Denom,
				chain.MicroSDRDenom,
				tc.askDenom,
			).Return(oracletypes.RateSnapshot{
				"uusd":               math.LegacyOneDec(),
				chain.MicroSDRDenom:  math.LegacyOneDec(),
				chain.MicroNoahDenom: math.LegacyOneDec(),
			}, nil).Times(1)

			gomock.InOrder(
				s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(tc.offerCoin)).Return(nil),
				s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(tc.offerCoin)).Return(nil),
				s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(tc.expectedSwap)).Return(nil),
				s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, trader, sdk.NewCoins(tc.expectedSwap)).Return(nil),
			)

			res, err := s.msgServer.Swap(s.ctx, &types.MsgSwap{
				Trader:         trader.String(),
				OfferCoin:      tc.offerCoin,
				AskDenom:       tc.askDenom,
				MinimumReceive: tc.expectedSwap,
			})
			s.Require().NoError(err)
			s.Require().Equal(tc.expectedSwap, res.SwapCoin)

			delta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(tc.expectedDelta.Equal(delta), "expected delta %s, got %s", tc.expectedDelta, delta)
		})
	}
}

func (s *KeeperTestSuite) TestMsgSwapRejectsMinimumReceiveAboveOutput() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	offerCoin := sdk.NewInt64Coin("uusd", 1000000)
	minimumReceive := sdk.NewInt64Coin("ukrw", 1296750001)

	s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "uusd", chain.MicroSDRDenom, "ukrw").
		Return(oracletypes.RateSnapshot{
			"uusd":              math.LegacyOneDec(),
			chain.MicroSDRDenom: math.LegacyMustNewDecFromStr("1.7"),
			"ukrw":              math.LegacyNewDec(1300),
		}, nil)
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, "uusd").
		Return(math.LegacyMustNewDecFromStr("0.0025"), nil)
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, "ukrw").
		Return(math.LegacyMustNewDecFromStr("0.0025"), nil)

	beforeDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	_, err = s.msgServer.Swap(s.ctx, &types.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      offerCoin,
		AskDenom:       "ukrw",
		MinimumReceive: minimumReceive,
	})
	s.Require().ErrorIs(err, types.ErrMinimumReceiveNotMet)
	s.Require().ErrorContains(err, "minimum 1296750001ukrw, received 1296750000ukrw")

	afterDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(beforeDelta, afterDelta)
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		s.Require().NotEqual(types.EventSwap, event.Type)
	}
}

func (s *KeeperTestSuite) TestMsgSwap_ComputeSwapErrorIncludesContext() {
	tests := []struct {
		name string
		msg  sdk.Msg
	}{
		{
			name: "swap",
			msg: &types.MsgSwap{
				Trader:         sdk.AccAddress([]byte("trader_______________")).String(),
				OfferCoin:      sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:       "ufoo",
				MinimumReceive: sdk.NewInt64Coin("ufoo", 1),
			},
		},
		{
			name: "swap send",
			msg: &types.MsgSwapSend{
				FromAddress:    sdk.AccAddress([]byte("from________________")).String(),
				ToAddress:      sdk.AccAddress([]byte("to__________________")).String(),
				OfferCoin:      sdk.NewCoin("uusd", math.NewInt(1000000)),
				AskDenom:       "ufoo",
				MinimumReceive: sdk.NewInt64Coin("ufoo", 1),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "uusd", chain.MicroSDRDenom, "ufoo").
				Return(nil, oracletypes.ErrUnknownDenom)

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
			name: "zero base pool",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           math.LegacyZeroDec(),
					PoolRecoveryPeriod: 14400,
					MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
				},
			},
			expectErr: "base pool must be positive",
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
			expectErr: "base pool must be positive",
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

func (s *KeeperTestSuite) TestMsgUpdateParamsRejectsNonPositiveEffectivePool() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyNewDec(-200)))

	_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params: types.Params{
			BasePool:           math.LegacyNewDec(100),
			PoolRecoveryPeriod: 14400,
			MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
		},
	})
	s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
	s.Require().ErrorContains(err, "effective ark pool must be positive")

	after, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after)
}

func (s *KeeperTestSuite) TestMsgUpdateParamsRejectsUnrepresentableEffectivePool() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, maxLegacyDecForKeeperTest()))

	s.Require().NotPanics(func() {
		_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    types.DefaultParams(),
		})
	})
	s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
	s.Require().ErrorContains(err, "effective ark pool")

	after, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after)
}

func (s *KeeperTestSuite) setupArkToArkSwapMocks(trader sdk.AccAddress, receiver sdk.AccAddress, offerCoin sdk.Coin, swapCoin sdk.Coin) {
	s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "uusd", chain.MicroSDRDenom, "ukrw").
		Return(oracletypes.RateSnapshot{
			"uusd":              math.LegacyOneDec(),
			chain.MicroSDRDenom: math.LegacyMustNewDecFromStr("1.7"),
			"ukrw":              math.LegacyNewDec(1300),
		}, nil)
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

func maxLegacyDecForKeeperTest() math.LegacyDec {
	precision := new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, precision)
	raw.Sub(raw, big.NewInt(1))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
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
