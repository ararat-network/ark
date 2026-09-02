package keeper_test

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestRegisterAsset() {
	assets := types.DefaultGenesisState().Assets

	registered := assets[0]
	s.expectFreshDenom(registered.Denom)
	s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, registered.Metadata)
	s.Require().NoError(s.keeper.RegisterAsset(s.ctx, registered.Denom))
	s.requireStoredAsset(
		registered.Denom,
		types.AssetStatus_ASSET_STATUS_ACTIVE,
		1,
	)

	tombstone := assets[1]
	tombstone.Status = types.AssetStatus_ASSET_STATUS_RETIRED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, tombstone.Denom, tombstone))
	err := s.keeper.RegisterAsset(s.ctx, tombstone.Denom)
	s.Require().ErrorIs(err, types.ErrAssetAlreadyExists)

	// The denomination is the whole input, so the only malformed registration
	// left is a malformed denomination.
	err = s.keeper.RegisterAsset(s.ctx, "NotADenom")
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)

	// Registration owns the denomination's Bank metadata and requires the
	// zero-supply invariant to hold by construction.
	withSupply := assets[3]
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, withSupply.Denom).
		Return(sdk.NewInt64Coin(withSupply.Denom, 1))
	err = s.keeper.RegisterAsset(s.ctx, withSupply.Denom)
	s.Require().ErrorIs(err, types.ErrAssetSupplyNotZero)

	withMetadata := assets[4]
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, withMetadata.Denom).
		Return(zeroAssetCoin(withMetadata.Denom))
	s.bankKeeper.EXPECT().
		GetDenomMetaData(s.ctx, withMetadata.Denom).
		Return(withMetadata.Metadata, true)
	err = s.keeper.RegisterAsset(s.ctx, withMetadata.Denom)
	s.Require().ErrorIs(err, types.ErrAssetAlreadyExists)

	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventAssetRegistered{
			Asset: types.Asset{
				Denom:    registered.Denom,
				Metadata: registered.Metadata,
				Status:   types.AssetStatus_ASSET_STATUS_ACTIVE,
				Version:  1,
			},
		},
	)
}

// TestRegisterAssetRequiresActiveFeed pins the admission rule registration
// shares with recovery and genesis import. Only Active admits: Adding is
// refused with the rest, so a feed addition and the registration against it
// cannot share a proposal. No refusal leaves a registry row behind.
func (s *KeeperTestSuite) TestRegisterAssetRequiresActiveFeed() {
	assets := types.DefaultGenesisState().Assets

	for _, tc := range []struct {
		name   string
		asset  types.Asset
		phase  oracletypes.FeedPhase
		admits bool
	}{
		{"active", assets[0], oracletypes.FeedPhaseActive, true},
		{"adding", assets[1], oracletypes.FeedPhaseAdding, false},
		{"removing", assets[2], oracletypes.FeedPhaseRemoving, false},
		{"off", assets[3], oracletypes.FeedPhaseOff, false},
	} {
		s.Run(tc.name, func() {
			s.expectFreshDenom(tc.asset.Denom)
			s.feedPhases[tc.asset.Denom] = tc.phase

			if !tc.admits {
				err := s.keeper.RegisterAsset(s.ctx, tc.asset.Denom)
				s.Require().ErrorIs(err, types.ErrAssetNotPriceable)
				stored, err := s.keeper.Assets.Has(s.ctx, tc.asset.Denom)
				s.Require().NoError(err)
				s.Require().False(stored)

				return
			}

			s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, tc.asset.Metadata)
			s.Require().NoError(s.keeper.RegisterAsset(s.ctx, tc.asset.Denom))
			s.requireStoredAsset(
				tc.asset.Denom,
				types.AssetStatus_ASSET_STATUS_ACTIVE,
				1,
			)
		})
	}
}

// TestRegisterAssetReportsIdentityConflictsBeforeFeed pins the precondition
// order. A denomination already registered is a collision whatever its feed is
// doing, and reporting the feed instead would send governance to fix the wrong
// thing.
func (s *KeeperTestSuite) TestRegisterAssetReportsIdentityConflictsBeforeFeed() {
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.feedPhases[asset.Denom] = oracletypes.FeedPhaseOff

	err := s.keeper.RegisterAsset(s.ctx, asset.Denom)
	s.Require().ErrorIs(err, types.ErrAssetAlreadyExists)
}

// TestRegisterAssetDerivesMetadata pins the registration input surface. The
// denomination is everything a proposal supplies and every metadata field
// follows from it, so no value a later message would need to correct ever
// enters the record — which is why there is no amendment message to correct it.
func (s *KeeperTestSuite) TestRegisterAssetDerivesMetadata() {
	const denom = "agold"
	derived := chain.NativeAssetMetadata(denom)
	s.expectFreshDenom(denom)
	s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, derived)

	s.Require().NoError(s.keeper.RegisterAsset(s.ctx, denom))

	stored, err := s.keeper.Assets.Get(s.ctx, denom)
	s.Require().NoError(err)
	s.Require().Equal(derived, stored.Metadata)
	s.Require().Equal("gold", stored.Metadata.Display)
	s.Require().Equal("ArkGOLD", stored.Metadata.Name)
	s.Require().Equal("arkGOLD", stored.Metadata.Symbol)
}

// TestFinaliseRetirementRefusesActiveAsset pins the absence of a shortcut for a
// registration governance regrets. Registration admits outright, so an unwanted
// asset unwinds the way every other asset does — halt first, so redemption
// stays open across the decision — rather than through a cancellation that
// skipped the exit.
func (s *KeeperTestSuite) TestFinaliseRetirementRefusesActiveAsset() {
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	err := s.keeper.FinaliseRetirement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.ZeroInt(),
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)

	s.Require().NoError(s.keeper.HaltIssuance(s.ctx, asset.Denom, asset.Version))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(zeroAssetCoin(asset.Denom))
	s.Require().NoError(s.keeper.FinaliseRetirement(
		s.ctx,
		asset.Denom,
		asset.Version+1,
		math.ZeroInt(),
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		asset.Version+2,
	)
}

func (s *KeeperTestSuite) TestBeginAndResumeIssuance() {
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.Require().NoError(s.keeper.HaltIssuance(
		s.ctx,
		asset.Denom,
		asset.Version,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		asset.Version+1,
	)
	s.Require().NoError(s.keeper.ResumeIssuance(
		s.ctx,
		asset.Denom,
		asset.Version+1,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ACTIVE,
		asset.Version+2,
	)

	s.Require().NoError(s.keeper.HaltIssuance(
		s.ctx,
		asset.Denom,
		asset.Version+2,
	))
	// Resuming no longer consults feed state: retirement is immediate, so
	// there is no "removal pending" window left to block on. The status
	// precondition is the whole guard.
	s.Require().NoError(s.keeper.ResumeIssuance(
		s.ctx,
		asset.Denom,
		asset.Version+3,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ACTIVE,
		asset.Version+4,
	)

	err := s.keeper.HaltIssuance(s.ctx, asset.Denom, asset.Version+2)
	s.Require().ErrorIs(err, types.ErrAssetVersionMismatch)
}

func (s *KeeperTestSuite) TestFinaliseRetirement() {
	assets := types.DefaultGenesisState().Assets

	direct := []struct {
		asset  types.Asset
		status types.AssetStatus
	}{
		{
			asset:  assets[0],
			status: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		},
		{
			asset:  assets[1],
			status: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		},
	}
	for _, test := range direct {
		test.asset.Status = test.status
		s.Require().NoError(s.keeper.Assets.Set(
			s.ctx,
			test.asset.Denom,
			test.asset,
		))
		s.bankKeeper.EXPECT().
			GetSupply(s.ctx, test.asset.Denom).
			Return(zeroAssetCoin(test.asset.Denom))
		s.Require().NoError(s.keeper.FinaliseRetirement(
			s.ctx,
			test.asset.Denom,
			test.asset.Version,
			math.ZeroInt(),
		))
		s.requireStoredAsset(
			test.asset.Denom,
			types.AssetStatus_ASSET_STATUS_RETIRED,
			test.asset.Version+1,
		)
	}

	// A written-off asset retires directly, residual permitted, and appends no
	// new record: its derecognition is already disclosed by the WRITE_OFF.
	writtenOff := assets[2]
	writtenOff.Status = types.AssetStatus_ASSET_STATUS_WRITTEN_OFF
	s.Require().NoError(s.keeper.Assets.Set(
		s.ctx,
		writtenOff.Denom,
		writtenOff,
	))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, writtenOff.Denom).
		Return(sdk.NewInt64Coin(writtenOff.Denom, 9))
	s.Require().NoError(s.keeper.FinaliseRetirement(
		s.ctx,
		writtenOff.Denom,
		writtenOff.Version,
		math.ZeroInt(),
	))
	s.requireStoredAsset(
		writtenOff.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		writtenOff.Version+1,
	)
	hasRecord, err := s.keeper.ResolutionRecords.Has(
		s.ctx,
		collections.Join(writtenOff.Denom, writtenOff.Version+1),
	)
	s.Require().NoError(err)
	s.Require().False(hasRecord)

	// A priced ISSUANCE_HALTED asset retires in the same block: there is no
	// target removal left to wait on, so the residual bound is checked and the
	// record appended right here.
	priced := assets[3]
	priced.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, priced.Denom, priced))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, priced.Denom).
		Return(zeroAssetCoin(priced.Denom))
	s.Require().NoError(s.keeper.FinaliseRetirement(
		s.ctx,
		priced.Denom,
		priced.Version,
		math.ZeroInt(),
	))
	s.requireStoredAsset(
		priced.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		priced.Version+1,
	)

	// A fully settled suspended asset retires at zero supply without recovery,
	// write-off, or any feed transition.
	settled := assets[4]
	settled.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, settled.Denom, settled))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, settled.Denom).
		Return(zeroAssetCoin(settled.Denom))
	s.Require().NoError(s.keeper.FinaliseRetirement(
		s.ctx,
		settled.Denom,
		settled.Version,
		math.ZeroInt(),
	))
	s.requireStoredAsset(
		settled.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		settled.Version+1,
	)

	issued := assets[5]
	issued.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, issued.Denom, issued))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, issued.Denom).
		Return(sdk.NewInt64Coin(issued.Denom, 1))
	err = s.keeper.FinaliseRetirement(s.ctx, issued.Denom, issued.Version, math.ZeroInt())
	s.Require().ErrorIs(err, types.ErrAssetSupplyNotZero)
}

func (s *KeeperTestSuite) TestFinaliseRetirementDerecognisesApprovedResidual() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(30)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	asset.Version = 4
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	// Supply above the approved bound is rejected outright.
	residual := sdk.NewInt64Coin(asset.Denom, 25)
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(residual).
		Times(2)
	err := s.keeper.FinaliseRetirement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.NewInt(24),
	)
	s.Require().ErrorIs(err, types.ErrAssetSupplyNotZero)

	// Within the bound, retirement closes the books and discloses the actual
	// residual.
	s.Require().NoError(s.keeper.FinaliseRetirement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.NewInt(30),
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_RETIRED,
		asset.Version+1,
	)
	record, err := s.keeper.ResolutionRecords.Get(
		s.ctx,
		collections.Join(asset.Denom, asset.Version+1),
	)
	s.Require().NoError(err)
	s.Require().Equal(types.ResolutionRecord{
		Denom:             asset.Denom,
		Kind:              types.ResolutionKind_RESOLUTION_KIND_RETIREMENT_RESIDUAL,
		Version:           asset.Version + 1,
		ResolutionHeight:  30,
		OutstandingSupply: residual,
	}, record)
}

// TestFinaliseRetirementRejectsDuplicateResidualBeforeWriting is the retirement
// half of the rule TestWriteOffAssetRejectsUnsafeState covers for write-off: a
// derecognition that cannot record its outcome must not have moved the asset or
// closed its plan on the way to finding out.
func (s *KeeperTestSuite) TestFinaliseRetirementRejectsDuplicateResidualBeforeWriting() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(30)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	asset.Version = 4
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 25)).
		AnyTimes()
	s.Require().NoError(s.keeper.ResolutionRecords.Set(
		s.ctx,
		collections.Join(asset.Denom, asset.Version+1),
		types.ResolutionRecord{},
	))

	err := s.keeper.FinaliseRetirement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.NewInt(30),
	)

	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		asset.Version,
	)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

// Only ISSUANCE_HALTED may approve a residual, and the two statuses that may
// not are refused for different reasons. Each case asserts the message naming
// its own remedy: supply is what blocks a suspended retirement, so it must
// point at WriteOffAsset rather than at whatever bound the message carried.
func (s *KeeperTestSuite) TestFinaliseRetirementRejectsUnapprovableResidual() {
	assets := types.DefaultGenesisState().Assets

	for i, test := range []struct {
		name    string
		status  types.AssetStatus
		supply  int64
		bound   math.Int
		expErr  error
		expText string
	}{
		{
			// Holders here may have had no exit, so the remedy is the
			// write-off, not a smaller bound — and that is what the error says
			// even though the message also carried an unapprovable residual.
			name:    "suspended supply outranks the bound it carried",
			status:  types.AssetStatus_ASSET_STATUS_SUSPENDED,
			supply:  5,
			bound:   math.NewInt(5),
			expErr:  types.ErrAssetSupplyNotZero,
			expText: "write it off instead",
		},
		{
			// Cleared of supply, the asset could retire; the approval it
			// declares is what it may not do from this status.
			name:    "suspended asset declares an approval it may not make",
			status:  types.AssetStatus_ASSET_STATUS_SUSPENDED,
			supply:  0,
			bound:   math.NewInt(5),
			expErr:  types.ErrInvalidAssetTransition,
			expText: "cannot approve a residual",
		},
		{
			name:    "suspended asset retires only at zero supply",
			status:  types.AssetStatus_ASSET_STATUS_SUSPENDED,
			supply:  5,
			bound:   math.ZeroInt(),
			expErr:  types.ErrAssetSupplyNotZero,
			expText: "write it off instead",
		},
		{
			// The residual is real and permitted here — the WRITE_OFF record
			// disclosed it — so the refusal is of the approval, not the supply.
			name:    "written-off asset re-approves a disclosed residual",
			status:  types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			supply:  9,
			bound:   math.NewInt(1),
			expErr:  types.ErrInvalidAssetTransition,
			expText: "already disclosed its residual",
		},
	} {
		s.Run(test.name, func() {
			asset := assets[i]
			asset.Status = test.status
			s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
			s.bankKeeper.EXPECT().
				GetSupply(s.ctx, asset.Denom).
				Return(sdk.NewInt64Coin(asset.Denom, test.supply))

			err := s.keeper.FinaliseRetirement(
				s.ctx,
				asset.Denom,
				asset.Version,
				test.bound,
			)

			s.Require().ErrorIs(err, test.expErr)
			s.Require().ErrorContains(err, test.expText)
			s.requireStoredAsset(asset.Denom, test.status, asset.Version)
		})
	}
}

// TestRetirementIsTerminal pins the lifecycle's one irreversible act: nothing
// leads out of RETIRED. Every transition is run against the same tombstone
// rather than one apiece, so a new way back out fails here whichever status it
// would have arrived from.
//
// No collaborator is primed, and that is part of what the test asserts: each
// transition answers status before it reads supply or a feed phase, so a
// tombstone is refused without anything else being consulted. A transition that
// started reading first would fail here on an unexpected call.
func (s *KeeperTestSuite) TestRetirementIsTerminal() {
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_RETIRED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	for _, test := range []struct {
		name       string
		transition func() error
	}{
		{"halt", func() error {
			return s.keeper.HaltIssuance(s.ctx, asset.Denom, asset.Version)
		}},
		{"resume", func() error {
			return s.keeper.ResumeIssuance(s.ctx, asset.Denom, asset.Version)
		}},
		{"suspend", func() error {
			return s.keeper.SuspendAsset(s.ctx, asset.Denom, asset.Version)
		}},
		{"recover", func() error {
			return s.keeper.RecoverAsset(s.ctx, asset.Denom, asset.Version)
		}},
		{"write off", func() error {
			return s.keeper.WriteOffAsset(s.ctx, asset.Denom, asset.Version)
		}},
		{"open a settlement", func() error {
			return s.keeper.OpenSettlement(
				s.ctx,
				asset.Denom,
				asset.Version,
				math.LegacyOneDec(),
				sdk.UnwrapSDKContext(s.ctx).BlockHeight()+1,
			)
		}},
		{"cancel a settlement", func() error {
			return s.keeper.CancelSettlement(s.ctx, asset.Denom, asset.Version)
		}},
		{"retire again", func() error {
			return s.keeper.FinaliseRetirement(
				s.ctx,
				asset.Denom,
				asset.Version,
				math.ZeroInt(),
			)
		}},
	} {
		s.Run(test.name, func() {
			s.Require().ErrorIs(test.transition(), types.ErrInvalidAssetTransition)
			s.requireStoredAsset(
				asset.Denom,
				types.AssetStatus_ASSET_STATUS_RETIRED,
				asset.Version,
			)
		})
	}
}

func (s *KeeperTestSuite) TestSuspendAsset() {
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.Require().NoError(s.keeper.SuspendAsset(
		s.ctx,
		asset.Denom,
		asset.Version,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
	// Suspension is a pure status move: the feed keeps running because it may
	// price other assets, and the status gate is the containment.
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventAssetStatusChanged{
			Denom:     asset.Denom,
			OldStatus: types.AssetStatus_ASSET_STATUS_ACTIVE,
			NewStatus: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			Version:   asset.Version + 1,
		},
	)

	err := s.keeper.SuspendAsset(
		s.ctx,
		asset.Denom,
		asset.Version+1,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
}

// Suspension has no blocker beyond its status precondition. Nothing about feed
// state blocks it: the feed keeps running, because containment is the status
// gate every consumer already honours rather than the absence of a rate.
func (s *KeeperTestSuite) TestSuspendAssetHasNoBlockerBeyondStatus() {
	assets := types.DefaultGenesisState().Assets

	other := assets[2]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, other.Denom, other))
	s.Require().NoError(s.keeper.SuspendAsset(
		s.ctx,
		other.Denom,
		other.Version,
	))
	s.requireStoredAsset(
		other.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		other.Version+1,
	)
}

func (s *KeeperTestSuite) TestRecoverAsset() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	assets := types.DefaultGenesisState().Assets

	// Recovery takes effect in the block it executes: governance has already
	// satisfied itself the feed is producing a stable price, so there is
	// nothing left to wait for and no completion is requested.
	suspended := assets[0]
	suspended.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	suspended.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, suspended.Denom, suspended))
	s.Require().NoError(s.keeper.RecoverAsset(
		s.ctx,
		suspended.Denom,
		suspended.Version,
	))
	s.requireStoredAsset(
		suspended.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		suspended.Version+1,
	)
	// Recovery is not repeatable: the asset is no longer in a recoverable
	// status, and the stale version is rejected regardless.
	err := s.keeper.RecoverAsset(
		s.ctx,
		suspended.Denom,
		suspended.Version+1,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)

	// A written-off asset recovers to the same status in one mutation rather
	// than stopping at SUSPENDED to wait.
	writtenOff := assets[1]
	writtenOff.Status = types.AssetStatus_ASSET_STATUS_WRITTEN_OFF
	writtenOff.Version = 3
	s.Require().NoError(s.keeper.Assets.Set(
		s.ctx,
		writtenOff.Denom,
		writtenOff,
	))
	s.Require().NoError(s.keeper.RecoverAsset(
		s.ctx,
		writtenOff.Denom,
		writtenOff.Version,
	))
	s.requireStoredAsset(
		writtenOff.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		writtenOff.Version+1,
	)
}

// Recovery answers the same admission rule as registration and genesis import.
// An adding feed has no rate yet, so a transition taking effect immediately
// cannot use it; a removing feed was already cleared of referents.
func (s *KeeperTestSuite) TestRecoverAssetRequiresActiveFeed() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	assets := types.DefaultGenesisState().Assets

	for _, tc := range []struct {
		name  string
		asset types.Asset
		phase oracletypes.FeedPhase
	}{
		{"adding", assets[0], oracletypes.FeedPhaseAdding},
		{"removing", assets[1], oracletypes.FeedPhaseRemoving},
		{"off", assets[2], oracletypes.FeedPhaseOff},
	} {
		s.Run(tc.name, func() {
			asset := tc.asset
			asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
			asset.Version = 2
			s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
			s.feedPhases[asset.Denom] = tc.phase

			err := s.keeper.RecoverAsset(s.ctx, asset.Denom, asset.Version)
			s.Require().ErrorIs(err, types.ErrAssetNotPriceable)
			s.requireStoredAsset(
				asset.Denom,
				types.AssetStatus_ASSET_STATUS_SUSPENDED,
				asset.Version,
			)
		})
	}
}

func zeroAssetCoin(denom string) sdk.Coin {
	return sdk.NewCoin(denom, math.ZeroInt())
}

func (s *KeeperTestSuite) requireStoredAsset(
	denom string,
	status types.AssetStatus,
	version uint64,
) {
	asset, err := s.keeper.Assets.Get(s.ctx, denom)
	s.Require().NoError(err)
	s.Require().Equal(status, asset.Status)
	s.Require().Equal(version, asset.Version)
}
