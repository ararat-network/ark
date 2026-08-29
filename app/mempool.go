package app

import (
	"github.com/spf13/cast"

	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"

	"github.com/ararat-network/ark/abci/lanes"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	securitytypes "github.com/ararat-network/ark/x/security/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// priorityLaneSet is wiring-owned and holds exactly the messages this chain
// proposes ahead of other traffic: governance participation, and each module's
// committee surface. Authority-gated messages are absent by construction —
// they execute in x/gov's EndBlocker and never transit the mempool. The oracle
// runtime's proposal handler wraps the SDK default handler over app.Mempool(),
// so this lane drains first when this node proposes.
//
// The set is compiled in and never configured. It decides proposer behaviour,
// so a node-local override would let honest-looking proposers diverge
// silently, and an on-chain parameter would only ever move in lockstep with
// the message surface itself — which is already a binary upgrade.
// TestPriorityMsgURLsCoverCommitteeSurface pins this list to the registry in
// both directions, so a rename cannot strand an entry and a new committee
// message cannot ship without a lane decision.
func priorityLaneSet() lanes.Set {
	return lanes.NewSet(
		// Governance participation. Passing proposals execute their own
		// messages later, outside the mempool.
		&govv1.MsgSubmitProposal{},
		&govv1.MsgDeposit{},
		&govv1.MsgVote{},
		&govv1.MsgVoteWeighted{},
		&govv1.MsgCancelProposal{},
		&govv1beta1.MsgSubmitProposal{},
		&govv1beta1.MsgDeposit{},
		&govv1beta1.MsgVote{},
		&govv1beta1.MsgVoteWeighted{},

		// x/asset: suspension is the emergency committee's only power.
		&assettypes.MsgEmergencySuspendAsset{},

		// x/claims
		&claimstypes.MsgCommitteeSubmitClaim{},
		&claimstypes.MsgCommitteeCancelClaim{},

		// x/market
		&markettypes.MsgCommitteeUpdatePolicy{},
		&markettypes.MsgCommitteeSetTobinTax{},

		// x/reserve
		&reservetypes.MsgCommitteeDeploy{},
		&reservetypes.MsgCommitteeRecordUpdate{},
		&reservetypes.MsgCommitteeAttributeReturn{},
		&reservetypes.MsgCommitteeReverseReturn{},
		&reservetypes.MsgCommitteeMarkImpaired{},
		&reservetypes.MsgCommitteeClearImpairment{},
		&reservetypes.MsgCommitteeCorrectPosition{},
		&reservetypes.MsgCommitteeClosePosition{},
		&reservetypes.MsgCommitteeBurnPaper{},
		&reservetypes.MsgCommitteeBurnSurplus{},
		&reservetypes.MsgCommitteeFundBuffer{},
		&reservetypes.MsgCommitteeFundInsurance{},

		// x/security
		&securitytypes.MsgCommitteePlanUpgrade{},
		&securitytypes.MsgCommitteeCancelUpgrade{},
		&securitytypes.MsgCommitteeRecoverClient{},

		// x/treasury
		&treasurytypes.MsgCommitteeUpdatePolicy{},
	)
}

// mempoolMaxTxs reads the app.toml mempool cap, so operators can hold the pool
// at or above CometBFT's mempool.size. An absent key means the app was built
// outside the server command — tests and simulations supply no mempool
// settings — and still gets the lanes; only an explicit negative value
// disables the pool.
func mempoolMaxTxs(appOpts servertypes.AppOptions) int {
	value := appOpts.Get(server.FlagMempoolMaxTxs)
	if value == nil {
		return lanes.DefaultMaxTx
	}

	return cast.ToInt(value)
}
