package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// CommitteeMsgs is the module's committee surface: the messages its
// conversion mandate authorises, which the priority lane carries and vouches
// for.
var CommitteeMsgs = []sdk.Msg{
	&MsgCommitteeUpdatePolicy{},
	&MsgCommitteeSetTobinTax{},
}
