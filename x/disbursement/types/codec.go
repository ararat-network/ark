package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers disbursement message names.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "ark/disbursement/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgCreateGrant{}, "ark/disbursement/MsgCreateGrant")
	legacy.RegisterAminoMsg(cdc, &MsgSetGrantsMandate{}, "ark/disbursement/MsgSetGrantsMandate")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeRegister{}, "ark/disbursement/MsgCommitteeRegister")
	legacy.RegisterAminoMsg(cdc, &MsgRelease{}, "ark/disbursement/MsgRelease")
	legacy.RegisterAminoMsg(cdc, &MsgCancelGrants{}, "ark/disbursement/MsgCancelGrants")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeSuspend{}, "ark/disbursement/MsgCommitteeSuspend")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeReinstate{}, "ark/disbursement/MsgCommitteeReinstate")
	legacy.RegisterAminoMsg(cdc, &MsgReinstateMembers{}, "ark/disbursement/MsgReinstateMembers")
	legacy.RegisterAminoMsg(cdc, &MsgVoidSuspensions{}, "ark/disbursement/MsgVoidSuspensions")
	legacy.RegisterAminoMsg(cdc, &MsgSetPayee{}, "ark/disbursement/MsgSetPayee")
	legacy.RegisterAminoMsg(cdc, &MsgSetController{}, "ark/disbursement/MsgSetController")
	legacy.RegisterAminoMsg(cdc, &MsgReturnUnallocated{}, "ark/disbursement/MsgReturnUnallocated")
	legacy.RegisterAminoMsg(cdc, &MsgOpenTranche{}, "ark/disbursement/MsgOpenTranche")
	legacy.RegisterAminoMsg(cdc, &MsgAuthoriseConversion{}, "ark/disbursement/MsgAuthoriseConversion")
	legacy.RegisterAminoMsg(cdc, &MsgCancelConversion{}, "ark/disbursement/MsgCancelConversion")
	legacy.RegisterAminoMsg(cdc, &MsgConvert{}, "ark/disbursement/MsgConvert")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeCompensate{}, "ark/disbursement/MsgCommitteeCompensate")
	legacy.RegisterAminoMsg(cdc, &MsgCancelTermGrants{}, "ark/disbursement/MsgCancelTermGrants")
}

// RegisterInterfaces registers disbursement transaction messages.
func RegisterInterfaces(r codectypes.InterfaceRegistry) {
	r.RegisterImplementations((*sdk.Msg)(nil), &MsgUpdateParams{}, &MsgSetGrantsMandate{}, &MsgCreateGrant{}, &MsgCommitteeRegister{}, &MsgRelease{}, &MsgCancelGrants{}, &MsgCommitteeSuspend{}, &MsgCommitteeReinstate{}, &MsgReinstateMembers{}, &MsgVoidSuspensions{}, &MsgSetPayee{}, &MsgSetController{}, &MsgReturnUnallocated{}, &MsgOpenTranche{}, &MsgAuthoriseConversion{}, &MsgCancelConversion{}, &MsgConvert{}, &MsgCommitteeCompensate{}, &MsgCancelTermGrants{})
	msgservice.RegisterMsgServiceDesc(r, &_Msg_serviceDesc)
}
