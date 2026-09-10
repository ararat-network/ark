package mempool

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
)

// proposalPool delegates ordering to a disposable SDK index and removal to the
// live pool. Its snapshot lanes need no pool lookup or lock during verification.
type proposalPool struct {
	*Pool
	index *sdkmempool.PriorityNonceMempool[priority]
}

func (p proposalPool) Select(ctx context.Context, txs [][]byte) sdkmempool.Iterator {
	return p.index.Select(ctx, txs)
}

func (p proposalPool) SelectBy(ctx context.Context, txs [][]byte, visit func(sdk.Tx) bool) {
	p.index.SelectBy(ctx, txs, visit)
}

// PrepareProposalHandler assigns proposal-local service lanes, then delegates
// the entire transaction-selection loop to the SDK. The oracle wrapper has
// already subtracted its bytes. Only actions larger than a full lane allowance
// move to ordinary fee competition; exhausting a lane does not permit overflow.
//
// Entries past a lane's allowance in pool order stay out of the index: the SDK
// verifies every candidate it iterates before the selector can decline it, so
// a saturated lane would otherwise cost one signature check per pending entry.
// An ordered sender's later entries wait with the first one left out, as they
// cannot execute before it. The selector remains the authority; an entry that
// fails verification leaves its share unused for this proposal.
func (p *Pool) PrepareProposalHandler(app baseapp.ProposalTxVerifier) sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		if req.MaxTxBytes <= 0 {
			return &abci.ResponsePrepareProposal{}, nil
		}
		var maxGas uint64
		if block := ctx.ConsensusParams().Block; block != nil && block.MaxGas > 0 {
			maxGas = uint64(block.MaxGas)
		}
		index := sdkmempool.NewPriorityMempool(priorityConfig())
		selector := &txSelector{lanes: make(map[senderNonce]int8)}
		var laneUsed [3]resources
		waiting := make(map[string]bool)
		for _, entry := range p.Snapshot() {
			key := entry.key
			unordered := false
			if tx, ok := entry.Tx.(sdk.TxWithUnordered); ok {
				unordered = tx.GetUnordered()
			}
			if !unordered && waiting[key.sender] {
				continue
			}
			lane := entry.Lane
			if lane != LaneNormal {
				allowance := blockAllowance(lane, uint64(req.MaxTxBytes), maxGas)
				// Encoding failures still go through SDK verification/removal handling.
				if bz, err := app.TxEncode(entry.Tx); err == nil {
					need := resources{uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{bz})), entry.Tx.(sdk.FeeTx).GetGas()}
					switch {
					case need.bytes > allowance.bytes || (maxGas > 0 && need.gas > allowance.gas):
						lane = LaneNormal
					case need.bytes > allowance.bytes-laneUsed[lane].bytes || (maxGas > 0 && need.gas > allowance.gas-laneUsed[lane].gas):
						if !unordered {
							waiting[key.sender] = true
						}
						continue
					default:
						laneUsed[lane].bytes += need.bytes
						laneUsed[lane].gas += need.gas
					}
				}
			}
			selector.lanes[key] = lane
			if err := index.Insert(WithLane(ctx.WithPriority(entry.Priority), lane), entry.Tx); err != nil {
				return nil, err
			}
		}
		handler := baseapp.NewDefaultProposalHandler(proposalPool{Pool: p, index: index}, app)
		handler.SetTxSelector(selector)
		return handler.PrepareProposalHandler()(ctx, req)
	}
}
