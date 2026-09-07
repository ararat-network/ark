package app

import (
	"fmt"
	"strconv"

	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"github.com/ararat-network/ark/app/ante"
	"github.com/ararat-network/ark/app/mempool"
	"github.com/ararat-network/ark/pkg/mandate"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	securitytypes "github.com/ararat-network/ark/x/security/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// Privileges is the compiled priority message set used by authenticated
// ante classification. Authority-only
// governance execution never transits the mempool. Registry coverage tests
// require an explicit lane decision for every committee message.
func (app *ArkApp) Privileges() mempool.Set {
	return app.mempoolHandler.Privileges()
}

func (app *ArkApp) newPrivileges() mempool.Set {
	return mempool.NewSet(
		ante.GovernancePrivilege(app.StakingKeeper, app.BankKeeper, app.GovKeeper),
		mempool.Privilege{Lane: mempool.LaneCommittee, Msgs: assettypes.CommitteeMsgs, Vouch: mandate.Vouch(app.AssetKeeper.AuthoriseCommittee)},
		mempool.Privilege{Lane: mempool.LaneCommittee, Msgs: claimstypes.CommitteeMsgs, Vouch: mandate.Vouch(app.ClaimsKeeper.AuthoriseCommittee)},
		mempool.Privilege{Lane: mempool.LaneCommittee, Msgs: markettypes.CommitteeMsgs, Vouch: mandate.Vouch(app.MarketKeeper.AuthoriseCommittee)},
		mempool.Privilege{Lane: mempool.LaneCommittee, Msgs: reservetypes.CommitteeMsgs, Vouch: mandate.Vouch(app.ReserveKeeper.AuthoriseCommittee)},
		mempool.Privilege{Lane: mempool.LaneCommittee, Msgs: securitytypes.CommitteeMsgs, Vouch: mandate.Vouch(app.SecurityKeeper.AuthoriseCommittee)},
		mempool.Privilege{Lane: mempool.LaneCommittee, Msgs: treasurytypes.CommitteeMsgs, Vouch: mandate.Vouch(app.TreasuryKeeper.AuthoriseCommittee)},
	)
}

// mempoolMaxTxs reads the bounded count; zero selects the shipped default.
// Disabling the pool is unsupported: the SDK's NoOp path also disables strict
// proposal verification. Read this again at app construction for non-CLI users.
func mempoolMaxTxs(appOpts servertypes.AppOptions) (int, error) {
	value := appOpts.Get(server.FlagMempoolMaxTxs)
	if value == nil {
		return mempool.DefaultMaxTx, nil
	}
	// Cast rejects malformed strings but truncates floats. Accept only values
	// whose text is an integer; app.toml and flags both use integer notation.
	maxTxs, err := strconv.Atoi(fmt.Sprint(value))
	if err != nil {
		return 0, fmt.Errorf("[mempool] max-txs must be an integer: %w", err)
	}
	if err := mempool.ValidateMaxTx(maxTxs); err != nil {
		return 0, err
	}
	return maxTxs, nil
}
