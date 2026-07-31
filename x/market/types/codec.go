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
	legacy.RegisterAminoMsg(cdc, &MsgSwap{}, "ark/x/market/MsgSwap")
	legacy.RegisterAminoMsg(cdc, &MsgSwapSend{}, "ark/x/market/MsgSwapSend")
	legacy.RegisterAminoMsg(cdc, &MsgSettle{}, "ark/x/market/MsgSettle")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/x/market/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgSetTobinTaxOverride{}, "ark/x/market/MsgSetTobinTaxOverride")
	legacy.RegisterAminoMsg(cdc, &MsgRemoveTobinTaxOverride{}, "ark/x/market/MsgRemoveTobinTaxOverride")
	legacy.RegisterAminoMsg(cdc, &MsgSetConversionMandate{}, "ark/x/market/MsgSetConversionMandate")
	legacy.RegisterAminoMsg(cdc, &MsgUpdatePolicy{}, "ark/x/market/MsgUpdatePolicy")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeUpdatePolicy{}, "ark/x/market/MsgCommitteeUpdatePolicy")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeSetTobinTax{}, "ark/x/market/MsgCommitteeSetTobinTax")

	cdc.RegisterConcrete(Params{}, "ark/x/market/Params", nil)
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
