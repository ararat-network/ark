package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

// coinMatcher compares coins by denomination and amount. gomock's default
// equality reaches into math.Int's big.Int internals, where a zero reached by
// subtraction and a zero written as a literal are not the same value.
type coinMatcher struct {
	coin sdk.Coin
}

func equalCoin(coin sdk.Coin) gomock.Matcher {
	return coinMatcher{coin: coin}
}

func (m coinMatcher) Matches(x any) bool {
	coin, ok := x.(sdk.Coin)

	return ok && coin.Denom == m.coin.Denom && coin.Amount.Equal(m.coin.Amount)
}

func (m coinMatcher) String() string {
	return "is equal to " + m.coin.String()
}

// settlementPlan builds an activated plan redeeming two units of the asset per
// one NOAH. The rate divides its offers exactly, which keeps the assertion on
// the rates Treasury is handed about routing rather than rounding.
func settlementPlan(denom string) assettypes.SettlementPlan {
	return assettypes.SettlementPlan{
		Denom:            denom,
		RedemptionRate:   math.LegacyNewDec(2),
		OpenedHeight:     1,
		ActivationHeight: 2,
	}
}

func (s *KeeperTestSuite) TestSettle() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	offerCoin := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)
	plan := settlementPlan(chain.USDBaseDenom)
	entitlement := sdk.NewInt64Coin(chain.NoahBaseDenom, 500_000)
	// The plan rate carried verbatim, because it is already quoted in units of
	// the asset per one NOAH, with the numeraire carried at one so the set has
	// the shape the oracle's own rate sets have. Treasury converts through
	// whatever rates it is handed, and a suspended asset's market price is
	// exactly what stopped being trustworthy, so it must be handed the committed
	// rate and never the oracle's. No oracle expectation is registered anywhere
	// in this test, which is what asserts the second half of that.
	planRates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDec(2),
	}

	tests := []struct {
		name         string
		bufferPaid   math.Int
		expectedMint sdk.Coin
	}{
		{
			// The buffer covers what it can and the rest is minted, so the holder
			// receives the whole entitlement either way: an orderly failure costs
			// bounded NOAH dilution rather than a haircut on the exit.
			name:         "buffer covers part of the entitlement",
			bufferPaid:   math.NewInt(200_000),
			expectedMint: sdk.NewInt64Coin(chain.NoahBaseDenom, 300_000),
		},
		{
			name:         "buffer covers the whole entitlement",
			bufferPaid:   math.NewInt(500_000),
			expectedMint: sdk.NewCoin(chain.NoahBaseDenom, math.ZeroInt()),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.assetStatuses = map[string]assettypes.AssetStatus{
				chain.USDBaseDenom: assettypes.AssetStatus_ASSET_STATUS_SUSPENDED,
			}
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
			s.assetKeeper.EXPECT().
				ActiveSettlementPlan(s.ctx, chain.USDBaseDenom).
				Return(plan, true, nil)

			calls := []any{
				s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
					s.ctx, trader, types.ModuleName, sdk.NewCoins(offerCoin),
				).Return(nil),
				s.treasuryKeeper.EXPECT().DrawRedemptionBuffer(
					s.ctx, offerCoin, entitlement.Amount, planRates,
				).Return(tc.bufferPaid, nil),
				s.bankKeeper.EXPECT().BurnCoins(
					s.ctx, types.ModuleName, sdk.NewCoins(offerCoin),
				).Return(nil),
			}
			if tc.expectedMint.IsPositive() {
				calls = append(calls, s.bankKeeper.EXPECT().MintCoins(
					s.ctx, types.ModuleName, sdk.NewCoins(tc.expectedMint),
				).Return(nil))
			}
			calls = append(
				calls,
				s.treasuryKeeper.EXPECT().RecordSupplyChange(
					s.ctx, offerCoin, equalCoin(tc.expectedMint), planRates,
				).Return(nil),
				s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
					s.ctx, types.ModuleName, trader, sdk.NewCoins(entitlement),
				).Return(nil),
			)
			gomock.InOrder(calls...)

			redeemed, err := s.keeper.Settle(s.ctx, trader, offerCoin)
			s.Require().NoError(err)
			s.Require().Equal(entitlement, redeemed)

			s.requireTypedEvent(&types.EventSettle{
				Trader:         trader.String(),
				OfferDenom:     offerCoin.Denom,
				OfferAmount:    offerCoin.Amount,
				RedeemedAmount: entitlement.Amount,
				RedemptionRate: plan.RedemptionRate,
			})

			// Settlement is a one-way redemption at a committed rate, not a route
			// through the market, so the constant-product pool the market prices
			// swaps from must be untouched by it.
			delta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(delta.IsZero(), "ark pool delta moved to %s", delta)
		})
	}
}

func (s *KeeperTestSuite) TestSettleRequiresAnActiveSettlementPlan() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	offerCoin := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)

	tests := []struct {
		name string
		plan assettypes.SettlementPlan
	}{
		{
			// x/asset reports a plan whose activation height has not arrived as
			// not found. Market takes that answer as given rather than reading the
			// plan's heights itself, so the terms travelling alongside the false
			// must not be enough to redeem against.
			name: "plan has not activated",
			plan: settlementPlan(chain.USDBaseDenom),
		},
		{
			name: "no plan was ever opened",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.assetStatuses = map[string]assettypes.AssetStatus{
				chain.USDBaseDenom: assettypes.AssetStatus_ASSET_STATUS_SUSPENDED,
			}
			s.assetKeeper.EXPECT().
				ActiveSettlementPlan(s.ctx, chain.USDBaseDenom).
				Return(tc.plan, false, nil)

			_, err := s.keeper.Settle(s.ctx, trader, offerCoin)
			s.Require().ErrorIs(err, types.ErrNoActiveSettlement)
			s.Require().ErrorContains(err, chain.USDBaseDenom)
		})
	}
}

func (s *KeeperTestSuite) TestSettleRejectsIneligibleStatus() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	offerCoin := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)

	// Settlement is the suspended holder's exit. Every other status either still
	// has conversion as a better route out or has nothing governance committed
	// to pay, so no plan lookup should even be attempted — the absent mock
	// expectation is what asserts that.
	statuses := []assettypes.AssetStatus{
		assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
		assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		assettypes.AssetStatus_ASSET_STATUS_PENDING,
		assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		assettypes.AssetStatus_ASSET_STATUS_RETIRED,
		assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED,
	}

	for _, status := range statuses {
		s.Run(status.String(), func() {
			s.assetStatuses = map[string]assettypes.AssetStatus{chain.USDBaseDenom: status}

			_, err := s.keeper.Settle(s.ctx, trader, offerCoin)
			s.Require().Error(err)
			if status == assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED {
				s.Require().ErrorIs(err, assettypes.ErrAssetNotFound)
				return
			}
			s.Require().ErrorIs(err, types.ErrIneligibleAsset)
			s.Require().ErrorContains(err, "cannot be settled")
		})
	}
}
