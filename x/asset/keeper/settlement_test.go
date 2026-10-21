package keeper_test

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/asset/types"
)

func (s *KeeperTestSuite) TestOpenSettlement() {
	s.at(10)
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
		100,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(types.SettlementPlan{
		Denom:               asset.Denom,
		RedemptionRate:      rate,
		ActivationTime:      testSettlementActivationTime(),
		EarliestClosingTime: testSettlementActivationTime() + 100,
		OpenedHeight:        10,
		OpenedTime:          blockTime(10),
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
	s.at(10)
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
		100,
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
	s.at(10)
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
		100,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)

	hasPlan, hasErr := s.keeper.SettlementPlans.Has(s.ctx, zeroSupply.Denom)
	s.Require().NoError(hasErr)
	s.Require().False(hasPlan)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestOpenSettlementRejectsInvalidTerms() {
	s.at(10)
	assets := types.DefaultGenesisState().Assets

	for i, test := range []struct {
		name   string
		rate   math.LegacyDec
		window uint64
	}{
		{
			name:   "redemption rate is not positive",
			rate:   math.LegacyZeroDec(),
			window: 100,
		},
		{
			// The window is mandatory: a plan with none would be one
			// WriteOffAsset could end the block after it activates.
			name:   "no window announced",
			rate:   math.LegacyOneDec(),
			window: 0,
		},
		{
			// The window is a domain-capped input, so the one timing mistake a
			// proposal can still make is asking for more than the cap.
			name:   "window past the cap",
			rate:   math.LegacyOneDec(),
			window: types.MaxSettlementWindow + 1,
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
				test.window,
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
	s.at(10)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:               asset.Denom,
		RedemptionRate:      math.LegacyOneDec(),
		ActivationTime:      blockTime(20),
		EarliestClosingTime: blockTime(30),
		OpenedHeight:        1,
		OpenedTime:          blockTime(1),
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

// From the activation time onward the plan is a hard commitment. Cancellation
// is the correction window only, so the boundary block is already too late.
func (s *KeeperTestSuite) TestCancelSettlementRejectedFromActivation() {
	assets := types.DefaultGenesisState().Assets
	boundary := assets[0]
	boundary.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	boundary.Version = 2
	plan := types.SettlementPlan{
		Denom:               boundary.Denom,
		RedemptionRate:      math.LegacyOneDec(),
		ActivationTime:      blockTime(20),
		EarliestClosingTime: blockTime(30),
		OpenedHeight:        1,
		OpenedTime:          blockTime(1),
	}
	s.at(20)
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
	s.at(25)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:               asset.Denom,
		RedemptionRate:      math.LegacyNewDecWithPrec(25, 1),
		ActivationTime:      blockTime(10),
		EarliestClosingTime: blockTime(20),
		OpenedHeight:        5,
		OpenedTime:          blockTime(5),
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
	s.at(25)
	assets := types.DefaultGenesisState().Assets

	// Write-off depends on supply and status, without consulting feed state.
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

// TestResolutionRecordsAccumulateAcrossVersions checks unique (denom, version) records across
// repeated resolution. Write-off followed by retirement preserves the earlier disclosure;
// retirement is terminal.
func (s *KeeperTestSuite) TestResolutionRecordsAccumulateAcrossVersions() {
	s.at(25)
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
	s.Require().NoError(s.keeper.FinaliseRetirement(
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

// testSettlementActivationTime returns the settlement activation time derived for a plan opened at
// block 10, under the launch delay.
func testSettlementActivationTime() uint64 {
	return blockTime(10) + types.DefaultSettlementActivationDelay
}

// An active settlement plan prevents write-off inside its committed redemption window. Other
// plan-ending paths restore pricing or require zero supply.
func (s *KeeperTestSuite) TestWriteOffHonoursCommittedWindow() {
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:               asset.Denom,
		RedemptionRate:      math.LegacyOneDec(),
		ActivationTime:      blockTime(10),
		EarliestClosingTime: blockTime(40),
		OpenedHeight:        5,
		OpenedTime:          blockTime(5),
	}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, plan))

	for _, height := range []int64{5, 10, 39} {
		s.at(height)
		s.bankKeeper.EXPECT().
			GetSupply(s.ctx, asset.Denom).
			Return(sdk.NewInt64Coin(asset.Denom, 100))

		err := s.keeper.WriteOffAsset(s.ctx, asset.Denom, asset.Version)
		s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
		s.Require().ErrorContains(err, fmt.Sprintf("committed until time %d", plan.EarliestClosingTime))
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
		Denom:               asset.Denom,
		RedemptionRate:      math.LegacyOneDec(),
		ActivationTime:      blockTime(10),
		EarliestClosingTime: blockTime(40),
		OpenedHeight:        5,
		OpenedTime:          blockTime(5),
	}
	supply := sdk.NewInt64Coin(asset.Denom, 100)
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, plan))

	// The commitment has run, so derecognition is permitted and closes the plan
	// on its way out.
	s.at(40)
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
// honour, so write-off is unconditional on time.
func (s *KeeperTestSuite) TestWriteOffAssetWithoutSettlementPlan() {
	s.at(25)
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
	s.at(10)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	activation := testSettlementActivationTime()
	s.Require().NoError(s.keeper.OpenSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.LegacyOneDec(),
		100,
	))
	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)

	// Well inside the committed window, which recovery is exempt from. The
	// event manager is reset so the assertion below sees only recovery's own.
	const recoveryHeight = 11
	s.atClock(recoveryHeight, activation+1)
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithEventManager(sdk.NewEventManager())
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
			ClosedHeight:   recoveryHeight,
		},
	)
}

// Retirement ends a spent settlement rather than refusing it. Plans live only
// on suspended assets, and suspended retirement already demands zero supply, so
// an attached plan is necessarily one nobody can still redeem against.
func (s *KeeperTestSuite) TestFinaliseRetirementClosesSpentSettlement() {
	s.at(50)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	plan := types.SettlementPlan{
		Denom:               asset.Denom,
		RedemptionRate:      math.LegacyOneDec(),
		ActivationTime:      blockTime(10),
		EarliestClosingTime: blockTime(40),
		OpenedHeight:        5,
		OpenedTime:          blockTime(5),
	}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, plan))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(zeroAssetCoin(asset.Denom))

	s.Require().NoError(s.keeper.FinaliseRetirement(
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
// that many seconds out, well before the launch delay would have reached.
func (s *KeeperTestSuite) TestOpenSettlementDelayFollowsParams() {
	const openHeight = 10

	params := types.DefaultParams()
	params.SettlementActivationDelay = 100
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	s.at(openHeight)
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
		200,
	))

	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(blockTime(openHeight)+100, plan.ActivationTime)
	// The launch delay would have put activation far beyond this, so only the
	// configured one can have produced it.
	s.Require().Less(plan.ActivationTime, testSettlementActivationTime())
}

// TestSettlementActivationFollowsBlockTime pins activation to block time: the redemption gate and
// the cancellation window follow the clock whatever the height reads.
func (s *KeeperTestSuite) TestSettlementActivationFollowsBlockTime() {
	const activation = 500
	assets := types.DefaultGenesisState().Assets

	for i, test := range []struct {
		name    string
		height  int64
		seconds uint64
		active  bool
	}{
		{"height past activation, time short of it", activation + 100, activation - 1, false},
		{"time at activation, height short of it", activation - 100, activation, true},
	} {
		s.Run(test.name, func() {
			asset := assets[i]
			asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
			asset.Version = 2
			s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
			s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, types.SettlementPlan{
				Denom:               asset.Denom,
				RedemptionRate:      math.LegacyOneDec(),
				ActivationTime:      activation,
				EarliestClosingTime: activation + 100,
				OpenedHeight:        1,
				OpenedTime:          1,
			}))
			s.atClock(test.height, test.seconds)

			_, active, err := s.keeper.ActiveSettlementPlan(s.ctx, asset.Denom)
			s.Require().NoError(err)
			s.Require().Equal(test.active, active)

			err = s.keeper.CancelSettlement(s.ctx, asset.Denom, asset.Version)
			if test.active {
				s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
				s.Require().ErrorContains(err, "can no longer be cancelled")
				return
			}
			s.Require().NoError(err)
		})
	}
}

// TestWriteOffClosingFollowsBlockTime pins the committed window to block time, and the record to
// the height it was written at.
func (s *KeeperTestSuite) TestWriteOffClosingFollowsBlockTime() {
	const closing = 500
	assets := types.DefaultGenesisState().Assets

	for i, test := range []struct {
		name    string
		height  int64
		seconds uint64
		closed  bool
	}{
		{"height past closing, time short of it", closing + 100, closing - 1, false},
		{"time at closing, height short of it", closing - 100, closing, true},
	} {
		s.Run(test.name, func() {
			asset := assets[i]
			asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
			asset.Version = 2
			s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
			s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, asset.Denom, types.SettlementPlan{
				Denom:               asset.Denom,
				RedemptionRate:      math.LegacyOneDec(),
				ActivationTime:      closing - 100,
				EarliestClosingTime: closing,
				OpenedHeight:        1,
				OpenedTime:          1,
			}))
			s.atClock(test.height, test.seconds)
			s.bankKeeper.EXPECT().
				GetSupply(s.ctx, asset.Denom).
				Return(sdk.NewInt64Coin(asset.Denom, 100))

			err := s.keeper.WriteOffAsset(s.ctx, asset.Denom, asset.Version)
			if !test.closed {
				s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
				s.Require().ErrorContains(err, "committed until time 500")
				s.requireStoredAsset(asset.Denom, types.AssetStatus_ASSET_STATUS_SUSPENDED, asset.Version)
				return
			}
			s.Require().NoError(err)
			s.requireStoredAsset(asset.Denom, types.AssetStatus_ASSET_STATUS_WRITTEN_OFF, asset.Version+1)
			record, err := s.keeper.ResolutionRecords.Get(s.ctx, collections.Join(asset.Denom, asset.Version+1))
			s.Require().NoError(err)
			s.Require().Equal(uint64(test.height), record.ResolutionHeight)
		})
	}
}
