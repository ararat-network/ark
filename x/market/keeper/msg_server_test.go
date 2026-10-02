package keeper_test

import (
	"context"
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

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestMsgSell() {
	trader := sdk.AccAddress([]byte("trader_______________")).String()

	tests := []struct {
		name      string
		msg       *types.MsgSell
		expectErr error
		errMsg    string
	}{
		{
			name:      "empty address",
			msg:       &types.MsgSell{},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "trader is invalid",
		},
		{
			name: "malformed address",
			msg: &types.MsgSell{
				Trader: "address",
			},
			expectErr: errortypes.ErrInvalidAddress,
			errMsg:    "trader is invalid",
		},
		{
			// The uppercase spelling decodes to the same account, so the
			// handler accepts it and the failure is the swap's own recursion
			// check — proof the address cleared parsing.
			name: "uppercase trader is accepted",
			msg: &types.MsgSell{
				Trader:         strings.ToUpper(trader),
				OfferCoin:      sdk.NewCoin("akrw", math.NewInt(1)),
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("akrw", 1),
			},
			expectErr: types.ErrRecursiveSwap,
			errMsg:    "recursive swap",
		},
		{
			name: "recursive swap",
			msg: &types.MsgSell{
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
			msg: &types.MsgSell{
				Trader: trader,
				OfferCoin: sdk.Coin{
					Denom:  "ausd",
					Amount: math.ZeroInt(),
				},
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("akrw", 1),
			},
			expectErr: types.ErrInvalidOrder,
			errMsg:    "invalid order",
		},
		{
			name: "negative amount",
			msg: &types.MsgSell{
				Trader: trader,
				OfferCoin: sdk.Coin{
					Denom:  "ausd",
					Amount: math.NewInt(-1),
				},
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("akrw", 1),
			},
			expectErr: types.ErrInvalidOrder,
			errMsg:    "invalid order",
		},
		{
			name: "zero minimum receive",
			msg: &types.MsgSell{
				Trader:         trader,
				OfferCoin:      sdk.NewInt64Coin("ausd", 1000),
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewCoin("akrw", math.ZeroInt()),
			},
			expectErr: types.ErrInvalidOrder,
			errMsg:    "invalid order",
		},
		{
			name: "minimum receive denom does not match ask denom",
			msg: &types.MsgSell{
				Trader:         trader,
				OfferCoin:      sdk.NewInt64Coin("ausd", 1000),
				AskDenom:       "akrw",
				MinimumReceive: sdk.NewInt64Coin("ausd", 1),
			},
			expectErr: types.ErrInvalidOrder,
			errMsg:    "minimum receive: denom",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.msgServer.Sell(s.ctx, tc.msg)
			s.Require().Error(err)
			s.Require().ErrorIs(err, tc.expectErr)
			s.Require().ErrorContains(err, tc.errMsg)
		})
	}
}

// TestMsgSellOmittedMinimumReceiveAcceptsMarketExecution pins the optionality:
// the zero coin is the one spelling of "no floor", and a swap carrying it
// executes at whatever the quote produced.
func (s *KeeperTestSuite) TestMsgSellOmittedMinimumReceiveAcceptsMarketExecution() {
	trader := sdk.AccAddress([]byte("addr________________"))
	offerCoin := sdk.NewCoin("ausd", math.NewInt(1000000))
	expectedSwapCoin := sdk.NewCoin("akrw", math.NewInt(1296750000))

	s.setupArkToArkSwapMocks(trader, offerCoin, expectedSwapCoin)

	res, err := s.msgServer.Sell(s.ctx, &types.MsgSell{
		Trader:    trader.String(),
		OfferCoin: offerCoin,
		AskDenom:  "akrw",
	})
	s.Require().NoError(err)
	s.Require().Equal(expectedSwapCoin, res.SwapCoin)
	s.requireSwapEvent(trader.String(), offerCoin, res.SwapCoin, res.SwapFee)
}

func (s *KeeperTestSuite) TestMsgSellNativeSettlementUsesQuotedState() {
	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = xdrBasePool(math.LegacyNewDec(400))
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))

	// No Treasury call appears in either case: a conversion records its own
	// facts and settles at the end of the block. A redemption therefore mints
	// its whole quoted output, and an expansion burns nothing, holding its
	// whole offer for settlement to place (D6).
	tests := []struct {
		name           string
		offerCoin      sdk.Coin
		askDenom       string
		expectedSwap   sdk.Coin
		expectedDelta  math.LegacyDec
		expectedBurn   sdk.Coin
		expectedMint   sdk.Coin
		expectedTotals types.ConversionTotals
	}{
		{
			name:          "stablecoin to noah",
			offerCoin:     sdk.NewInt64Coin("ausd", 100),
			askDenom:      chain.NoahBaseDenom,
			expectedSwap:  sdk.NewInt64Coin(chain.NoahBaseDenom, 80),
			expectedDelta: math.LegacyNewDec(100),
			expectedBurn:  sdk.NewInt64Coin("ausd", 100),
			expectedMint:  sdk.NewInt64Coin(chain.NoahBaseDenom, 80),
			expectedTotals: types.ConversionTotals{
				GrossOffer:        math.ZeroInt(),
				EligiblePrincipal: math.ZeroInt(),
				RedemptionOutput:  math.NewInt(80),
				RedeemedValue:     math.LegacyNewDec(100),
			},
		},
		{
			name:          "noah to stablecoin",
			offerCoin:     sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
			askDenom:      "ausd",
			expectedSwap:  sdk.NewInt64Coin("ausd", 80),
			expectedDelta: math.LegacyNewDec(-80),
			expectedBurn:  chain.NoahCoin(math.ZeroInt()),
			expectedMint:  sdk.NewInt64Coin("ausd", 80),
			expectedTotals: types.ConversionTotals{
				GrossOffer:        math.NewInt(100),
				EligiblePrincipal: math.NewInt(80),
				RedemptionOutput:  math.ZeroInt(),
				RedeemedValue:     math.LegacyZeroDec(),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
			trader := sdk.AccAddress([]byte("trader_______________"))
			rates := oracletypes.RateSet{
				"ausd":              math.LegacyOneDec(),
				chain.XDRBaseDenom:  math.LegacyOneDec(),
				chain.NoahBaseDenom: math.LegacyOneDec(),
			}
			s.oracleKeeper.EXPECT().GetRateSet(
				s.ctx,
				tc.offerCoin.Denom,
				chain.XDRBaseDenom,
				tc.askDenom,
			).Return(rates, nil).Times(1)

			calls := []any{
				s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(tc.offerCoin)).Return(nil),
			}
			if tc.expectedBurn.IsPositive() {
				calls = append(calls, s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(tc.expectedBurn)).Return(nil))
			}
			calls = append(calls,
				s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(tc.expectedMint)).Return(nil),
				s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, trader, sdk.NewCoins(tc.expectedSwap)).Return(nil),
			)
			gomock.InOrder(calls...)

			res, err := s.msgServer.Sell(s.ctx, &types.MsgSell{
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
			s.requireSettledTotals(tc.expectedTotals)
		})
	}
}

// TestMsgSellExpansionFeeReconcilesWithRecordedResidual checks disclosed fee equals offer minus
// minted value within one base unit of truncation. A non-unit rate exercises denomination rounding.
func (s *KeeperTestSuite) TestMsgSellExpansionFeeReconcilesWithRecordedResidual() {
	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = xdrBasePool(math.LegacyNewDec(400))
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))

	trader := sdk.AccAddress([]byte("trader_______________"))
	offer := sdk.NewInt64Coin(chain.NoahBaseDenom, 101)
	rates := oracletypes.RateSet{
		chain.USDBaseDenom:  math.LegacyMustNewDecFromStr("1.37"),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.NoahBaseDenom: math.LegacyOneDec(),
	}
	s.oracleKeeper.EXPECT().GetRateSet(
		s.ctx, chain.NoahBaseDenom, chain.XDRBaseDenom, chain.USDBaseDenom,
	).Return(rates, nil)
	s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
		s.ctx, trader, types.ModuleName, sdk.NewCoins(offer),
	).Return(nil)
	s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, gomock.Any()).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
		s.ctx, types.ModuleName, trader, gomock.Any(),
	).Return(nil)

	res, err := s.msgServer.Sell(s.ctx, &types.MsgSell{
		Trader:         trader.String(),
		OfferCoin:      offer,
		AskDenom:       chain.USDBaseDenom,
		MinimumReceive: sdk.NewInt64Coin(chain.USDBaseDenom, 1),
	})
	s.Require().NoError(err)

	var totals types.ConversionTotals
	s.treasuryKeeper.EXPECT().
		SettleConversions(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, recorded types.ConversionTotals) (math.Int, error) {
			totals = recorded
			return math.ZeroInt(), nil
		})
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	s.endBlock()

	// The whole offer was recorded, and the principal is the output's NOAH
	// value truncated — which the fixture makes an actual truncation.
	outputValue, err := rates.Convert(sdk.NewDecCoinFromCoin(res.SwapCoin), chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().False(outputValue.Amount.IsInteger(), "the fixture must make eligible principal truncate")
	s.Require().Equal(offer.Amount, totals.GrossOffer)
	s.Require().Equal(outputValue.Amount.TruncateInt(), totals.EligiblePrincipal)

	// The residual settlement places is the disclosed fee, within one base unit.
	residual := math.LegacyNewDecFromInt(totals.GrossOffer.Sub(totals.EligiblePrincipal))
	feeValue, err := rates.Convert(res.SwapFee, chain.NoahBaseDenom)
	s.Require().NoError(err)
	difference := residual.Sub(feeValue.Amount)
	s.Require().True(
		difference.Abs().LT(math.LegacyOneDec()),
		"residual %s and fee value %s differ by %s, more than one base unit",
		residual, feeValue.Amount, difference,
	)
}

func (s *KeeperTestSuite) TestMsgSellSettlementErrorsPropagate() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	injectedErr := errors.New("injected settlement failure")
	stableOffer := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)
	stableOutput := sdk.NewInt64Coin(chain.KRWBaseDenom, 1_296_750_000)
	stableRates := oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyNewDec(1300),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	}
	nativeRates := oracletypes.RateSet{
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
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
					s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
						s.ctx, types.ModuleName, trader, sdk.NewCoins(stableOutput),
					).Return(injectedErr),
				)
			},
		},
		{
			// An expansion burns nothing inside the conversion (D6), so the
			// output mint is what can fail there; the offer it retains is
			// Treasury's to place at settlement.
			name:      "expansion output mint",
			offerCoin: sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
			askDenom:  chain.USDBaseDenom,
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSet(
					s.ctx,
					chain.NoahBaseDenom,
					chain.XDRBaseDenom,
					chain.USDBaseDenom,
				).Return(nativeRates, nil)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 100)),
					).Return(nil),
					s.bankKeeper.EXPECT().MintCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 80)),
					).Return(injectedErr),
				)
			},
		},
		{
			// A redemption mints its whole quoted output and waits for no
			// Buffer, so the mint is what can fail inside the conversion.
			name:      "redemption output mint",
			offerCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 100),
			askDenom:  chain.NoahBaseDenom,
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSet(
					s.ctx,
					chain.USDBaseDenom,
					chain.XDRBaseDenom,
					chain.NoahBaseDenom,
				).Return(nativeRates, nil)
				gomock.InOrder(
					s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
						s.ctx, trader, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
					).Return(nil),
					s.bankKeeper.EXPECT().BurnCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
					).Return(nil),
					s.bankKeeper.EXPECT().MintCoins(
						s.ctx, types.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 80)),
					).Return(injectedErr),
				)
			},
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			capacity := types.DefaultConversionPolicy()
			if test.offerCoin.Denom == chain.NoahBaseDenom || test.askDenom == chain.NoahBaseDenom {
				capacity.BasePool = xdrBasePool(math.LegacyNewDec(400))
			}
			s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
			test.setup()

			_, err := s.msgServer.Sell(s.ctx, &types.MsgSell{
				Trader:         trader.String(),
				OfferCoin:      test.offerCoin,
				AskDenom:       test.askDenom,
				MinimumReceive: sdk.NewInt64Coin(test.askDenom, 1),
			})
			s.Require().ErrorIs(err, injectedErr)
		})
	}
}

func (s *KeeperTestSuite) TestMsgSellRejectsMinimumReceiveAboveOutput() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	offerCoin := sdk.NewInt64Coin("ausd", 1000000)
	minimumReceive := sdk.NewInt64Coin("akrw", 1296750001)

	s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSet{
			"ausd": math.LegacyNewDec(1300),
			"akrw": math.LegacyOneDec(),
		}, nil)

	beforeDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	_, err = s.msgServer.Sell(s.ctx, &types.MsgSell{
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

func (s *KeeperTestSuite) TestMsgSell_QuoteErrorIncludesContext() {
	tests := []struct {
		name string
		msg  *types.MsgSell
	}{
		{
			name: "sell",
			msg: &types.MsgSell{
				Trader:         sdk.AccAddress([]byte("trader_______________")).String(),
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

			_, err := s.msgServer.Sell(s.ctx, tc.msg)
			s.Require().Error(err)
			s.Require().ErrorIs(err, oracletypes.ErrUnknownDenom)
			s.Require().ErrorContains(err, "computing swap from 1000000ausd to afoo")
		})
	}
}

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	consensusAuthority := authtypes.NewModuleAddress("consensus").String()

	// Each case mutates default params so only the field under test is invalid. The default Tobin
	// rate keeps the independent rate validation satisfied.
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
				p.DefaultTobinTax = math.LegacyNewDecWithPrec(5, 3)
			},
		},
		{
			name:        "invalid authority",
			authority:   "invalid_authority",
			expectErr:   "invalid authority",
			expectErrIs: errortypes.ErrUnauthorized,
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

func (s *KeeperTestSuite) setupArkToArkSwapMocks(trader sdk.AccAddress, offerCoin sdk.Coin, swapCoin sdk.Coin) {
	// One USD is 1300 NOAH and one KRW is one, so one USD is 1300 KRW.
	s.expectStableToStableQuote(oracletypes.RateSet{
		"ausd": math.LegacyNewDec(1300),
		"akrw": math.LegacyOneDec(),
	})

	gomock.InOrder(
		s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(s.ctx, trader, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(offerCoin)).Return(nil),
		s.bankKeeper.EXPECT().MintCoins(s.ctx, types.ModuleName, sdk.NewCoins(swapCoin)).Return(nil),
		s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, trader, sdk.NewCoins(swapCoin)).Return(nil),
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

func xdrBasePool(amount math.LegacyDec) sdk.DecCoin {
	return sdk.NewDecCoinFromDec(chain.XDRBaseDenom, amount)
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

func (s *KeeperTestSuite) requireSwapEvent(trader string, offer sdk.Coin, swapCoin sdk.Coin, swapFee sdk.DecCoin) {
	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.requireTypedEvents(events, &types.EventSwap{
		Trader:      trader,
		OfferDenom:  offer.Denom,
		OfferAmount: offer.Amount,
		SwapDenom:   swapCoin.Denom,
		SwapAmount:  swapCoin.Amount,
		FeeDenom:    swapFee.Denom,
		FeeAmount:   swapFee.Amount,
	})
}
