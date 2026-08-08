package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers all Treasury messages used by direct or
// governance transactions.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/treasury/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgSetMonetaryMandate{}, "ark/treasury/MsgSetMonetaryMandate")
	legacy.RegisterAminoMsg(cdc, &MsgUpdatePolicy{}, "ark/treasury/MsgUpdatePolicy")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeUpdatePolicy{}, "ark/treasury/MsgCommitteeUpdatePolicy")
}

// RegisterInterfaces registers Treasury messages as sdk.Msg implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgSetMonetaryMandate{},
		&MsgUpdatePolicy{},
		&MsgCommitteeUpdatePolicy{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
