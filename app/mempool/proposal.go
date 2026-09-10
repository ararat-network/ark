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

// PrepareProposalHandler assigns proposal-local lanes and delegates selection to the SDK after
// oracle-byte reservation. Saturated lanes and nonce successors wait; only transactions larger than
// a full lane allowance use ordinary priority. See README.md for index and selector accounting.
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
