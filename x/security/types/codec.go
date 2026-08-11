package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers concrete types on the LegacyAmino codec.
// The names use the bare `ark/security/` prefix rather than `ark/x/`, which
// keeps the longest inside amino's 39-character registration limit.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgSetSecurityMandate{}, "ark/security/MsgSetSecurityMandate")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteePlanUpgrade{}, "ark/security/MsgCommitteePlanUpgrade")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeCancelUpgrade{}, "ark/security/MsgCommitteeCancelUpgrade")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeRecoverClient{}, "ark/security/MsgCommitteeRecoverClient")
}

// RegisterInterfaces registers the interfaces types with the interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSetSecurityMandate{},
		&MsgCommitteePlanUpgrade{},
		&MsgCommitteeCancelUpgrade{},
		&MsgCommitteeRecoverClient{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
