package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers all Claims messages used by direct or
// governance transactions. Every name stays inside the 39-character limit
// RegisterAminoMsg enforces for Ledger signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/x/claims/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgSetClaimsMandate{}, "ark/x/claims/MsgSetClaimsMandate")
	legacy.RegisterAminoMsg(cdc, &MsgSubmitClaim{}, "ark/x/claims/MsgSubmitClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeSubmitClaim{}, "ark/x/claims/MsgCommitteeSubmitClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCancelClaim{}, "ark/x/claims/MsgCancelClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeCancelClaim{}, "ark/x/claims/MsgCommitteeCancelClaim")

	cdc.RegisterConcrete(Params{}, "ark/x/claims/Params", nil)
}

// RegisterInterfaces registers Claims messages as sdk.Msg implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgSetClaimsMandate{},
		&MsgSubmitClaim{},
		&MsgCommitteeSubmitClaim{},
		&MsgCancelClaim{},
		&MsgCommitteeCancelClaim{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
