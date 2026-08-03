package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/x/asset/types"
)

func emergencyCommittee() string {
	return authtypes.NewModuleAddress("asset-emergency-committee").String()
}

// seedLiveMandate appoints the committee over a window containing height 10
// and leaves the context at that height.
func (s *KeeperTestSuite) seedLiveMandate() uint64 {
	s.Require().NoError(s.keeper.EmergencyMandate.Set(
		s.ctx,
		types.DefaultEmergencyMandate(),
	))
	s.Require().NoError(s.keeper.SetEmergencyMandate(
		s.ctx,
		emergencyCommittee(),
		1,
		1_000,
	))
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)

	stored, err := s.keeper.EmergencyMandate.Get(s.ctx)
	s.Require().NoError(err)

	return stored.Term
}

func (s *KeeperTestSuite) TestSetMandateAdvancesTerm() {
	s.Require().NoError(s.keeper.EmergencyMandate.Set(
		s.ctx,
		types.DefaultEmergencyMandate(),
	))

	s.Require().NoError(s.keeper.SetEmergencyMandate(
		s.ctx,
		emergencyCommittee(),
		1,
		1_000,
	))
	stored, err := s.keeper.EmergencyMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), stored.Term)
	s.Require().Equal(emergencyCommittee(), stored.Committee)

	// Disabling retains the latest term so previously prepared committee
	// transactions cannot become valid again.
	s.Require().NoError(s.keeper.SetEmergencyMandate(s.ctx, "", 0, 0))
	stored, err = s.keeper.EmergencyMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), stored.Term)
	s.Require().True(stored.IsDisabled())

	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventEmergencyMandateSet{
			Term:             1,
			Committee:        emergencyCommittee(),
			ActivationHeight: 1,
			ExpiryHeight:     1_000,
		},
		&types.EventEmergencyMandateSet{Term: 2},
	)
}

func (s *KeeperTestSuite) TestEmergencySuspendConsumesOneActionPerTerm() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	asset.Version = 4
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.Require().NoError(s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	))
	// Committee actions are governance-equivalent mutations and advance the
	// asset version.
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
	used, err := s.keeper.EmergencyActions.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().True(used)

	// Re-suspending an asset governance has already recovered within the same
	// term is griefing and requires governance. The asset is restored to a
	// suspendable status first, so the consumed bound is the only thing left
	// that can reject.
	restored := asset
	restored.Status = types.AssetStatus_ASSET_STATUS_ACTIVE
	restored.Version = asset.Version + 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, restored))
	err = s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyActionConsumed)
}

func (s *KeeperTestSuite) TestEmergencyHaltConsumesOneActionPerTerm() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	asset.Version = 4
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.Require().NoError(s.keeper.EmergencyHaltIssuance(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		asset.Version+1,
	)
	used, err := s.keeper.EmergencyActions.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().True(used)
	// The status a halt lands in is shared with wind-downs and recoveries, so
	// the event is the only place a committee halt is distinguishable. The
	// mandate appointment from seedLiveMandate leads.
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventEmergencyMandateSet{
			Term:             term,
			Committee:        emergencyCommittee(),
			ActivationHeight: 1,
			ExpiryHeight:     1_000,
		},
		&types.EventAssetStatusChanged{
			Denom:     asset.Denom,
			OldStatus: types.AssetStatus_ASSET_STATUS_ACTIVE,
			NewStatus: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			Version:   asset.Version + 1,
		},
		&types.EventEmergencyHalted{
			Denom:   asset.Denom,
			Term:    term,
			Version: asset.Version + 1,
		},
	)

	// Governance resumes it, and the committee still cannot halt again: the
	// halt/resume race is the same griefing the suspension bound closes.
	resumed := asset
	resumed.Version = asset.Version + 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, resumed))
	err = s.keeper.EmergencyHaltIssuance(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyActionConsumed)
}

// The halt must never be a look the committee takes before suspending. Both
// powers spend one budget, so the ladder is refused at the bound rather than at
// a status check — the asset is left in a status the suspension would otherwise
// accept, so nothing else could be doing the rejecting.
func (s *KeeperTestSuite) TestEmergencyHaltForfeitsTheTermsSuspension() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.Require().NoError(s.keeper.EmergencyHaltIssuance(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	))
	err := s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyActionConsumed)
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		asset.Version+1,
	)
}

// The reverse ladder is impossible twice over: the shared bound rejects it, and
// a SUSPENDED asset could not be halted anyway.
func (s *KeeperTestSuite) TestEmergencySuspendForfeitsTheTermsHalt() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.Require().NoError(s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	))
	err := s.keeper.EmergencyHaltIssuance(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyActionConsumed)
}

// Only ACTIVE issues, so only ACTIVE can stop issuing. An already-halted asset
// is refused rather than silently spending the term's action.
func (s *KeeperTestSuite) TestEmergencyHaltRejectsNonActiveStatus() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	err := s.keeper.EmergencyHaltIssuance(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrInvalidAssetTransition)
}

func (s *KeeperTestSuite) TestEmergencyHaltRejectsInactiveMandate() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	err := s.keeper.EmergencyHaltIssuance(
		s.ctx,
		authtypes.NewModuleAddress("impostor").String(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)

	err = s.keeper.EmergencyHaltIssuance(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term+1,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)

	expired := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(1_000)
	err = s.keeper.EmergencyHaltIssuance(
		expired,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)

	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ACTIVE,
		asset.Version,
	)
}

// Suspension must reach an asset governance has already halted: a wind-down that
// turns into a peg failure is contained by the tool for peg failures. The
// committee's own halt cannot reach this path, because it spent the action.
func (s *KeeperTestSuite) TestEmergencySuspendFromIssuanceHalted() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	s.Require().NoError(s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
}

func (s *KeeperTestSuite) TestEmergencySuspendRejectsInactiveMandate() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	err := s.keeper.EmergencySuspendAsset(
		s.ctx,
		authtypes.NewModuleAddress("impostor").String(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)

	err = s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term+1,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)

	expired := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(1_000)
	err = s.keeper.EmergencySuspendAsset(
		expired,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)

	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ACTIVE,
		asset.Version,
	)
}

// Re-appointment is the only way to re-arm a committee that has spent an asset's
// action, and it is the escape hatch behind a halt the committee should not have
// reached for. It runs at governance speed, which is the acknowledged cost of
// the shared budget.
func (s *KeeperTestSuite) TestSetMandateClearsConsumedActions() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.EmergencyHaltIssuance(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	))

	// Replacement always advances the term, so clearing usage on replacement
	// is exactly term-scoping.
	s.Require().NoError(s.keeper.SetEmergencyMandate(
		s.ctx,
		emergencyCommittee(),
		1,
		1_000,
	))
	used, err := s.keeper.EmergencyActions.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().False(used)

	// The re-armed committee may now suspend the asset it halted, which is what
	// makes re-appointment a real remedy rather than a formality.
	stored, err := s.keeper.EmergencyMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		stored.Term,
	))
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+2,
	)

	// The old term no longer authorises anything.
	err = s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)
}
