package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// CommitteeMsgs is the module's committee surface: the messages its mandate
// authorises, which the priority lane carries and vouches for.
var CommitteeMsgs = []sdk.Msg{
	&MsgCommitteeDeploy{},
	&MsgCommitteeRecordUpdate{},
	&MsgCommitteeAttributeReturn{},
	&MsgCommitteeReverseReturn{},
	&MsgCommitteeMarkImpaired{},
	&MsgCommitteeClearImpairment{},
	&MsgCommitteeCorrectPosition{},
	&MsgCommitteeClosePosition{},
	&MsgCommitteeBurnPaper{},
	&MsgCommitteeBurnSurplus{},
	&MsgCommitteeFundBuffer{},
	&MsgCommitteeFundInsurance{},
}
