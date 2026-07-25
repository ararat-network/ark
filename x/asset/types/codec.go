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
	legacy.RegisterAminoMsg(cdc, &MsgRegisterAsset{}, "ark/x/asset/MsgRegisterAsset")
	legacy.RegisterAminoMsg(cdc, &MsgSetOracleRequired{}, "ark/x/asset/MsgSetOracleRequired")
	legacy.RegisterAminoMsg(cdc, &MsgActivateAsset{}, "ark/x/asset/MsgActivateAsset")
	legacy.RegisterAminoMsg(cdc, &MsgCancelAssetRegistration{}, "ark/x/asset/MsgCancelAssetRegistration")
	legacy.RegisterAminoMsg(cdc, &MsgBeginRetirement{}, "ark/x/asset/MsgBeginRetirement")
	legacy.RegisterAminoMsg(cdc, &MsgCancelRetirement{}, "ark/x/asset/MsgCancelRetirement")
	legacy.RegisterAminoMsg(cdc, &MsgFinalizeRetirement{}, "ark/x/asset/MsgFinalizeRetirement")
	legacy.RegisterAminoMsg(cdc, &MsgReactivateAsset{}, "ark/x/asset/MsgReactivateAsset")
	legacy.RegisterAminoMsg(cdc, &MsgBeginDelisting{}, "ark/x/asset/MsgBeginDelisting")
	legacy.RegisterAminoMsg(cdc, &MsgOpenSettlement{}, "ark/x/asset/MsgOpenSettlement")
	legacy.RegisterAminoMsg(cdc, &MsgCancelSettlement{}, "ark/x/asset/MsgCancelSettlement")
	legacy.RegisterAminoMsg(cdc, &MsgBeginRelisting{}, "ark/x/asset/MsgBeginRelisting")
	legacy.RegisterAminoMsg(cdc, &MsgWriteOffAsset{}, "ark/x/asset/MsgWriteOffAsset")
}

// RegisterInterfaces registers asset message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterAsset{},
		&MsgSetOracleRequired{},
		&MsgActivateAsset{},
		&MsgCancelAssetRegistration{},
		&MsgBeginRetirement{},
		&MsgCancelRetirement{},
		&MsgFinalizeRetirement{},
		&MsgReactivateAsset{},
		&MsgBeginDelisting{},
		&MsgOpenSettlement{},
		&MsgCancelSettlement{},
		&MsgBeginRelisting{},
		&MsgWriteOffAsset{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
