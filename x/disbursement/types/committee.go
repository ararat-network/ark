package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// CommitteeMsgs is the committee surface: the messages its mandate authorises, which the priority
// lane carries and vouches for. The registrar's MsgRegisterMembers is not among them: registration is
// routine traffic on the normal lane, and only the appointment is a committee act.
var CommitteeMsgs = []sdk.Msg{
	&MsgCommitteeRegistrar{},
	&MsgCommitteeSuspend{},
	&MsgCommitteeReinstate{},
	&MsgCommitteeCompensate{},
}
