package keeper_test

import (
	"errors"
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
	treasurytypes "ark/x/treasury/types"
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
				Trader:         trader,
				OfferCoin:      sdk.NewCoin("ukrw", math.NewInt(1)),
				AskDenom:       "ukrw",
				MinimumReceive: sdk.NewInt64Coin("ukrw", 1),
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
				AskDenom:       "ukrw",
				MinimumReceive: sdk.NewInt64Coin("ukrw", 1),
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
				AskDenom:       "ukrw",
				MinimumReceive: sdk.NewInt64Coin("ukrw", 1),
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
	s.requireSwapEvent(fromAddr.String(), toAddr.String(), offerCoin, res.SwapCoin, res.SwapFee)
}

func (s *KeeperTestSuite) TestMsgSwapNativeSettlementUsesQuotedState() {
	params := types.DefaultParams()
	params.BasePool = sdrBasePool(math.LegacyNewDec(400))
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tests := []struct {
		name          string
		offerCoin     sdk.Coin
		askDenom      string
		expectedSwap  sdk.Coin
		expectedDelta math.LegacyDec
		expectedMint  sdk.Coin
		setupTreasury func(oracletypes.RateSnapshot) *gomock.Call
	}{
		{
			name:          "stablecoin to noah",
			offerCoin:     sdk.NewInt64Coin("uusd", 100),
			askDenom:      chain.MicroNoahDenom,
			expectedSwap:  sdk.NewInt64Coin(chain.MicroNoahDenom, 80),
			expectedDelta: math.LegacyNewDec(100),
			expectedMint:  sdk.NewInt64Coin(chain.MicroNoahDenom, 60),
			setupTreasury: func(rates oracletypes.RateSnapshot) *gomock.Call {
				return s.treasuryKeeper.EXPECT().DrawRedemptionBuffer(
					s.ctx,
					sdk.NewInt64Coin(chain.MicroUSDDenom, 100),
					math.NewInt(80),
					rates,
				).Return(treasurytypes.BufferDraw{BufferPaid: math.NewInt(20)}, nil)
			},
		},
		{
			name:          "noah to stablecoin",
			offerCoin:     sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
			askDenom:      "uusd",
			expectedSwap:  sdk.NewInt64Coin("uusd", 80),
			expectedDelta: math.LegacyNewDec(-80),
			expectedMint:  sdk.NewInt64Coin("uusd", 80),
			setupTreasury: func(rates oracletypes.RateSnapshot) *gomock.Call {
				return s.treasuryKeeper.EXPECT().RouteExpansion(
					s.ctx,
					sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
					sdk.NewInt64Coin(chain.MicroUSDDenom, 80),
					rates,
				).Return(treasurytypes.ExpansionAllocation{
					EligiblePrincipalNoah:   math.NewInt(80),
					RedemptionBufferCredit:  math.ZeroInt(),
					StrategicReserveCredit:  math.ZeroInt(),
					InsuranceCredit:         math.ZeroInt(),
					SpreadAndDustBurn:       math.NewInt(20),
					OverflowBurn:            math.NewInt(80),
					TargetValuationComplete: true,
				}, nil)
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
			trader := sdk.AccAddress([]byte("trader_______________"))
			rates := oracletypes.RateSnapshot{
				"uusd":               math.LegacyOneDec(),
				chain.MicroSDRDenom:  math.LegacyOneDec(),
				chain.MicroNoahDenom: math.LegacyOneDec(),
			}
			s.oracleKeeper.EXPECT().GetRateSnapshot(
				s.ctx,
				tc.offerCoin.Denom,
				chain.MicroSDRDenom,
				tc.askDenom,
			).Return(rates, nil).Times(1)

			treasuryCall := tc.setupTreasury(rates)
			burned := tc.offerCoin
			if tc.offerCoin.Denom == chain.MicroNoahDenom {
				burned = sdk.NewInt64Coin(chain.MicroNoahDenom, 100)
			}
			recordCall := s.treasuryKeeper.EXPECT().RecordSupplyChange(
				s.ctx,
				burned,
				tc.expectedMint,
				rates,
			).Return(nil)

			gomock.InOrder(
				s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(tc.offerCoin)).Return(nil),
				treasuryCall,
				s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(tc.offerCoin)).Return(nil),
				s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(tc.expectedMint)).Return(nil),
				recordCall,
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

func (s *KeeperTestSuite) TestMsgSwapSettlementErrorsPropagate() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	injectedErr := errors.New("injected settlement failure")
	stableOffer := sdk.NewInt64Coin(chain.MicroUSDDenom, 1_000_000)
	stableOutput := sdk.NewInt64Coin(chain.MicroKRWDenom, 1_296_750_000)
	stableRates := oracletypes.RateSnapshot{
		chain.MicroUSDDenom: math.LegacyOneDec(),
		chain.MicroKRWDenom: math.LegacyNewDec(1300),
	}
	nativeRates := oracletypes.RateSnapshot{
		chain.MicroUSDDenom:  math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacyOneDec(),
		chain.MicroNoahDenom: math.LegacyOneDec(),
	}

	tests := []struct {
		name      string
		offerCoin sdk.Coin
		askDenom  string
		setup     func()
	}{
		{
			name:      "user transfer",
			offerCoin: stableOffer,
			askDenom:  chain.MicroKRWDenom,
			setup: func() {
				s.expectStableToStableQuote(stableRates)
				s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
					s.ctx, trader, types.ModuleName, sdk.NewCoins(stableOffer),
				).Return(injectedErr)
			},
		},
		{
			name:      "burn",
			offerCoin: stableOffer,
			askDenom:  chain.MicroKRWDenom,
			setup: func() {
				s.expectStableToStableQuote(stableRates)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(nil),
					s.bankKeeper.EXPECT().BurnCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(injectedErr),
				)
			},
		},
		{
			name:      "mint",
			offerCoin: stableOffer,
			askDenom:  chain.MicroKRWDenom,
			setup: func() {
				s.expectStableToStableQuote(stableRates)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(nil),
					s.bankKeeper.EXPECT().BurnCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(nil),
					s.bankKeeper.EXPECT().MintCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(stableOutput),
					).Return(injectedErr),
				)
			},
		},
		{
			name:      "supply accounting",
			offerCoin: stableOffer,
			askDenom:  chain.MicroKRWDenom,
			setup: func() {
				s.expectStableToStableQuote(stableRates)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(nil),
					s.bankKeeper.EXPECT().BurnCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(nil),
					s.bankKeeper.EXPECT().MintCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(stableOutput),
					).Return(nil),
					s.treasuryKeeper.EXPECT().RecordSupplyChange(
						s.ctx, stableOffer, stableOutput, stableRates,
					).Return(injectedErr),
				)
			},
		},
		{
			name:      "recipient payout",
			offerCoin: stableOffer,
			askDenom:  chain.MicroKRWDenom,
			setup: func() {
				s.expectStableToStableQuote(stableRates)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(nil),
					s.bankKeeper.EXPECT().BurnCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(stableOffer),
					).Return(nil),
					s.bankKeeper.EXPECT().MintCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(stableOutput),
					).Return(nil),
					s.treasuryKeeper.EXPECT().RecordSupplyChange(
						s.ctx, stableOffer, stableOutput, stableRates,
					).Return(nil),
					s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
						s.ctx, types.ModuleName, trader, sdk.NewCoins(stableOutput),
					).Return(injectedErr),
				)
			},
		},
		{
			name:      "expansion routing",
			offerCoin: sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
			askDenom:  chain.MicroUSDDenom,
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSnapshot(
					s.ctx,
					chain.MicroNoahDenom,
					chain.MicroSDRDenom,
					chain.MicroUSDDenom,
				).Return(nativeRates, nil)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 100)),
					).Return(nil),
					s.treasuryKeeper.EXPECT().RouteExpansion(
						s.ctx,
						sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
						sdk.NewInt64Coin(chain.MicroUSDDenom, 80),
						nativeRates,
					).Return(treasurytypes.ExpansionAllocation{}, injectedErr),
				)
			},
		},
		{
			name:      "redemption buffer draw",
			offerCoin: sdk.NewInt64Coin(chain.MicroUSDDenom, 100),
			askDenom:  chain.MicroNoahDenom,
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSnapshot(
					s.ctx,
					chain.MicroUSDDenom,
					chain.MicroSDRDenom,
					chain.MicroNoahDenom,
				).Return(nativeRates, nil)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)),
					).Return(nil),
					s.treasuryKeeper.EXPECT().DrawRedemptionBuffer(
						s.ctx,
						sdk.NewInt64Coin(chain.MicroUSDDenom, 100),
						math.NewInt(80),
						nativeRates,
					).Return(treasurytypes.BufferDraw{}, injectedErr),
				)
			},
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			params := types.DefaultParams()
			if test.offerCoin.Denom == chain.MicroNoahDenom || test.askDenom == chain.MicroNoahDenom {
				params.BasePool = sdrBasePool(math.LegacyNewDec(400))
			}
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
			test.setup()

			_, err := s.msgServer.Swap(s.ctx, &types.MsgSwap{
				Trader:         trader.String(),
				OfferCoin:      test.offerCoin,
				AskDenom:       test.askDenom,
				MinimumReceive: sdk.NewInt64Coin(test.askDenom, 1),
			})
			s.Require().ErrorIs(err, injectedErr)
		})
	}
}

func (s *KeeperTestSuite) TestMsgSwapRejectsMinimumReceiveAboveOutput() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	offerCoin := sdk.NewInt64Coin("uusd", 1000000)
	minimumReceive := sdk.NewInt64Coin("ukrw", 1296750001)

	s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "uusd", "ukrw").
		Return(oracletypes.RateSnapshot{
			"uusd": math.LegacyOneDec(),
			"ukrw": math.LegacyNewDec(1300),
		}, nil)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: "uusd", TobinTax: math.LegacyMustNewDecFromStr("0.0025")},
		{Denom: "ukrw", TobinTax: math.LegacyMustNewDecFromStr("0.0025")},
	}, nil)

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
	swapEvent, err := sdk.TypedEventToEvent(&types.EventSwap{})
	s.Require().NoError(err)
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		s.Require().NotEqual(swapEvent.Type, event.Type)
	}
}

func (s *KeeperTestSuite) TestMsgSwap_QuoteErrorIncludesContext() {
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
			s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "uusd", "ufoo").
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
					BasePool:           sdrBasePool(math.LegacyNewDec(2000000000000)),
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
					BasePool:           sdrBasePool(math.LegacyZeroDec()),
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
					BasePool:           sdrBasePool(math.LegacyNewDec(3000000000000)),
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
					BasePool: sdk.DecCoin{
						Denom:  chain.MicroSDRDenom,
						Amount: math.LegacyNewDec(-1),
					},
					PoolRecoveryPeriod: 14400,
					MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
				},
			},
			expectErr: "invalid base pool",
		},
		{
			name: "zero recovery period",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
					BasePool:           sdrBasePool(math.LegacyNewDec(1000000000000)),
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
					BasePool:           sdrBasePool(math.LegacyNewDec(1000000000000)),
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
					BasePool:           sdrBasePool(math.LegacyNewDec(1000000000000)),
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

func (s *KeeperTestSuite) TestMsgUpdateParamsRescalesSameDenomDelta() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	tests := []struct {
		name          string
		oldDelta      math.LegacyDec
		expectedDelta math.LegacyDec
	}{
		{name: "positive delta", oldDelta: math.LegacyNewDec(25), expectedDelta: math.LegacyNewDec(50)},
		{name: "negative delta", oldDelta: math.LegacyNewDec(-25), expectedDelta: math.LegacyNewDec(-50)},
		{name: "zero delta", oldDelta: math.LegacyZeroDec(), expectedDelta: math.LegacyZeroDec()},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			current := types.DefaultParams()
			current.BasePool = sdrBasePool(math.LegacyNewDec(100))
			s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, test.oldDelta))

			updated := current
			updated.BasePool = sdrBasePool(math.LegacyNewDec(200))
			_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
				Authority: authority,
				Params:    updated,
			})
			s.Require().NoError(err)

			storedParams, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(updated, storedParams)
			storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(test.expectedDelta.Equal(storedDelta))

			s.requirePoolUpdateEvent(current.BasePool, updated.BasePool, test.oldDelta, test.expectedDelta)
		})
	}
}

func (s *KeeperTestSuite) TestMsgUpdateParamsAppliesLiveDenomAndCurveChanges() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	current := types.DefaultParams()
	current.BasePool = sdrBasePool(math.LegacyNewDec(100))
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyNewDec(25)))

	submitted := current
	submitted.BasePool = sdk.NewDecCoinFromDec(chain.MicroUSDDenom, math.LegacySmallestDec())
	submitted.PoolRecoveryPeriod++
	submitted.MinStabilitySpread = math.LegacyNewDecWithPrec(5, 2)
	applied := sdk.NewDecCoin(chain.MicroUSDDenom, math.NewInt(200))
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), chain.MicroUSDDenom).
		Return(math.LegacyZeroDec(), nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroSDRDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom: math.LegacyNewDec(2),
	}, nil)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    submitted,
	})
	s.Require().NoError(err)

	storedParams, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	expectedParams := submitted
	expectedParams.BasePool = applied
	s.Require().Equal(expectedParams, storedParams)
	storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDec(50).Equal(storedDelta))

	s.requirePoolUpdateEvent(
		current.BasePool,
		applied,
		math.LegacyNewDec(25),
		math.LegacyNewDec(50),
	)
}

func (s *KeeperTestSuite) TestMsgUpdateParamsDenomChangeFailuresPreserveState() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	oracleInternalErr := errors.New("oracle params unavailable")
	tests := []struct {
		name       string
		setup      func()
		expectErr  string
		errorIs    error
		errorIsNot error
	}{
		{
			name: "denomination is not configured in oracle",
			setup: func() {
				s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), chain.MicroUSDDenom).
					Return(math.LegacyDec{}, oracletypes.ErrUnknownDenom)
			},
			expectErr: "is not configured in oracle",
			errorIs:   errortypes.ErrInvalidRequest,
		},
		{
			name: "oracle configuration lookup fails internally",
			setup: func() {
				s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), chain.MicroUSDDenom).
					Return(math.LegacyDec{}, oracleInternalErr)
			},
			expectErr:  "checking base pool denom uusd in oracle",
			errorIs:    oracleInternalErr,
			errorIsNot: errortypes.ErrInvalidRequest,
		},
		{
			name: "rate snapshot is stale",
			setup: func() {
				s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), chain.MicroUSDDenom).
					Return(math.LegacyZeroDec(), nil)
				s.oracleKeeper.EXPECT().GetRateSnapshot(
					gomock.Any(),
					chain.MicroSDRDenom,
					chain.MicroUSDDenom,
				).Return(nil, oracletypes.ErrStaleExchangeRate)
			},
			expectErr: oracletypes.ErrStaleExchangeRate.Error(),
			errorIs:   oracletypes.ErrStaleExchangeRate,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			current := types.DefaultParams()
			current.BasePool = sdrBasePool(math.LegacyNewDec(100))
			oldDelta := math.LegacyNewDec(25)
			s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))
			submitted := current
			submitted.BasePool = sdk.NewDecCoin(chain.MicroUSDDenom, math.NewInt(200))
			if test.setup != nil {
				test.setup()
			}
			eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

			_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
				Authority: authority,
				Params:    submitted,
			})
			s.Require().ErrorContains(err, test.expectErr)
			if test.errorIs != nil {
				s.Require().ErrorIs(err, test.errorIs)
			}
			if test.errorIsNot != nil {
				s.Require().NotErrorIs(err, test.errorIsNot)
			}
			storedParams, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(current, storedParams)
			storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(oldDelta.Equal(storedDelta))
			s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
		})
	}
}

func (s *KeeperTestSuite) TestMsgUpdateParamsRejectsNonPositiveEffectivePool() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before := types.DefaultParams()
	before.BasePool = sdrBasePool(math.LegacyNewDec(100))
	s.Require().NoError(s.keeper.Params.Set(s.ctx, before))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyNewDec(-200)))

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params: types.Params{
			BasePool:           sdrBasePool(math.LegacyNewDec(200)),
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

func (s *KeeperTestSuite) TestMsgUpdateParamsRescaleOverflowPreservesState() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before := types.DefaultParams()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, before))
	oldDelta := maxLegacyDecForKeeperTest()
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))

	updated := before
	updated.BasePool.Amount = updated.BasePool.Amount.MulInt64(2)
	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    updated,
	})
	s.Require().ErrorIs(err, types.ErrArithmeticOutOfRange)
	s.Require().ErrorContains(err, "rescaling ark pool delta")

	after, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after)
	storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(oldDelta.Equal(storedDelta))
}

func (s *KeeperTestSuite) setupArkToArkSwapMocks(trader sdk.AccAddress, receiver sdk.AccAddress, offerCoin sdk.Coin, swapCoin sdk.Coin) {
	s.expectStableToStableQuote(oracletypes.RateSnapshot{
		"uusd": math.LegacyOneDec(),
		"ukrw": math.LegacyNewDec(1300),
	})

	gomock.InOrder(
		s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(swapCoin)).Return(nil),
		s.treasuryKeeper.EXPECT().RecordSupplyChange(s.ctx, offerCoin, swapCoin, gomock.Any()).Return(nil),
		s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, receiver, sdk.NewCoins(swapCoin)).Return(nil),
	)
}

func (s *KeeperTestSuite) expectStableToStableQuote(rates oracletypes.RateSnapshot) {
	s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "uusd", "ukrw").Return(rates, nil)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: "uusd", TobinTax: math.LegacyMustNewDecFromStr("0.0025")},
		{Denom: "ukrw", TobinTax: math.LegacyMustNewDecFromStr("0.0025")},
	}, nil)
}

func maxLegacyDecForKeeperTest() math.LegacyDec {
	precision := new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, precision)
	raw.Sub(raw, big.NewInt(1))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}

func sdrBasePool(amount math.LegacyDec) sdk.DecCoin {
	return sdk.NewDecCoinFromDec(chain.MicroSDRDenom, amount)
}

func (s *KeeperTestSuite) requirePoolUpdateEvent(
	oldBasePool sdk.DecCoin,
	newBasePool sdk.DecCoin,
	oldArkPoolDelta math.LegacyDec,
	newArkPoolDelta math.LegacyDec,
) {
	s.requireTypedEvent(&types.EventPoolUpdated{
		OldBasePoolDenom:  oldBasePool.Denom,
		OldBasePoolAmount: oldBasePool.Amount,
		NewBasePoolDenom:  newBasePool.Denom,
		NewBasePoolAmount: newBasePool.Amount,
		OldArkPoolDelta:   oldArkPoolDelta,
		NewArkPoolDelta:   newArkPoolDelta,
	})
}

func (s *KeeperTestSuite) requireSwapEvent(trader string, recipient string, offer sdk.Coin, swapCoin sdk.Coin, swapFee sdk.DecCoin) {
	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.requireTypedEvents(events, &types.EventSwap{
		Trader:      trader,
		Recipient:   recipient,
		OfferDenom:  offer.Denom,
		OfferAmount: offer.Amount,
		SwapDenom:   swapCoin.Denom,
		SwapAmount:  swapCoin.Amount,
		FeeDenom:    swapFee.Denom,
		FeeAmount:   swapFee.Amount,
	})
}
