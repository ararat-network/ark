package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// CommitteeMsgs is the module's committee surface: the messages its
// emergency mandate authorises, which the priority lane carries and vouches
// for. Suspension is the committee's only power.
var CommitteeMsgs = []sdk.Msg{&MsgEmergencySuspendAsset{}}
