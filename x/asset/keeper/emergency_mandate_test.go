package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/mandate"
	"ark/x/asset/types"
)

// absentCommitteeShape is the observation an appointment records for a
// committee address holding no account, which is every committee here: these
// suites drive the keeper directly rather than through a signed transaction,
// so no committee account is ever created.
var absentCommitteeShape = mandate.CommitteeShape{
	KeyKind: mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_ABSENT,
}

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
			CommitteeShape:   absentCommitteeShape,
		},
		&types.EventEmergencyMandateSet{Term: 2},
	)
}

func (s *KeeperTestSuite) TestEmergencySuspendConsumesOneSuspensionPerTerm() {
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
	used, err := s.keeper.EmergencySuspensions.Has(s.ctx, asset.Denom)
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
	s.Require().ErrorIs(err, types.ErrEmergencySuspensionConsumed)
}

// Suspension is the committee's only power, so it must reach an asset
// governance has already halted: a wind-down that turns into a peg failure is
// contained by the same single tool.
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

func (s *KeeperTestSuite) TestSetMandateClearsConsumedSuspensions() {
	term := s.seedLiveMandate()
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.EmergencySuspendAsset(
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
	used, err := s.keeper.EmergencySuspensions.Has(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().False(used)

	// The old term no longer authorises anything.
	err = s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	)
	s.Require().ErrorIs(err, types.ErrEmergencyMandateInactive)
}
