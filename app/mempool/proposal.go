package mempool

import (
	abci "github.com/cometbft/cometbft/abci/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PrepareProposalHandler runs inside the oracle wrapper, whose MaxTxBytes has
// already subtracted the injected extended commit. Skipped candidates never
// change proposal state. It runs under the lifecycle PrepareProposal lock;
// it must not acquire that lock again. Allocation and fee order remain local
// proposer policy.
func (h *Handler) PrepareProposalHandler(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	if req.MaxTxBytes <= 0 {
		return &abci.ResponsePrepareProposal{}, nil
	}
	var gas uint64
	if block := ctx.ConsensusParams().Block; block != nil && block.MaxGas > 0 {
		gas = uint64(block.MaxGas)
	}
	// RunTx applies BaseApp's signature verification setting per transaction;
	// there is no getter, and the check context carries it.
	ctx = ctx.WithIsSigverifyTx(h.app.GetContextForCheckTx(nil).IsSigverifyTx())
	entries := h.pool.Snapshot()
	// Refresh only bounded, read-only vouches for already authenticated entries.
	// Running complete ante for every queued transaction here would verify
	// signatures twice and turn normal backlog into proposal CPU starvation.
	set := h.privileges
	for i, e := range entries {
		entries[i].Lane = proposalLane(ctx, set, e)
	}
	txs := SelectEntries(entries, uint64(req.MaxTxBytes), gas, func(e Entry, serviceLane int8) bool {
		checked, write, err := h.validatePending(ctx, e.Tx, e.Bytes)
		if err != nil || (serviceLane != LaneNormal && FromContext(checked) != serviceLane) {
			return false
		}
		write()
		return true
	})
	return &abci.ResponsePrepareProposal{Txs: txs}, nil
}

// Eligibility failures (including exhausting the read budget) lose preference;
// the actual transaction validation still decides validity on selection.
func proposalLane(ctx sdk.Context, set Set, e Entry) (lane int8) {
	defer func() {
		if recover() != nil {
			lane = LaneNormal
		}
	}()
	feeTx, ok := e.Tx.(sdk.FeeTx)
	if !ok {
		return LaneNormal
	}
	cached, _ := ctx.WithGasMeter(storetypes.NewGasMeter(feeTx.GetGas())).CacheContext()
	lane, err := set.Classify(cached, e.Tx)
	if err != nil {
		return LaneNormal
	}
	return lane
}
