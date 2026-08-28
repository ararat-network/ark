package keeper_test

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/asset/types"
)

func (s *KeeperTestSuite) TestOpenSettlement() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	rate := math.LegacyNewDecWithPrec(25, 1)
	s.Require().NoError(s.keeper.OpenSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
		rate,
		testSettlementActivationHeight(10)+100,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(types.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        rate,
		ActivationHeight:      testSettlementActivationHeight(10),
		EarliestClosingHeight: testSettlementActivationHeight(10) + 100,
		OpenedHeight:          10,
	}, plan)
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventAssetStatusChanged{
			Denom:     asset.Denom,
			OldStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			NewStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			Version:   asset.Version + 1,
		},
		&types.EventSettlementOpened{
			SettlementPlan: plan,
		},
	)
}

func (s *KeeperTestSuite) TestOpenSettlementReinstatesWrittenOffAsset() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_WRITTEN_OFF
	asset.Version = 3
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	s.Require().NoError(s.keeper.OpenSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.LegacyOneDec(),
		testSettlementActivationHeight(10)+100,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventAssetStatusChanged{
			Denom:     asset.Denom,
			OldStatus: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			NewStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			Version:   asset.Version + 1,
		},
		&types.EventSettlementOpened{
			SettlementPlan: plan,
		},
	)
}

func (s *KeeperTestSuite) TestOpenSettlementRejectsUnsafeState() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	assets := types.DefaultGenesisState().Assets

	zeroSupply := assets[0]
	zeroSupply.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	s.Require().NoError(s.keeper.Assets.Set(
		s.ctx,
		zeroSupply.Denom,
		zeroSupply,
	))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, zeroSupply.Denom).
		Return(zeroAssetCoin(zeroSupply.Denom))
	err := s.keeper.OpenSettlement(
		s.ctx,
		zeroSupply.Denom,
		zeroSupply.Version,
		math.LegacyOneDec(),
		testSettlementActivationHeight(10)+100,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)

	hasPlan, hasErr := s.keeper.SettlementPlans.Has(s.ctx, zeroSupply.Denom)
	s.Require().NoError(hasErr)
	s.Require().False(hasPlan)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestOpenSettlementRejectsInvalidTerms() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	assets := types.DefaultGenesisState().Assets

	for i, test := range []struct {
		name          string
		rate          math.LegacyDec
		closingHeight int64
	}{
		{
			name:          "redemption rate is not positive",
			rate:          math.LegacyZeroDec(),
			closingHeight: testSettlementActivationHeight(10) + 100,
		},
		{
			// The window is mandatory: a plan with no announced closing height
			// would be one WriteOffAsset could end the block after it activates.
			name:          "no closing window announced",
			rate:          math.LegacyOneDec(),
			closingHeight: 0,
		},
		{
			// The derived activation height is the floor the announced window
			// has to clear, so a closing height inside the correction window is
			// the one timing mistake a proposal can still make.
			name:          "closing window precedes activation",
			rate:          math.LegacyOneDec(),
			closingHeight: testSettlementActivationHeight(10),
		},
	} {
		s.Run(test.name, func() {
			asset := assets[i]
			asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
			s.Require().NoError(s.keeper.Assets.Set(
				s.ctx,
				asset.Denom,
				asset,
			))
			s.bankKeeper.EXPECT().
				GetSupply(s.ctx, asset.Denom).
				Return(sdk.NewInt64Coin(asset.Denom, 100))

			err := s.keeper.OpenSettlement(
				s.ctx,
				asset.Denom,
				asset.Version,
				test.rate,
				test.closingHeight,
			)
			s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
			hasPlan, hasErr := s.keeper.SettlementPlans.Has(s.ctx, asset.Denom)
			s.Require().NoError(hasErr)
			s.Require().False(hasPlan)
			s.requireStoredAsset(
				asset.Denom,
				types.AssetStatus_ASSET_STATUS_SUSPENDED,
				asset.Version,
			)
		})
	}
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestCancelSettlementBeforeActivation() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        math.LegacyOneDec(),
		ActivationHeight:      20,
		EarliestClosingHeight: 30,
		OpenedHeight:          1,
	}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(
		s.ctx,
		asset.Denom,
		plan,
	))

	s.Require().NoError(s.keeper.CancelSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
	hasPlan, err := s.keeper.SettlementPlans.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().False(hasPlan)
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventAssetStatusChanged{
			Denom:     asset.Denom,
			OldStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			NewStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			Version:   asset.Version + 1,
		},
		&types.EventSettlementCancelled{
			Denom:   asset.Denom,
			Version: asset.Version + 1,
		},
	)
}

// From the activation height onward the plan is a hard commitment. Cancellation
// is the correction window only, so the boundary block is already too late.
func (s *KeeperTestSuite) TestCancelSettlementRejectedFromActivation() {
	assets := types.DefaultGenesisState().Assets
	boundary := assets[0]
	boundary.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	boundary.Version = 2
	plan := types.SettlementPlan{
		Denom:                 boundary.Denom,
		RedemptionRate:        math.LegacyOneDec(),
		ActivationHeight:      20,
		EarliestClosingHeight: 30,
		OpenedHeight:          1,
	}
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(plan.ActivationHeight)
	s.Require().NoError(s.keeper.Assets.Set(
		s.ctx,
		boundary.Denom,
		boundary,
	))
	s.Require().NoError(s.keeper.SettlementPlans.Set(
		s.ctx,
		boundary.Denom,
		plan,
	))

	err := s.keeper.CancelSettlement(
		s.ctx,
		boundary.Denom,
		boundary.Version,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
	s.Require().ErrorContains(err, "can no longer be cancelled")
	s.requireStoredAsset(
		boundary.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		boundary.Version,
	)
	storedPlan, getErr := s.keeper.SettlementPlans.Get(s.ctx, boundary.Denom)
	s.Require().NoError(getErr)
	s.Require().Equal(plan, storedPlan)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

	missing := assets[1]
	missing.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, missing.Denom, missing))
	err = s.keeper.CancelSettlement(s.ctx, missing.Denom, missing.Version)
	s.Require().ErrorIs(err, types.ErrSettlementPlanNotFound)
}

func (s *KeeperTestSuite) TestWriteOffAsset() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(25)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        math.LegacyNewDecWithPrec(25, 1),
		ActivationHeight:      10,
		EarliestClosingHeight: 20,
		OpenedHeight:          5,
	}
	supply := sdk.NewInt64Coin(asset.Denom, 100)
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(
		s.ctx,
		asset.Denom,
		plan,
	))
	s.bankKeeper.EXPECT().GetSupply(s.ctx, asset.Denom).Return(supply)

	s.Require().NoError(s.keeper.WriteOffAsset(
		s.ctx,
		asset.Denom,
		asset.Version,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		asset.Version+1,
	)
	hasPlan, err := s.keeper.SettlementPlans.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().False(hasPlan)
	record, err := s.keeper.ResolutionRecords.Get(
		s.ctx,
		collections.Join(asset.Denom, asset.Version+1),
	)
	s.Require().NoError(err)
	s.Require().Equal(types.ResolutionRecord{
		Denom:             asset.Denom,
		Kind:              types.ResolutionKind_RESOLUTION_KIND_WRITE_OFF,
		Version:           asset.Version + 1,
		ResolutionHeight:  25,
		OutstandingSupply: supply,
		SettlementPlan:    &plan,
	}, record)
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventAssetStatusChanged{
			Denom:     asset.Denom,
			OldStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			NewStatus: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			Version:   asset.Version + 1,
		},
		&types.EventSettlementClosed{
			SettlementPlan: plan,
			Version:        asset.Version + 1,
			ClosedHeight:   25,
		},
		&types.EventAssetResolved{ResolutionRecord: record},
	)
}

func (s *KeeperTestSuite) TestWriteOffAssetRejectsUnsafeState() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(25)
	assets := types.DefaultGenesisState().Assets

	// Write-off no longer touches feed state, so nothing about a feed can
	// block it: supply and status are the whole precondition set.
	zeroSupply := assets[1]
	zeroSupply.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	s.Require().NoError(s.keeper.Assets.Set(
		s.ctx,
		zeroSupply.Denom,
		zeroSupply,
	))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, zeroSupply.Denom).
		Return(zeroAssetCoin(zeroSupply.Denom))
	err := s.keeper.WriteOffAsset(
		s.ctx,
		zeroSupply.Denom,
		zeroSupply.Version,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)

	duplicate := assets[2]
	duplicate.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	duplicate.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(
		s.ctx,
		duplicate.Denom,
		duplicate,
	))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, duplicate.Denom).
		Return(sdk.NewInt64Coin(duplicate.Denom, 100))
	recordKey := collections.Join(duplicate.Denom, duplicate.Version+1)
	s.Require().NoError(s.keeper.ResolutionRecords.Set(
		s.ctx,
		recordKey,
		types.ResolutionRecord{},
	))
	err = s.keeper.WriteOffAsset(
		s.ctx,
		duplicate.Denom,
		duplicate.Version,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
	s.requireStoredAsset(
		duplicate.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		duplicate.Version,
	)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

// TestResolutionRecordsAccumulateAcrossVersions pins the (denom, version)
// uniqueness the append guard relies on: an asset resolved twice keeps both
// records, because the governance transitions between them advanced the
// version. A retirement residual is never followed by a further record for
// the same denom — residual supply makes the tombstone permanent — so
// write-off then retirement is the only sequence that resolves one denom
// twice.
func (s *KeeperTestSuite) TestResolutionRecordsAccumulateAcrossVersions() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(25)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	supply := sdk.NewInt64Coin(asset.Denom, 100)
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.bankKeeper.EXPECT().GetSupply(s.ctx, asset.Denom).Return(supply)
	s.Require().NoError(s.keeper.WriteOffAsset(
		s.ctx,
		asset.Denom,
		asset.Version,
	))

	// Recovery from write-off restores ISSUANCE_HALTED in one mutation, which
	// advances the version and is what makes the second resolution record's
	// key distinct from the first.
	s.Require().NoError(s.keeper.RecoverAsset(
		s.ctx,
		asset.Denom,
		asset.Version+1,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		asset.Version+2,
	)

	s.bankKeeper.EXPECT().GetSupply(s.ctx, asset.Denom).Return(supply)
	s.Require().NoError(s.keeper.FinalizeRetirement(
		s.ctx,
		asset.Denom,
		asset.Version+2,
		supply.Amount,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		asset.Version+3,
	)

	writeOff, err := s.keeper.ResolutionRecords.Get(
		s.ctx,
		collections.Join(asset.Denom, asset.Version+1),
	)
	s.Require().NoError(err)
	s.Require().Equal(
		types.ResolutionKind_RESOLUTION_KIND_WRITE_OFF,
		writeOff.Kind,
	)
	residual, err := s.keeper.ResolutionRecords.Get(
		s.ctx,
		collections.Join(asset.Denom, asset.Version+3),
	)
	s.Require().NoError(err)
	s.Require().Equal(
		types.ResolutionKind_RESOLUTION_KIND_RETIREMENT_RESIDUAL,
		residual.Kind,
	)
	s.Require().Equal(supply, residual.OutstandingSupply)
}

// testSettlementActivationHeight returns the settlement activation height
// derived for a plan opened at openHeight, under the launch delay.
func testSettlementActivationHeight(openHeight int64) int64 {
	return openHeight + int64(types.DefaultSettlementActivationDelayBlocks)
}

// The announced window is now enforced against the one message that could
// break it. Committing to a redemption period and then derecognizing inside it
// is what this rejects; every other plan-ending path either restores pricing or
// requires the supply already gone.
func (s *KeeperTestSuite) TestWriteOffHonoursCommittedWindow() {
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        math.LegacyOneDec(),
		ActivationHeight:      10,
		EarliestClosingHeight: 40,
		OpenedHeight:          5,
	}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, plan))

	for _, height := range []int64{5, 10, 39} {
		s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
		s.bankKeeper.EXPECT().
			GetSupply(s.ctx, asset.Denom).
			Return(sdk.NewInt64Coin(asset.Denom, 100))

		err := s.keeper.WriteOffAsset(s.ctx, asset.Denom, asset.Version)
		s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
		s.Require().ErrorContains(err, "committed until height 40")
	}
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version,
	)
	storedPlan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(plan, storedPlan)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestWriteOffAfterCommittedWindow() {
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        math.LegacyOneDec(),
		ActivationHeight:      10,
		EarliestClosingHeight: 40,
		OpenedHeight:          5,
	}
	supply := sdk.NewInt64Coin(asset.Denom, 100)
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, plan))

	// The commitment has run, so derecognition is permitted and closes the plan
	// on its way out.
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(40)
	s.bankKeeper.EXPECT().GetSupply(s.ctx, asset.Denom).Return(supply)
	s.Require().NoError(s.keeper.WriteOffAsset(
		s.ctx,
		asset.Denom,
		asset.Version,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		asset.Version+1,
	)
	hasPlan, err := s.keeper.SettlementPlans.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().False(hasPlan)
}

// A suspended asset carrying no settlement plan has no announced window to
// honour, so write-off is unconditional on height.
func (s *KeeperTestSuite) TestWriteOffAssetWithoutSettlementPlan() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(25)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	// It takes effect in the block governance decides, and schedules nothing:
	// feed membership is not asset state, so no epoch has to turn first.
	s.Require().NoError(s.keeper.WriteOffAsset(
		s.ctx,
		asset.Denom,
		asset.Version,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		asset.Version+1,
	)
}

// Recovery closes an open settlement in the same act, and does so without
// consulting the announced window. Holders are not losing an exit — they are
// getting the ordinary one back, which is what the settlement substituted for.
func (s *KeeperTestSuite) TestRecoverAssetClosesSettlement() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	activation := testSettlementActivationHeight(10)
	s.Require().NoError(s.keeper.OpenSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.LegacyOneDec(),
		activation+100,
	))
	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)

	// Well inside the committed window, which recovery is exempt from. The
	// event manager is reset so the assertion below sees only recovery's own.
	s.ctx = sdk.UnwrapSDKContext(s.ctx).
		WithBlockHeight(activation + 1).
		WithEventManager(sdk.NewEventManager())
	s.Require().NoError(s.keeper.RecoverAsset(
		s.ctx,
		asset.Denom,
		asset.Version+1,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		asset.Version+2,
	)
	hasPlan, err := s.keeper.SettlementPlans.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().False(hasPlan, "recovery closed the plan")
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventAssetStatusChanged{
			Denom:     asset.Denom,
			OldStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			NewStatus: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			Version:   asset.Version + 2,
		},
		&types.EventSettlementClosed{
			SettlementPlan: plan,
			Version:        asset.Version + 2,
			ClosedHeight:   activation + 1,
		},
	)
}

// Retirement ends a spent settlement rather than refusing it. Plans live only
// on suspended assets, and suspended retirement already demands zero supply, so
// an attached plan is necessarily one nobody can still redeem against.
func (s *KeeperTestSuite) TestFinalizeRetirementClosesSpentSettlement() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(50)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        math.LegacyOneDec(),
		ActivationHeight:      10,
		EarliestClosingHeight: 40,
		OpenedHeight:          5,
	}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, plan))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(zeroAssetCoin(asset.Denom))

	s.Require().NoError(s.keeper.FinalizeRetirement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.ZeroInt(),
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		asset.Version+1,
	)
	hasPlan, err := s.keeper.SettlementPlans.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().False(hasPlan, "retirement closed the plan")
}

// TestOpenSettlementDelayFollowsParams proves the correction window is the
// parameter and not the launch constant: a shortened delay activates the plan
// that many blocks out, well before the launch delay would have reached.
func (s *KeeperTestSuite) TestOpenSettlementDelayFollowsParams() {
	const openHeight = 10

	params := types.DefaultParams()
	params.SettlementActivationDelayBlocks = 100
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(openHeight)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	s.Require().NoError(s.keeper.OpenSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.LegacyOneDec(),
		openHeight+200,
	))

	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(int64(openHeight+100), plan.ActivationHeight)
	// The launch delay would have put activation far beyond this, so only the
	// configured one can have produced it.
	s.Require().Less(plan.ActivationHeight, testSettlementActivationHeight(openHeight))
}
