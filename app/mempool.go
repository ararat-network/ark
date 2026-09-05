package app

import (
	"github.com/spf13/cast"

	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"github.com/ararat-network/ark/abci/lanes"
	"github.com/ararat-network/ark/app/ante"
	"github.com/ararat-network/ark/pkg/mandate"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	securitytypes "github.com/ararat-network/ark/x/security/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// Privileges is the priority lane: governance participation and each
// module's committee surface, every message vouched for at CheckTx by the
// first refusal its handler would make. The mempool classifies with it and
// the ante chain vouches with it, both from this one constructor, so the two
// cannot disagree. It reads the keepers, so it is callable only once they
// are injected. Authority-gated messages are absent by construction — they
// execute in x/gov's EndBlocker and never transit the mempool. The oracle
// runtime's proposal handler wraps the SDK default handler over
// app.Mempool(), so this lane drains first when this node proposes.
//
// The set is compiled in and never configured. It decides proposer
// behaviour, so a node-local override would let honest-looking proposers
// diverge silently, and an on-chain parameter would only ever move in
// lockstep with the message surface itself — which is already a binary
// upgrade. TestPriorityMsgURLsCoverCommitteeSurface pins the set to the
// registry in both directions, so a rename cannot strand an entry and a new
// committee message cannot ship without a lane decision.
func (app *ArkApp) Privileges() lanes.Set {
	return lanes.NewSet(
		ante.GovernancePrivilege(app.StakingKeeper, app.BankKeeper, app.GovKeeper),
		lanes.Privilege{Msgs: assettypes.CommitteeMsgs, Vouch: mandate.Vouch(app.AssetKeeper.AuthoriseCommittee)},
		lanes.Privilege{Msgs: claimstypes.CommitteeMsgs, Vouch: mandate.Vouch(app.ClaimsKeeper.AuthoriseCommittee)},
		lanes.Privilege{Msgs: markettypes.CommitteeMsgs, Vouch: mandate.Vouch(app.MarketKeeper.AuthoriseCommittee)},
		lanes.Privilege{Msgs: reservetypes.CommitteeMsgs, Vouch: mandate.Vouch(app.ReserveKeeper.AuthoriseCommittee)},
		lanes.Privilege{Msgs: securitytypes.CommitteeMsgs, Vouch: mandate.Vouch(app.SecurityKeeper.AuthoriseCommittee)},
		lanes.Privilege{Msgs: treasurytypes.CommitteeMsgs, Vouch: mandate.Vouch(app.TreasuryKeeper.AuthoriseCommittee)},
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
