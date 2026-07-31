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
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/x/oracle/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgAddFeed{}, "ark/x/oracle/MsgAddFeed")
	legacy.RegisterAminoMsg(cdc, &MsgRemoveFeed{}, "ark/x/oracle/MsgRemoveFeed")
	legacy.RegisterAminoMsg(cdc, &MsgSetReferenceDenom{}, "ark/x/oracle/MsgSetReferenceDenom")

	cdc.RegisterConcrete(Params{}, "ark/x/oracle/Params", nil)
}

// RegisterInterfaces registers the x/oracle interfaces types with the interface registry
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgAddFeed{},
		&MsgRemoveFeed{},
		&MsgSetReferenceDenom{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
