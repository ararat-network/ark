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
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/claims/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgSetClaimsMandate{}, "ark/claims/MsgSetClaimsMandate")
	legacy.RegisterAminoMsg(cdc, &MsgSubmitClaim{}, "ark/claims/MsgSubmitClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeSubmitClaim{}, "ark/claims/MsgCommitteeSubmitClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCancelClaim{}, "ark/claims/MsgCancelClaim")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeCancelClaim{}, "ark/claims/MsgCommitteeCancelClaim")
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
