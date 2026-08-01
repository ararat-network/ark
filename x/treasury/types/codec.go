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
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/x/treasury/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgSetMonetaryMandate{}, "ark/x/treasury/MsgSetMonetaryMandate")
	legacy.RegisterAminoMsg(cdc, &MsgUpdatePolicy{}, "ark/x/treasury/MsgUpdatePolicy")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeUpdatePolicy{}, "ark/x/treasury/MsgCommitteeUpdatePolicy")
	legacy.RegisterAminoMsg(cdc, &MsgSetClaimsMandate{}, "ark/x/treasury/MsgSetClaimsMandate")
	legacy.RegisterAminoMsg(cdc, &MsgSubmitClaim{}, "ark/x/treasury/MsgSubmitClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeSubmitClaim{}, "ark/x/treasury/MsgCommitteeSubmitClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCancelClaim{}, "ark/x/treasury/MsgCancelClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeCancelClaim{}, "ark/x/treasury/MsgCommitteeCancelClaim")
	legacy.RegisterAminoMsg(cdc, &MsgExecuteClaim{}, "ark/x/treasury/MsgExecuteClaim")
	legacy.RegisterAminoMsg(cdc, &MsgTransferReserveToBuffer{}, "ark/x/treasury/MsgTransferToBuffer")

	cdec := cdc
	cdec.RegisterConcrete(Params{}, "ark/x/treasury/Params", nil)
}

// RegisterInterfaces registers Treasury messages as sdk.Msg implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgSetMonetaryMandate{},
		&MsgUpdatePolicy{},
		&MsgCommitteeUpdatePolicy{},
		&MsgSetClaimsMandate{},
		&MsgSubmitClaim{},
		&MsgCommitteeSubmitClaim{},
		&MsgCancelClaim{},
		&MsgCommitteeCancelClaim{},
		&MsgExecuteClaim{},
		&MsgTransferReserveToBuffer{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
