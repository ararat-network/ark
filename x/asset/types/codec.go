package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers concrete asset message types.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/x/asset/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgRegisterAsset{}, "ark/x/asset/MsgRegisterAsset")
	legacy.RegisterAminoMsg(cdc, &MsgHaltIssuance{}, "ark/x/asset/MsgHaltIssuance")
	legacy.RegisterAminoMsg(cdc, &MsgResumeIssuance{}, "ark/x/asset/MsgResumeIssuance")
	legacy.RegisterAminoMsg(cdc, &MsgSuspendAsset{}, "ark/x/asset/MsgSuspendAsset")
	legacy.RegisterAminoMsg(cdc, &MsgOpenSettlement{}, "ark/x/asset/MsgOpenSettlement")
	legacy.RegisterAminoMsg(cdc, &MsgCancelSettlement{}, "ark/x/asset/MsgCancelSettlement")
	legacy.RegisterAminoMsg(cdc, &MsgRecoverAsset{}, "ark/x/asset/MsgRecoverAsset")
	legacy.RegisterAminoMsg(cdc, &MsgWriteOffAsset{}, "ark/x/asset/MsgWriteOffAsset")
	legacy.RegisterAminoMsg(cdc, &MsgFinalizeRetirement{}, "ark/x/asset/MsgFinalizeRetirement")
	legacy.RegisterAminoMsg(cdc, &MsgSetEmergencyMandate{}, "ark/x/asset/MsgSetEmergencyMandate")
	legacy.RegisterAminoMsg(cdc, &MsgEmergencySuspendAsset{}, "ark/x/asset/MsgEmergencySuspendAsset")
	legacy.RegisterAminoMsg(cdc, &MsgEmergencyHaltIssuance{}, "ark/x/asset/MsgEmergencyHaltIssuance")
}

// RegisterInterfaces registers asset message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgRegisterAsset{},
		&MsgHaltIssuance{},
		&MsgResumeIssuance{},
		&MsgSuspendAsset{},
		&MsgOpenSettlement{},
		&MsgCancelSettlement{},
		&MsgRecoverAsset{},
		&MsgWriteOffAsset{},
		&MsgFinalizeRetirement{},
		&MsgSetEmergencyMandate{},
		&MsgEmergencySuspendAsset{},
		&MsgEmergencyHaltIssuance{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
