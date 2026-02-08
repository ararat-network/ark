package types

import (
	marketv1 "noah/api/noah/market/v1"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the necessary x/market interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization
// (SIGN_MODE_LEGACY_AMINO_JSON), which is required for Ledger hardware wallet signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &marketv1.MsgSwap{}, "noah/MsgSwap")
	legacy.RegisterAminoMsg(cdc, &marketv1.MsgSwapSend{}, "noah/MsgSwapSend")

	cdc.RegisterConcrete(&marketv1.Params{}, "noah/market/Params", nil)
}

// RegisterInterfaces registers the x/market interfaces and implementations
// with the interface registry for Protobuf serialization (SIGN_MODE_DIRECT).
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&marketv1.MsgSwap{},
		&marketv1.MsgSwapSend{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &marketv1.Msg_ServiceDesc)
}
