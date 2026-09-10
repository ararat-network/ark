package app

import (
	"fmt"
	"strconv"

	abci "github.com/cometbft/cometbft/abci/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
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
	return app.privileges
}

// Pool is the pending pool behind CometBFT's list.
func (app *ArkApp) Pool() *mempool.Pool {
	return app.lanePool
}

// CheckTx observes the SDK response without changing validation or recheck.
func (app *ArkApp) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	res, err := app.App.CheckTx(req)
	status := "invalid"
	if err == nil && res != nil && res.Code == 0 {
		status = "accepted"
	} else if res != nil && res.Codespace == mempool.ErrCapacity.Codespace() && res.Code == mempool.ErrCapacity.ABCICode() {
		status = "full"
	}
	abcimetrics.RecordAdmission(status)
	return res, err
}

// FinalizeBlock records the block's transactions for committed storage release;
// removing them here is unsafe under optimistic execution.
func (app *ArkApp) FinalizeBlock(req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	res, err := app.App.FinalizeBlock(req)
	if err == nil {
		app.lanePool.Finalised(req.Txs)
	}
	return res, err
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

// mempoolConfig reads the existing app.toml count and config.toml byte keys.
// Startup supplies CometBFT's resolved byte values; embedded callers that omit
// them get the same defaults. Validate again here for non-CLI callers.
func mempoolConfig(appOpts servertypes.AppOptions) (mempool.Config, error) {
	cfg := mempool.DefaultConfig()
	// Cast truncates floats. Parse integer text instead, including strings
	// supplied by environment variables, and reject overflow before narrowing.
	read := func(key string, fallback int64, bits int) (int64, error) {
		value := appOpts.Get(key)
		if value == nil {
			return fallback, nil
		}
		n, err := strconv.ParseInt(fmt.Sprint(value), 10, bits)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer: %w", key, err)
		}
		return n, nil
	}
	count, err := read(mempool.MaxTxsKey, int64(cfg.MaxTxs), strconv.IntSize)
	if err != nil {
		return cfg, err
	}
	cfg.MaxTxs = int(count)
	cfg.MaxTxsBytes, err = read(mempool.MaxPoolBytesKey, cfg.MaxTxsBytes, 64)
	if err != nil {
		return cfg, err
	}
	txBytes, err := read(mempool.MaxTransactionBytesKey, int64(cfg.MaxTxBytes), strconv.IntSize)
	if err != nil {
		return cfg, err
	}
	cfg.MaxTxBytes = int(txBytes)
	return cfg, cfg.Validate()
}
