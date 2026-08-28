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
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/asset/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgRegisterAsset{}, "ark/asset/MsgRegisterAsset")
	legacy.RegisterAminoMsg(cdc, &MsgHaltIssuance{}, "ark/asset/MsgHaltIssuance")
	legacy.RegisterAminoMsg(cdc, &MsgResumeIssuance{}, "ark/asset/MsgResumeIssuance")
	legacy.RegisterAminoMsg(cdc, &MsgSuspendAsset{}, "ark/asset/MsgSuspendAsset")
	legacy.RegisterAminoMsg(cdc, &MsgOpenSettlement{}, "ark/asset/MsgOpenSettlement")
	legacy.RegisterAminoMsg(cdc, &MsgCancelSettlement{}, "ark/asset/MsgCancelSettlement")
	legacy.RegisterAminoMsg(cdc, &MsgRecoverAsset{}, "ark/asset/MsgRecoverAsset")
	legacy.RegisterAminoMsg(cdc, &MsgWriteOffAsset{}, "ark/asset/MsgWriteOffAsset")
	legacy.RegisterAminoMsg(cdc, &MsgFinaliseRetirement{}, "ark/asset/MsgFinaliseRetirement")
	legacy.RegisterAminoMsg(cdc, &MsgSetEmergencyMandate{}, "ark/asset/MsgSetEmergencyMandate")
	legacy.RegisterAminoMsg(cdc, &MsgEmergencySuspendAsset{}, "ark/asset/MsgEmergencySuspendAsset")
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
		&MsgFinaliseRetirement{},
		&MsgSetEmergencyMandate{},
		&MsgEmergencySuspendAsset{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
