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
						Authority:        invalidAuthority,
						Committee:        emergencyCommittee(),
						ActivationHeight: 1,
						ExpiryHeight:     100,
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
				_, err := server.FinalizeRetirement(
					s.ctx,
					&types.MsgFinalizeRetirement{
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
						Authority:             invalidAuthority,
						Denom:                 asset.Denom,
						ExpectedVersion:       asset.Version,
						RedemptionRate:        math.LegacyOneDec(),
						EarliestClosingHeight: 10,
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
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(10)
	server := keeper.NewMsgServerImpl(s.keeper)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	response, err := server.OpenSettlement(s.ctx, &types.MsgOpenSettlement{
		Authority:             authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Denom:                 asset.Denom,
		ExpectedVersion:       asset.Version,
		RedemptionRate:        math.LegacyOneDec(),
		EarliestClosingHeight: 110 + int64(types.DefaultSettlementActivationDelayBlocks),
	})
	s.Require().NoError(err)
	s.Require().NotNil(response)
	s.requireStoredAsset(
		asset.Denom,
		types.AssetStatus_ASSET_STATUS_SUSPENDED,
		asset.Version+1,
	)
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
				_, err := server.FinalizeRetirement(s.ctx, nil)
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
		Authority:        govAuthority,
		Committee:        govAuthority,
		ActivationHeight: 1,
		ExpiryHeight:     100,
	})
	s.Require().ErrorContains(err, "distinct from the Asset authority")

	// The distinctness judgment runs on the canonical spelling, so re-casing
	// the authority is still the authority.
	_, err = server.SetEmergencyMandate(s.ctx, &types.MsgSetEmergencyMandate{
		Authority:        govAuthority,
		Committee:        strings.ToUpper(govAuthority),
		ActivationHeight: 1,
		ExpiryHeight:     100,
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
		Authority:        consensusAuthority,
		Committee:        consensusAuthority,
		ActivationHeight: 1,
		ExpiryHeight:     100,
	})
	s.Require().ErrorContains(err, "distinct from the Asset authority")
}

func (s *KeeperTestSuite) TestMsgServerUpdateParams() {
	server := keeper.NewMsgServerImpl(s.keeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	updated := types.DefaultParams()
	updated.SettlementActivationDelayBlocks = 500
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
		Params:    types.Params{SettlementActivationDelayBlocks: 0},
	})
	s.Require().ErrorContains(err, "SettlementActivationDelayBlocks must be between one and")

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

	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(openHeight)
	asset := types.DefaultGenesisState().Assets[0]
	asset.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	asset.Version = 2
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, asset.Denom).
		Return(sdk.NewInt64Coin(asset.Denom, 100))

	activation := testSettlementActivationHeight(openHeight)
	s.Require().NoError(s.keeper.OpenSettlement(
		s.ctx,
		asset.Denom,
		asset.Version,
		math.LegacyOneDec(),
		activation+100,
	))

	shortened := types.DefaultParams()
	shortened.SettlementActivationDelayBlocks = 1
	_, err := keeper.NewMsgServerImpl(s.keeper).UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Params:    shortened,
	})
	s.Require().NoError(err)

	plan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(activation, plan.ActivationHeight)
}
