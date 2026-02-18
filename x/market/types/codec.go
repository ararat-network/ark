package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the necessary x/market interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization
// (SIGN_MODE_LEGACY_AMINO_JSON), which is required for Ledger hardware wallet signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgSwap{}, "noah/MsgSwap")
	legacy.RegisterAminoMsg(cdc, &MsgSwapSend{}, "noah/MsgSwapSend")

	cdc.RegisterConcrete(&Params{}, "noah/market/Params", nil)
}

// RegisterInterfaces registers the x/market interfaces and implementations
// with the interface registry for Protobuf serialization (SIGN_MODE_DIRECT).
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSwap{},
		&MsgSwapSend{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}
