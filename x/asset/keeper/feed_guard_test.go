package keeper_test

import (
	"fmt"

	"ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

func assetReferent(referent string) oracletypes.FeedReferent {
	return oracletypes.FeedReferent{
		Consumer: types.ModuleName,
		Referent: referent,
	}
}

// TestFeedReferentsPinAssetsNeedingTheirFeed walks the full status matrix.
// The pin is not "an asset exists under this denom" but "this asset is priced
// live right now", which is what makes a tombstone's denomination an inert key
// rather than a veto that can never be cleared.
func (s *KeeperTestSuite) TestFeedReferentsPinAssetsNeedingTheirFeed() {
	tests := []struct {
		name   string
		status types.AssetStatus
		pinned bool
	}{
		{
			name:   "active",
			status: types.AssetStatus_ASSET_STATUS_ACTIVE,
			pinned: true,
		},
		{
			name:   "issuance halted",
			status: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			pinned: true,
		},
		{
			name:   "suspended idle",
			status: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		},
		{
			name:   "written off",
			status: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		},
		{
			name:   "retired tombstone",
			status: types.AssetStatus_ASSET_STATUS_RETIRED,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			asset := types.DefaultGenesisState().Assets[0]
			asset.Status = tt.status
			s.Require().NoError(asset.Validate())
			s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

			referents, err := s.keeper.FeedReferents(s.ctx, asset.Denom)
			s.Require().NoError(err)
			if !tt.pinned {
				s.Require().Empty(referents)
				return
			}
			s.Require().Equal(
				[]oracletypes.FeedReferent{
					assetReferent(fmt.Sprintf("asset %s (%s)", asset.Denom, tt.status)),
				},
				referents,
			)
		})
	}
}

func (s *KeeperTestSuite) TestFeedReferentsIgnoreUnregisteredDenoms() {
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	// A feed may run ahead of its asset being listed, and nothing pins it.
	referents, err := s.keeper.FeedReferents(s.ctx, "agold")
	s.Require().NoError(err)
	s.Require().Empty(referents)
}
