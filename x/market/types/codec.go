package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers concrete types on the LegacyAmino codec
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgSwap{}, "ark/market/MsgSwap")
	legacy.RegisterAminoMsg(cdc, &MsgSwapSend{}, "ark/market/MsgSwapSend")
	legacy.RegisterAminoMsg(cdc, &MsgSettle{}, "ark/market/MsgSettle")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/market/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgSetTobinTaxOverride{}, "ark/market/MsgSetTobinTaxOverride")
	legacy.RegisterAminoMsg(cdc, &MsgRemoveTobinTaxOverride{}, "ark/market/MsgRemoveTobinTaxOverride")
	legacy.RegisterAminoMsg(cdc, &MsgSetConversionMandate{}, "ark/market/MsgSetConversionMandate")
	legacy.RegisterAminoMsg(cdc, &MsgUpdatePolicy{}, "ark/market/MsgUpdatePolicy")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeUpdatePolicy{}, "ark/market/MsgCommitteeUpdatePolicy")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeSetTobinTax{}, "ark/market/MsgCommitteeSetTobinTax")
}

// RegisterInterfaces registers the interfaces types with the interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSwap{},
		&MsgSwapSend{},
		&MsgSettle{},
		&MsgUpdateParams{},
		&MsgSetTobinTaxOverride{},
		&MsgRemoveTobinTaxOverride{},
		&MsgSetConversionMandate{},
		&MsgUpdatePolicy{},
		&MsgCommitteeUpdatePolicy{},
		&MsgCommitteeSetTobinTax{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
