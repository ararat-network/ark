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
	legacy.RegisterAminoMsg(cdc, &MsgRegisterMembers{}, "ark/disbursement/MsgRegisterMembers")
	legacy.RegisterAminoMsg(cdc, &MsgRelease{}, "ark/disbursement/MsgRelease")
	legacy.RegisterAminoMsg(cdc, &MsgCancelGrants{}, "ark/disbursement/MsgCancelGrants")
	legacy.RegisterAminoMsg(cdc, &MsgSuspendMembers{}, "ark/disbursement/MsgSuspendMembers")
	legacy.RegisterAminoMsg(cdc, &MsgReinstateMembers{}, "ark/disbursement/MsgReinstateMembers")
	legacy.RegisterAminoMsg(cdc, &MsgVoidSuspensions{}, "ark/disbursement/MsgVoidSuspensions")
	legacy.RegisterAminoMsg(cdc, &MsgSetPayee{}, "ark/disbursement/MsgSetPayee")
	legacy.RegisterAminoMsg(cdc, &MsgSetController{}, "ark/disbursement/MsgSetController")
	legacy.RegisterAminoMsg(cdc, &MsgReturnUnallocated{}, "ark/disbursement/MsgReturnUnallocated")
}

// RegisterInterfaces registers disbursement transaction messages.
func RegisterInterfaces(r codectypes.InterfaceRegistry) {
	r.RegisterImplementations((*sdk.Msg)(nil), &MsgUpdateParams{}, &MsgCreateGrant{}, &MsgRegisterMembers{}, &MsgRelease{}, &MsgCancelGrants{}, &MsgSuspendMembers{}, &MsgReinstateMembers{}, &MsgVoidSuspensions{}, &MsgSetPayee{}, &MsgSetController{}, &MsgReturnUnallocated{})
	msgservice.RegisterMsgServiceDesc(r, &_Msg_serviceDesc)
}
