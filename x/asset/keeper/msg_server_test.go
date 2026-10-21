package keeper_test

import (
	"strings"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/x/asset/keeper"
	"github.com/ararat-network/ark/x/asset/types"
)

func (s *KeeperTestSuite) TestMsgServerRejectsInvalidAuthority() {
	server := keeper.NewMsgServerImpl(s.keeper)
	invalidAuthority := sdk.AccAddress("not-gov").String()
	asset := types.DefaultGenesisState().Assets[0]

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "register asset",
			call: func() error {
				_, err := server.RegisterAsset(s.ctx, &types.MsgRegisterAsset{
					Authority: invalidAuthority,
					Denom:     asset.Denom,
				})
				return err
			},
		},
		{
			name: "cancel settlement",
			call: func() error {
				_, err := server.CancelSettlement(
					s.ctx,
					&types.MsgCancelSettlement{
						Authority:       invalidAuthority,
						Denom:           asset.Denom,
						ExpectedVersion: asset.Version,
					},
				)
				return err
			},
		},
		{
			name: "set emergency mandate",
			call: func() error {
				_, err := server.SetEmergencyMandate(
					s.ctx,
					&types.MsgSetEmergencyMandate{
						Authority:      invalidAuthority,
						Committee:      emergencyCommittee(),
						ActivationTime: 1,
						ExpiryTime:     100,
					},
				)
				return err
			},
		},
		{
			name: "halt issuance",
			call: func() error {
				_, err := server.HaltIssuance(
					s.ctx,
					&types.MsgHaltIssuance{
						Authority:       invalidAuthority,
						Denom:           asset.Denom,
						ExpectedVersion: asset.Version,
					},
				)
				return err
			},
		},
		{
			name: "resume issuance",
			call: func() error {
				_, err := server.ResumeIssuance(
					s.ctx,
					&types.MsgResumeIssuance{
						Authority:       invalidAuthority,
						Denom:           asset.Denom,
						ExpectedVersion: asset.Version,
					},
				)
				return err
			},
		},
		{
			name: "finalise retirement",
			call: func() error {
				_, err := server.FinaliseRetirement(
					s.ctx,
					&types.MsgFinaliseRetirement{
						Authority:       invalidAuthority,
						Denom:           asset.Denom,
						ExpectedVersion: asset.Version,
					},
				)
				return err
			},
		},
		{
			name: "suspend asset",
			call: func() error {
				_, err := server.SuspendAsset(s.ctx, &types.MsgSuspendAsset{
					Authority:       invalidAuthority,
					Denom:           asset.Denom,
					ExpectedVersion: asset.Version,
				})
				return err
			},
		},
		{
			name: "open settlement",
			call: func() error {
				_, err := server.OpenSettlement(
					s.ctx,
					&types.MsgOpenSettlement{
						Authority:        invalidAuthority,
						Denom:            asset.Denom,
						ExpectedVersion:  asset.Version,
						RedemptionRate:   math.LegacyOneDec(),
						SettlementWindow: 10,
					},
				)
				return err
			},
		},
		{
			name: "recover asset",
			call: func() error {
				_, err := server.RecoverAsset(
					s.ctx,
					&types.MsgRecoverAsset{
						Authority:       invalidAuthority,
						Denom:           asset.Denom,
						ExpectedVersion: asset.Version,
					},
				)
				return err
			},
		},
		{
			name: "write off asset",
			call: func() error {
				_, err := server.WriteOffAsset(
					s.ctx,
					&types.MsgWriteOffAsset{
						Authority:       invalidAuthority,
						Denom:           asset.Denom,
						ExpectedVersion: asset.Version,
					},
				)
				return err
			},
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.Require().ErrorContains(test.call(), "invalid authority")
		})
	}
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestMsgServerRoutesLifecycleMutation() {
	server := keeper.NewMsgServerImpl(s.keeper)
	asset := types.DefaultGenesisState().Assets[0]
	s.expectFreshDenom(asset.Denom)
	s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, asset.Metadata)

	response, err := server.RegisterAsset(s.ctx, &types.MsgRegisterAsset{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Denom:     asset.Denom,
	})
	s.Require().NoError(err)
	s.Require().NotNil(response)
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_ACTIVE,
		1,
	)
}

func (s *KeeperTestSuite) TestMsgServerRoutesSettlementMutation() {
	s.at(10)
	server := keeper.NewMsgServerImpl(s.keeper)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	response, err := server.OpenSettlement(s.ctx, &types.MsgOpenSettlement{
		Authority:        authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Denom:            asset.Denom,
		ExpectedVersion:  asset.Version,
		RedemptionRate:   math.LegacyOneDec(),
		SettlementWindow: 100,
	})
	s.Require().NoError(err)
	s.Require().NotNil(response)
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
}

// TestMsgServerRoutesEveryGovernanceTransition drives each governance transition through the msg
// server as the authority. A row's start and outcome identify its keeper method, so a handler wired
// to the wrong one fails here.
func (s *KeeperTestSuite) TestMsgServerRoutesEveryGovernanceTransition() {
	s.at(10)
	server := keeper.NewMsgServerImpl(s.keeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	assets := types.DefaultGenesisState().Assets

	tests := []struct {
		name  string
		from  types.AssetStatus
		setup func(denom string)
		call  func(denom string, version uint64) error
		want  types.AssetStatus
	}{
		{
			name: "halt issuance",
			from: types.AssetStatus_ASSET_STATUS_ACTIVE,
			call: func(denom string, version uint64) error {
				_, err := server.HaltIssuance(s.ctx, &types.MsgHaltIssuance{Authority: authority, Denom: denom, ExpectedVersion: version})
				return err
			},
			want: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		},
		{
			name: "resume issuance",
			from: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			call: func(denom string, version uint64) error {
				_, err := server.ResumeIssuance(s.ctx, &types.MsgResumeIssuance{Authority: authority, Denom: denom, ExpectedVersion: version})
				return err
			},
			want: types.AssetStatus_ASSET_STATUS_ACTIVE,
		},
		{
			name: "suspend asset",
			from: types.AssetStatus_ASSET_STATUS_ACTIVE,
			call: func(denom string, version uint64) error {
				_, err := server.SuspendAsset(s.ctx, &types.MsgSuspendAsset{Authority: authority, Denom: denom, ExpectedVersion: version})
				return err
			},
			want: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		},
		{
			name: "recover asset",
			from: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			call: func(denom string, version uint64) error {
				_, err := server.RecoverAsset(s.ctx, &types.MsgRecoverAsset{Authority: authority, Denom: denom, ExpectedVersion: version})
				return err
			},
			want: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		},
		{
			name: "cancel settlement",
			from: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			setup: func(denom string) {
				s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, denom, types.SettlementPlan{
					Denom:               denom,
					RedemptionRate:      math.LegacyOneDec(),
					ActivationTime:      blockTime(20),
					EarliestClosingTime: blockTime(30),
					OpenedHeight:        1,
					OpenedTime:          blockTime(1),
				}))
			},
			call: func(denom string, version uint64) error {
				_, err := server.CancelSettlement(s.ctx, &types.MsgCancelSettlement{Authority: authority, Denom: denom, ExpectedVersion: version})
				return err
			},
			want: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		},
		{
			name: "write off asset",
			from: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			setup: func(denom string) {
				s.bankKeeper.EXPECT().GetSupply(s.ctx, denom).Return(sdk.NewInt64Coin(denom, 100))
			},
			call: func(denom string, version uint64) error {
				_, err := server.WriteOffAsset(s.ctx, &types.MsgWriteOffAsset{Authority: authority, Denom: denom, ExpectedVersion: version})
				return err
			},
			want: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		},
		{
			name: "finalise retirement",
			from: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			setup: func(denom string) {
				s.bankKeeper.EXPECT().GetSupply(s.ctx, denom).Return(sdk.NewInt64Coin(denom, 0))
			},
			call: func(denom string, version uint64) error {
				_, err := server.FinaliseRetirement(s.ctx, &types.MsgFinaliseRetirement{
					Authority: authority, Denom: denom, ExpectedVersion: version, MaxResidualSupply: math.ZeroInt(),
				})
				return err
			},
			want: types.AssetStatus_ASSET_STATUS_RETIRED,
		},
	}

	for i, tc := range tests {
		s.Run(tc.name, func() {
			asset := assets[i]
			asset.Status = tc.from
			asset.Version = 2
			s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
			if tc.setup != nil {
				tc.setup(asset.Denom)
			}

			s.Require().NoError(tc.call(asset.Denom, asset.Version))
			s.requireStoredAsset(asset.Denom, tc.want, asset.Version+1)
		})
	}

	// Cancellation is the one row whose status does not move; the plan it removed is its outcome.
	hasPlan, err := s.keeper.SettlementPlans.Has(s.ctx, assets[4].Denom)
	s.Require().NoError(err)
	s.Require().False(hasPlan)
}

func (s *KeeperTestSuite) TestMsgServerRejectsNilLifecycleMessages() {
	server := keeper.NewMsgServerImpl(s.keeper)
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "register asset",
			call: func() error {
				_, err := server.RegisterAsset(s.ctx, nil)
				return err
			},
		},
		{
			name: "cancel settlement",
			call: func() error {
				_, err := server.CancelSettlement(s.ctx, nil)
				return err
			},
		},
		{
			name: "set emergency mandate",
			call: func() error {
				_, err := server.SetEmergencyMandate(s.ctx, nil)
				return err
			},
		},
		{
			name: "emergency suspend asset",
			call: func() error {
				_, err := server.EmergencySuspendAsset(s.ctx, nil)
				return err
			},
		},
		{
			name: "halt issuance",
			call: func() error {
				_, err := server.HaltIssuance(s.ctx, nil)
				return err
			},
		},
		{
			name: "resume issuance",
			call: func() error {
				_, err := server.ResumeIssuance(s.ctx, nil)
				return err
			},
		},
		{
			name: "finalise retirement",
			call: func() error {
				_, err := server.FinaliseRetirement(s.ctx, nil)
				return err
			},
		},
		{
			name: "suspend asset",
			call: func() error {
				_, err := server.SuspendAsset(s.ctx, nil)
				return err
			},
		},
		{
			name: "open settlement",
			call: func() error {
				_, err := server.OpenSettlement(s.ctx, nil)
				return err
			},
		},
		{
			name: "recover asset",
			call: func() error {
				_, err := server.RecoverAsset(s.ctx, nil)
				return err
			},
		},
		{
			name: "write off asset",
			call: func() error {
				_, err := server.WriteOffAsset(s.ctx, nil)
				return err
			},
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.Require().Error(test.call())
		})
	}
}

func (s *KeeperTestSuite) TestSetMandateRejectsAuthorityCommittee() {
	server := keeper.NewMsgServerImpl(s.keeper)
	s.Require().NoError(s.keeper.EmergencyMandate.Set(
		s.ctx,
		types.DefaultEmergencyMandate(),
	))

	govAuthority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	_, err := server.SetEmergencyMandate(s.ctx, &types.MsgSetEmergencyMandate{
		Authority:      govAuthority,
		Committee:      govAuthority,
		ActivationTime: 1,
		ExpiryTime:     100,
	})
	s.Require().ErrorContains(err, "distinct from the Asset authority")

	// The distinctness judgment runs on the canonical spelling, so re-casing
	// the authority is still the authority.
	_, err = server.SetEmergencyMandate(s.ctx, &types.MsgSetEmergencyMandate{
		Authority:      govAuthority,
		Committee:      strings.ToUpper(govAuthority),
		ActivationTime: 1,
		ExpiryTime:     100,
	})
	s.Require().ErrorContains(err, "distinct from the Asset authority")
}

func (s *KeeperTestSuite) TestConsensusAuthorityCannotBecomeEmergencyCommittee() {
	server := keeper.NewMsgServerImpl(s.keeper)
	s.Require().NoError(s.keeper.EmergencyMandate.Set(
		s.ctx,
		types.DefaultEmergencyMandate(),
	))

	consensusAuthority := authtypes.NewModuleAddress("consensus-authority").String()
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
		Authority: &cmtproto.AuthorityParams{Authority: consensusAuthority},
	})
	_, err := server.SetEmergencyMandate(s.ctx, &types.MsgSetEmergencyMandate{
		Authority:      consensusAuthority,
		Committee:      consensusAuthority,
		ActivationTime: 1,
		ExpiryTime:     100,
	})
	s.Require().ErrorContains(err, "distinct from the Asset authority")
}

func (s *KeeperTestSuite) TestMsgServerUpdateParams() {
	server := keeper.NewMsgServerImpl(s.keeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	updated := types.DefaultParams()
	updated.SettlementActivationDelay = 500
	_, err := server.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    updated,
	})
	s.Require().NoError(err)
	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(updated, stored)

	// A rejected update leaves the stored params untouched, so a proposal that
	// fails validation cannot half-apply.
	_, err = server.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    types.Params{SettlementActivationDelay: 0},
	})
	s.Require().ErrorContains(err, "SettlementActivationDelay must be between one and")

	_, err = server.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: sdk.AccAddress("not-gov").String(),
		Params:    types.DefaultParams(),
	})
	s.Require().ErrorContains(err, "invalid authority")

	stored, err = s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(updated, stored)
}

// TestMsgServerUpdateParamsLeavesOpenPlansAlone pins that the delay is checked
// when a plan is written and never again: shortening it must not reach terms
// holders have already been shown.
func (s *KeeperTestSuite) TestMsgServerUpdateParamsLeavesOpenPlansAlone() {
	const openHeight = 10

	s.at(openHeight)
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

	shortened := types.DefaultParams()
	shortened.SettlementActivationDelay = 1
	_, err := keeper.NewMsgServerImpl(s.keeper).UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Params:    shortened,
	})
	s.Require().NoError(err)

	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(activation, plan.ActivationTime)
}
