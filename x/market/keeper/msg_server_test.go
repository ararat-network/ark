package keeper_test

import (
	"errors"
	"math/big"
	"strings"

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
			errMsg:    "trader is invalid",
		},
		{
			name: "malformed address",
			msg: &types.MsgSwap{
				Trader: "address",
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "trader is invalid",
		},
		{
			name: "non-canonical address",
			msg: &types.MsgSwap{
				Trader: strings.ToUpper(trader),
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "trader must be a canonical account address",
		},
		{
			name: "recursive swap",
			msg: &types.MsgSwap{
				Trader:         trader,
				OfferCoin:      sdk.NewCoin("akrw", math.NewInt(1)),
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("akrw", 1),
			},
			expectErr: types.ErrRecursiveSwap,
			errMsg:    "recursive swap",
		},
		{
			name: "zero amount",
			msg: &types.MsgSwap{
				Trader: trader,
				OfferCoin: sdk.Coin{
					Denom:  "ausd",
					Amount: math.ZeroInt(),
				},
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("akrw", 1),
			},
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "negative amount",
			msg: &types.MsgSwap{
				Trader: trader,
				OfferCoin: sdk.Coin{
					Denom:  "ausd",
					Amount: math.NewInt(-1),
				},
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("akrw", 1),
			},
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "zero minimum receive",
			msg: &types.MsgSwap{
				Trader:         trader,
				OfferCoin:      sdk.NewInt64Coin("ausd", 1000),
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewCoin("akrw", math.ZeroInt()),
			},
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "minimum receive denom does not match ask denom",
			msg: &types.MsgSwap{
				Trader:         trader,
				OfferCoin:      sdk.NewInt64Coin("ausd", 1000),
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("ausd", 1),
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
			errMsg:    "from address is invalid",
		},
		{
			name: "malformed from address",
			msg: &types.MsgSwapSend{
				FromAddress: "invalid",
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "from address is invalid",
		},
		{
			name: "empty to address",
			msg: &types.MsgSwapSend{
				FromAddress: fromAddr.String(),
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "to address is invalid",
		},
		{
			name: "malformed to address",
			msg: &types.MsgSwapSend{
				FromAddress: fromAddr.String(),
				ToAddress:   "invalid",
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "to address is invalid",
		},
		{
			name: "non-canonical to address",
			msg: &types.MsgSwapSend{
				FromAddress: fromAddr.String(),
				ToAddress:   strings.ToUpper(fromAddr.String()),
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "to address must be a canonical account address",
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
	offerCoin := sdk.NewCoin("ausd", math.NewInt(1000000))
	expectedSwapCoin := sdk.NewCoin("akrw", math.NewInt(1296750000))
	expectedSwapFee := sdk.NewDecCoinFromDec("akrw", math.LegacyNewDec(3250000))

	s.setupArkToArkSwapMocks(fromAddr, toAddr, offerCoin, expectedSwapCoin)

	res, err := s.msgServer.SwapSend(s.ctx, &types.MsgSwapSend{
		FromAddress:    fromAddr.String(),
		ToAddress:      toAddr.String(),
		OfferCoin:      offerCoin,
		AskDenom:       "akrw",
		MinimumReceive: expectedSwapCoin,
	})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(expectedSwapCoin, res.SwapCoin)
	s.Require().True(expectedSwapFee.Amount.Equal(res.SwapFee.Amount))
	s.requireSwapEvent(fromAddr.String(), toAddr.String(), offerCoin, res.SwapCoin, res.SwapFee)
}

// TestMsgSwapOmittedMinimumReceiveAcceptsMarketExecution pins the optionality:
// the zero coin is the one spelling of "no floor", and a swap carrying it
// executes at whatever the quote produced.
func (s *KeeperTestSuite) TestMsgSwapOmittedMinimumReceiveAcceptsMarketExecution() {
	trader := sdk.AccAddress([]byte("addr________________"))
	offerCoin := sdk.NewCoin("ausd", math.NewInt(1000000))
	expectedSwapCoin := sdk.NewCoin("akrw", math.NewInt(1296750000))

	s.setupArkToArkSwapMocks(trader, trader, offerCoin, expectedSwapCoin)

	res, err := s.msgServer.Swap(s.ctx, &types.MsgSwap{
		Trader:    trader.String(),
		OfferCoin: offerCoin,
		AskDenom:  "akrw",
	})
	s.Require().NoError(err)
	s.Require().Equal(expectedSwapCoin, res.SwapCoin)
}

func (s *KeeperTestSuite) TestMsgSwapSendOmittedMinimumReceiveAcceptsMarketExecution() {
	fromAddr := sdk.AccAddress([]byte("from________________"))
	toAddr := sdk.AccAddress([]byte("to__________________"))
	offerCoin := sdk.NewCoin("ausd", math.NewInt(1000000))
	expectedSwapCoin := sdk.NewCoin("akrw", math.NewInt(1296750000))

	s.setupArkToArkSwapMocks(fromAddr, toAddr, offerCoin, expectedSwapCoin)

	res, err := s.msgServer.SwapSend(s.ctx, &types.MsgSwapSend{
		FromAddress: fromAddr.String(),
		ToAddress:   toAddr.String(),
		OfferCoin:   offerCoin,
		AskDenom:    "akrw",
	})
	s.Require().NoError(err)
	s.Require().Equal(expectedSwapCoin, res.SwapCoin)
}

func (s *KeeperTestSuite) TestMsgSwapNativeSettlementUsesQuotedState() {
	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = sdrBasePool(math.LegacyNewDec(400))
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))

	tests := []struct {
		name          string
		offerCoin     sdk.Coin
		askDenom      string
		expectedSwap  sdk.Coin
		expectedDelta math.LegacyDec
		expectedMint  sdk.Coin
		setupTreasury func(oracletypes.RateSet) *gomock.Call
	}{
		{
			name:          "stablecoin to noah",
			offerCoin:     sdk.NewInt64Coin("ausd", 100),
			askDenom:      chain.NoahBaseDenom,
			expectedSwap:  sdk.NewInt64Coin(chain.NoahBaseDenom, 80),
			expectedDelta: math.LegacyNewDec(100),
			expectedMint:  sdk.NewInt64Coin(chain.NoahBaseDenom, 60),
			setupTreasury: func(rates oracletypes.RateSet) *gomock.Call {
				return s.treasuryKeeper.EXPECT().DrawRedemptionBuffer(
					s.ctx,
					sdk.NewInt64Coin(chain.USDBaseDenom, 100),
					math.NewInt(80),
					rates,
				).Return(treasurytypes.BufferDraw{BufferPaid: math.NewInt(20)}, nil)
			},
		},
		{
			name:          "noah to stablecoin",
			offerCoin:     sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
			askDenom:      "ausd",
			expectedSwap:  sdk.NewInt64Coin("ausd", 80),
			expectedDelta: math.LegacyNewDec(-80),
			expectedMint:  sdk.NewInt64Coin("ausd", 80),
			setupTreasury: func(rates oracletypes.RateSet) *gomock.Call {
				return s.treasuryKeeper.EXPECT().RouteExpansion(
					s.ctx,
					sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
					sdk.NewInt64Coin(chain.USDBaseDenom, 80),
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
			rates := oracletypes.RateSet{
				"ausd":              math.LegacyOneDec(),
				chain.SDRBaseDenom:  math.LegacyOneDec(),
				chain.NoahBaseDenom: math.LegacyOneDec(),
			}
			s.oracleKeeper.EXPECT().GetRateSet(
				s.ctx,
				tc.offerCoin.Denom,
				chain.SDRBaseDenom,
				tc.askDenom,
			).Return(rates, nil).Times(1)

			treasuryCall := tc.setupTreasury(rates)
			burned := tc.offerCoin
			if tc.offerCoin.Denom == chain.NoahBaseDenom {
				burned = sdk.NewInt64Coin(chain.NoahBaseDenom, 100)
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
	stableOffer := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)
	stableOutput := sdk.NewInt64Coin(chain.KRWBaseDenom, 1_296_750_000)
	stableRates := oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyNewDec(1300),
	}
	nativeRates := oracletypes.RateSet{
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.SDRBaseDenom:  math.LegacyOneDec(),
		chain.NoahBaseDenom: math.LegacyOneDec(),
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
			askDenom:  chain.KRWBaseDenom,
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
			askDenom:  chain.KRWBaseDenom,
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
			askDenom:  chain.KRWBaseDenom,
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
			askDenom:  chain.KRWBaseDenom,
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
			askDenom:  chain.KRWBaseDenom,
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
			offerCoin: sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
			askDenom:  chain.USDBaseDenom,
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSet(
					s.ctx,
					chain.NoahBaseDenom,
					chain.SDRBaseDenom,
					chain.USDBaseDenom,
				).Return(nativeRates, nil)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 100)),
					).Return(nil),
					s.treasuryKeeper.EXPECT().RouteExpansion(
						s.ctx,
						sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
						sdk.NewInt64Coin(chain.USDBaseDenom, 80),
						nativeRates,
					).Return(treasurytypes.ExpansionAllocation{}, injectedErr),
				)
			},
		},
		{
			name:      "redemption buffer draw",
			offerCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 100),
			askDenom:  chain.NoahBaseDenom,
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSet(
					s.ctx,
					chain.USDBaseDenom,
					chain.SDRBaseDenom,
					chain.NoahBaseDenom,
				).Return(nativeRates, nil)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
					).Return(nil),
					s.treasuryKeeper.EXPECT().DrawRedemptionBuffer(
						s.ctx,
						sdk.NewInt64Coin(chain.USDBaseDenom, 100),
						math.NewInt(80),
						nativeRates,
					).Return(treasurytypes.BufferDraw{}, injectedErr),
				)
			},
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			capacity := types.DefaultConversionPolicy()
			if test.offerCoin.Denom == chain.NoahBaseDenom || test.askDenom == chain.NoahBaseDenom {
				capacity.BasePool = sdrBasePool(math.LegacyNewDec(400))
			}
			s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))
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
	offerCoin := sdk.NewInt64Coin("ausd", 1000000)
	minimumReceive := sdk.NewInt64Coin("akrw", 1296750001)

	s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSet{
			"ausd": math.LegacyOneDec(),
			"akrw": math.LegacyNewDec(1300),
		}, nil)

	beforeDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	_, err = s.msgServer.Swap(s.ctx, &types.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      offerCoin,
		AskDenom:       "akrw",
		MinimumReceive: minimumReceive,
	})
	s.Require().ErrorIs(err, types.ErrMinimumReceiveNotMet)
	s.Require().ErrorContains(err, "minimum 1296750001akrw, received 1296750000akrw")

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
				OfferCoin:      sdk.NewCoin("ausd", math.NewInt(1000000)),
				AskDenom:       "afoo",
				MinimumReceive: sdk.NewInt64Coin("afoo", 1),
			},
		},
		{
			name: "swap send",
			msg: &types.MsgSwapSend{
				FromAddress:    sdk.AccAddress([]byte("from________________")).String(),
				ToAddress:      sdk.AccAddress([]byte("to__________________")).String(),
				OfferCoin:      sdk.NewCoin("ausd", math.NewInt(1000000)),
				AskDenom:       "afoo",
				MinimumReceive: sdk.NewInt64Coin("afoo", 1),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "afoo").
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
			s.Require().ErrorContains(err, "computing swap from 1000000ausd to afoo")
		})
	}
}

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	consensusAuthority := authtypes.NewModuleAddress("consensus").String()

	// Every case starts from the default params and bends one field, so the
	// submitted message is always valid apart from the thing under test — the
	// default Tobin rate included, since an unset one now fails validation on its
	// own.
	tests := []struct {
		name        string
		setup       func()
		authority   string
		mutate      func(*types.Params)
		expectErr   string
		expectErrIs error
	}{
		{
			name:      "valid params",
			authority: authority,
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDecWithPrec(5, 2)
				p.DefaultTobinTax = math.LegacyNewDecWithPrec(5, 3)
			},
		},
		{
			name: "consensus params authority overrides keeper authority",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
					Authority: &cmtproto.AuthorityParams{Authority: consensusAuthority},
				})
			},
			authority: consensusAuthority,
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDecWithPrec(5, 2)
			},
		},
		{
			name:        "invalid authority",
			authority:   "invalid_authority",
			expectErr:   "invalid authority",
			expectErrIs: errortypes.ErrUnauthorized,
		},
		{
			name:      "negative min stability spread",
			authority: authority,
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDec(-1)
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name:      "min stability spread greater than 1",
			authority: authority,
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDecWithPrec(11, 1) // 1.1
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name:      "unset default tobin tax",
			authority: authority,
			mutate: func(p *types.Params) {
				p.DefaultTobinTax = math.LegacyDec{}
			},
			expectErr: "tobin tax must be set",
		},
		{
			name:      "default tobin tax of one",
			authority: authority,
			mutate: func(p *types.Params) {
				p.DefaultTobinTax = math.LegacyOneDec()
			},
			expectErr: "tobin tax must be in [0, 1)",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			ctx := s.ctx
			defer func() { s.ctx = ctx }()
			if tc.setup != nil {
				tc.setup()
			}
			params := types.DefaultParams()
			if tc.mutate != nil {
				tc.mutate(&params)
			}

			_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
				Authority: tc.authority,
				Params:    params,
			})
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
				if tc.expectErrIs != nil {
					s.Require().ErrorIs(err, tc.expectErrIs)
				}
			} else {
				s.Require().NoError(err)

				stored, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				s.Require().Equal(params, stored)
			}
		})
	}
}

func (s *KeeperTestSuite) setupArkToArkSwapMocks(trader sdk.AccAddress, receiver sdk.AccAddress, offerCoin sdk.Coin, swapCoin sdk.Coin) {
	s.expectStableToStableQuote(oracletypes.RateSet{
		"ausd": math.LegacyOneDec(),
		"akrw": math.LegacyNewDec(1300),
	})

	gomock.InOrder(
		s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(swapCoin)).Return(nil),
		s.treasuryKeeper.EXPECT().RecordSupplyChange(s.ctx, offerCoin, swapCoin, gomock.Any()).Return(nil),
		s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, receiver, sdk.NewCoins(swapCoin)).Return(nil),
	)
}

// expectStableToStableQuote stubs the one rate read a stable pair makes. Neither
// leg carries an override, so the pair's spread is the 0.25% default the
// expected amounts here are calculated from.
func (s *KeeperTestSuite) expectStableToStableQuote(rates oracletypes.RateSet) {
	s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "akrw").Return(rates, nil)
}

func maxLegacyDecForKeeperTest() math.LegacyDec {
	precision := new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, precision)
	raw.Sub(raw, big.NewInt(1))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}

func sdrBasePool(amount math.LegacyDec) sdk.DecCoin {
	return sdk.NewDecCoinFromDec(chain.SDRBaseDenom, amount)
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
