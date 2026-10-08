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
	legacy.RegisterAminoMsg(cdc, &MsgSetGrantsMandate{}, "ark/disbursement/MsgSetGrantsMandate")
	legacy.RegisterAminoMsg(cdc, &MsgOpenTranche{}, "ark/disbursement/MsgOpenTranche")
	legacy.RegisterAminoMsg(cdc, &MsgReturnUnallocated{}, "ark/disbursement/MsgReturnUnallocated")
	legacy.RegisterAminoMsg(cdc, &MsgGrantOwnership{}, "ark/disbursement/MsgGrantOwnership")
	legacy.RegisterAminoMsg(cdc, &MsgCompensate{}, "ark/disbursement/MsgCompensate")
	legacy.RegisterAminoMsg(cdc, &MsgCancelGrants{}, "ark/disbursement/MsgCancelGrants")
	legacy.RegisterAminoMsg(cdc, &MsgReinstateMembers{}, "ark/disbursement/MsgReinstateMembers")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeRegistrar{}, "ark/disbursement/MsgCommitteeRegistrar")
	legacy.RegisterAminoMsg(cdc, &MsgRegisterMembers{}, "ark/disbursement/MsgRegisterMembers")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeSuspend{}, "ark/disbursement/MsgCommitteeSuspend")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeReinstate{}, "ark/disbursement/MsgCommitteeReinstate")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeCompensate{}, "ark/disbursement/MsgCommitteeCompensate")
	legacy.RegisterAminoMsg(cdc, &MsgSetPayee{}, "ark/disbursement/MsgSetPayee")
	legacy.RegisterAminoMsg(cdc, &MsgSetController{}, "ark/disbursement/MsgSetController")
}

// RegisterInterfaces registers disbursement transaction messages.
func RegisterInterfaces(r codectypes.InterfaceRegistry) {
	r.RegisterImplementations((*sdk.Msg)(nil), &MsgUpdateParams{}, &MsgSetGrantsMandate{}, &MsgOpenTranche{}, &MsgReturnUnallocated{}, &MsgGrantOwnership{}, &MsgCompensate{}, &MsgCancelGrants{}, &MsgReinstateMembers{}, &MsgCommitteeRegistrar{}, &MsgRegisterMembers{}, &MsgCommitteeSuspend{}, &MsgCommitteeReinstate{}, &MsgCommitteeCompensate{}, &MsgSetPayee{}, &MsgSetController{})
	msgservice.RegisterMsgServiceDesc(r, &_Msg_serviceDesc)
}
