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
	legacy.RegisterAminoMsg(cdc, &MsgAddBurnTaxExemptionAddress{}, "noah/x/treasury/MsgAddBurnTaxExemptionAddress")
	legacy.RegisterAminoMsg(cdc, &MsgRemoveBurnTaxExemptionAddress{}, "noah/x/treasury/MsgRemoveBurnTaxExemptionAddress")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "noah/x/treasury/MsgUpdateParams")

	cdc.RegisterConcrete(Params{}, "noah/x/treasury/Params", nil)
}

// RegisterInterfaces registers the interfaces types with the interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgAddBurnTaxExemptionAddress{},
		&MsgAddBurnTaxExemptionAddress{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
