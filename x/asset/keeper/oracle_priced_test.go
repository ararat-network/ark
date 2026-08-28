package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
)

// seedOraclePricedFixture stores one asset per membership case. Every asset
// is keyed to a feed, so membership is purely a status predicate — and, by
// the conversion invariant, exactly the convertible set.
func (s *KeeperTestSuite) seedOraclePricedFixture() {
	statuses := map[string]types.AssetStatus{
		chain.CNYBaseDenom: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		chain.EURBaseDenom: types.AssetStatus_ASSET_STATUS_ACTIVE,
		chain.GBPBaseDenom: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		chain.JPYBaseDenom: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		chain.KRWBaseDenom: types.AssetStatus_ASSET_STATUS_RETIRED,
		chain.MNTBaseDenom: types.AssetStatus_ASSET_STATUS_ACTIVE,
		chain.USDBaseDenom: types.AssetStatus_ASSET_STATUS_ACTIVE,
	}
	for _, asset := range types.DefaultGenesisState().Assets {
		asset.Status = statuses[asset.Denom]
		s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	}

}

func (s *KeeperTestSuite) TestOraclePricedDenoms() {
	s.seedOraclePricedFixture()

	denoms, err := s.keeper.OraclePricedDenoms(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]string{
		chain.EURBaseDenom,
		chain.GBPBaseDenom,
		chain.MNTBaseDenom,
		chain.USDBaseDenom,
	}, denoms)
}

func (s *KeeperTestSuite) TestIsOraclePriced() {
	s.seedOraclePricedFixture()

	tests := []struct {
		name   string
		denom  string
		member bool
	}{
		{name: "active", denom: chain.EURBaseDenom, member: true},
		{name: "issuance halted", denom: chain.GBPBaseDenom, member: true},
		{name: "written off", denom: chain.CNYBaseDenom},
		{name: "suspended", denom: chain.JPYBaseDenom},
		{name: "retired", denom: chain.KRWBaseDenom},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			member, err := s.keeper.IsOraclePriced(s.ctx, tt.denom)
			s.Require().NoError(err)
			s.Require().Equal(tt.member, member)
		})
	}

	_, err := s.keeper.IsOraclePriced(s.ctx, "azzz")
	s.Require().ErrorIs(err, types.ErrAssetNotFound)
}

// The three tests below pin the signal consumers rebuild against. Membership
// is derived from asset status on every read rather than announced, so a
// transition is visible in the same block it lands — and a transition that
// stays inside the live set moves nothing.
func (s *KeeperTestSuite) TestOraclePricedDenomsHoldInsideLiveBoundary() {
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	// ACTIVE to ISSUANCE_HALTED stays priceable on both sides.
	s.Require().NoError(s.keeper.HaltIssuance(s.ctx, asset.Denom, asset.Version))
	s.requireOraclePricedDenoms([]string{asset.Denom})

	// ISSUANCE_HALTED to SUSPENDED leaves the live set.
	s.Require().NoError(s.keeper.SuspendAsset(s.ctx, asset.Denom, asset.Version+1))
	s.requireOraclePricedDenoms(nil)
}

// TestOraclePricedDenomsGainOnRegistration pins membership to registration
// itself. There is no longer a status between the two, so an asset joins the
// live set in the block its registration executes.
func (s *KeeperTestSuite) TestOraclePricedDenomsGainOnRegistration() {
	asset := types.DefaultGenesisState().Assets[0]
	s.requireOraclePricedDenoms(nil)

	s.expectFreshDenom(asset.Denom)
	s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, asset.Metadata)
	s.Require().NoError(s.keeper.RegisterAsset(s.ctx, asset.Denom))
	s.requireOraclePricedDenoms([]string{asset.Denom})
}

func (s *KeeperTestSuite) TestOraclePricedDenomsDropOnRemovalPromotion() {
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(gomock.Any(), asset.Denom).
		Return(zeroAssetCoin(asset.Denom)).
		AnyTimes()

	// Retirement is immediate, so the asset leaves the live set in the same
	// block that finalises it.
	s.Require().NoError(s.keeper.FinalizeRetirement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.ZeroInt(),
	))
	s.requireOraclePricedDenoms(nil)
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		asset.Version+1,
	)
}

func (s *KeeperTestSuite) requireOraclePricedDenoms(expected []string) {
	denoms, err := s.keeper.OraclePricedDenoms(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected, denoms)
}
