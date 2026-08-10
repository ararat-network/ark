package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers all Reserve messages used by governance
// transactions.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgSetReserveMandate{}, "ark/reserve/MsgSetReserveMandate")
	legacy.RegisterAminoMsg(cdc, &MsgSetRecognitionPolicy{}, "ark/reserve/MsgSetRecognitionPolicy")
	legacy.RegisterAminoMsg(cdc, &MsgFundBuffer{}, "ark/reserve/MsgFundBuffer")
	legacy.RegisterAminoMsg(cdc, &MsgFundInsurance{}, "ark/reserve/MsgFundInsurance")
	legacy.RegisterAminoMsg(cdc, &MsgCorrectPosition{}, "ark/reserve/MsgCorrectPosition")
	legacy.RegisterAminoMsg(cdc, &MsgClearImpairment{}, "ark/reserve/MsgClearImpairment")
	legacy.RegisterAminoMsg(cdc, &MsgMarkImpaired{}, "ark/reserve/MsgMarkImpaired")
	legacy.RegisterAminoMsg(cdc, &MsgClosePosition{}, "ark/reserve/MsgClosePosition")
	legacy.RegisterAminoMsg(cdc, &MsgReverseReturn{}, "ark/reserve/MsgReverseReturn")
	legacy.RegisterAminoMsg(cdc, &MsgBurnReserveAssets{}, "ark/reserve/MsgBurnReserveAssets")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeDeploy{}, "ark/reserve/MsgCommitteeDeploy")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeRecordUpdate{}, "ark/reserve/MsgCommitteeRecordUpdate")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeAttributeReturn{}, "ark/reserve/MsgCommitteeAttribute")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeMarkImpaired{}, "ark/reserve/MsgCommitteeMarkImpaired")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeClearImpairment{}, "ark/reserve/MsgCommitteeClear")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeCorrectPosition{}, "ark/reserve/MsgCommitteeCorrect")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeReverseReturn{}, "ark/reserve/MsgCommitteeReverseReturn")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeClosePosition{}, "ark/reserve/MsgCommitteeClosePosition")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeBurnPaper{}, "ark/reserve/MsgCommitteeBurnPaper")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeBurnSurplus{}, "ark/reserve/MsgCommitteeBurnSurplus")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeFundBuffer{}, "ark/reserve/MsgCommitteeFundBuffer")
	legacy.RegisterAminoMsg(cdc, &MsgCommitteeFundInsurance{}, "ark/reserve/MsgCommitteeFundInsurance")
}

// RegisterInterfaces registers Reserve messages as sdk.Msg implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSetReserveMandate{},
		&MsgSetRecognitionPolicy{},
		&MsgFundBuffer{},
		&MsgFundInsurance{},
		&MsgCorrectPosition{},
		&MsgClearImpairment{},
		&MsgMarkImpaired{},
		&MsgClosePosition{},
		&MsgReverseReturn{},
		&MsgBurnReserveAssets{},
		&MsgCommitteeDeploy{},
		&MsgCommitteeRecordUpdate{},
		&MsgCommitteeAttributeReturn{},
		&MsgCommitteeMarkImpaired{},
		&MsgCommitteeClearImpairment{},
		&MsgCommitteeCorrectPosition{},
		&MsgCommitteeReverseReturn{},
		&MsgCommitteeClosePosition{},
		&MsgCommitteeBurnPaper{},
		&MsgCommitteeBurnSurplus{},
		&MsgCommitteeFundBuffer{},
		&MsgCommitteeFundInsurance{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
