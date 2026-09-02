package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/types"
)

// settlementPlan builds an activated plan redeeming two units of the asset per
// one NOAH. The rate divides its offers exactly, which keeps the assertion on
// what settlement records about routing rather than rounding.
func settlementPlan(denom string) assettypes.SettlementPlan {
	return assettypes.SettlementPlan{
		Denom: denom,
		// Half a NOAH per unit of settled paper.
		RedemptionRate:        math.LegacyNewDecWithPrec(5, 1),
		OpenedHeight:          1,
		ActivationHeight:      2,
		EarliestClosingHeight: 102,
	}
}

func (s *KeeperTestSuite) TestSettle() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	offerCoin := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)
	plan := settlementPlan(chain.USDBaseDenom)
	entitlement := sdk.NewInt64Coin(chain.NoahBaseDenom, 500_000)
	tests := []struct {
		name string
	}{
		{
			// The whole entitlement is minted here and the Buffer's share of it
			// burned back at settlement, so the holder receives the whole
			// entitlement without waiting on a valuation: an orderly failure
			// costs bounded NOAH dilution rather than a haircut on the exit.
			name: "the exit mints its whole entitlement",
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

			gomock.InOrder(
				s.bankKeeper.EXPECT().SendCoinsFromAccountToModule(
					s.ctx, trader, types.ModuleName, sdk.NewCoins(offerCoin),
				).Return(nil),
				s.bankKeeper.EXPECT().BurnCoins(
					s.ctx, types.ModuleName, sdk.NewCoins(offerCoin),
				).Return(nil),
				s.bankKeeper.EXPECT().MintCoins(
					s.ctx, types.ModuleName, sdk.NewCoins(entitlement),
				).Return(nil),
				s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
					s.ctx, types.ModuleName, trader, sdk.NewCoins(entitlement),
				).Return(nil),
			)

			redeemed, err := s.keeper.Settle(s.ctx, trader, offerCoin)
			s.Require().NoError(err)
			s.Require().Equal(entitlement, redeemed)
			// The entitlement is recorded at the plan's own committed rate — no
			// oracle expectation is registered anywhere in this test, which is
			// what asserts a suspended asset's market price never reaches it.
			s.requireSettledTotals(types.ConversionTotals{
				GrossOffer:        math.ZeroInt(),
				EligiblePrincipal: math.ZeroInt(),
				RedemptionOutput:  entitlement.Amount,
				RedeemedValue:     math.LegacyNewDecFromInt(entitlement.Amount),
			})

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
