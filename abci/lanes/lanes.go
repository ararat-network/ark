// Package lanes orders the app-side mempool into a priority lane and a normal
// lane, so that a privileged class of transaction is proposed, and therefore
// executed, ahead of all other traffic.
//
// Which messages are privileged is not decided here: the Set is supplied by
// the caller, because that choice is wiring policy over the app's own module
// surface, while this package is protocol mechanism. Ordering is
// proposer-local and deliberately unenforced in ProcessProposal;
// docs/DESIGN_NOTES.md §8 records that decision
// and the deferred alternatives.
package lanes

import (
	"context"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
)

// Lane values order the pool: the higher lane drains first, ante fee priority
// orders within a lane.
const (
	LaneNormal   int8 = 0
	LanePriority int8 = 1
)

// DefaultMaxTx is the shipped pool bound, mirroring CometBFT's own
// mempool.size default. Operators size the pool through app.toml's
// [mempool] max-txs; keep it at or above CometBFT's mempool.size so comet's
// full-pool gate binds first, since a smaller app pool fails CheckTx for
// transactions comet already admitted. At the cap Insert rejects outright —
// no eviction, no lane preference — so lanes order the pool but never win
// admission to it.
const DefaultMaxTx = 5000

// Priority is the mempool ordering key.
type Priority struct {
	Lane int8
	// Fee is the ante-assigned fee priority from the CheckTx context.
	Fee int64
}

// TxPriority orders by lane, then by ante fee priority; the mempool breaks
// remaining ties by arrival.
func TxPriority(set Set) mempool.TxPriority[Priority] {
	return mempool.TxPriority[Priority]{
		GetTxPriority: func(goCtx context.Context, tx sdk.Tx) Priority {
			return Priority{
				Lane: set.Classify(tx),
				Fee:  sdk.UnwrapSDKContext(goCtx).Priority(),
			}
		},
		Compare:  Compare,
		MinValue: Priority{Lane: LaneNormal, Fee: math.MinInt64},
	}
}

// Compare orders two priority keys, lane before fee.
func Compare(a, b Priority) int {
	if a.Lane != b.Lane {
		if a.Lane < b.Lane {
			return -1
		}
		return 1
	}
	switch {
	case a.Fee < b.Fee:
		return -1
	case a.Fee > b.Fee:
		return 1
	default:
		return 0
	}
}

// NewMempool returns the lane-ordered mempool wired into BaseApp. The default
// SignerExtractionAdapter preserves per-sender nonce order within and across
// lanes, so a sender's earlier normal transaction can legitimately precede
// their priority transaction in a proposal.
func NewMempool(maxTx int, set Set) *mempool.PriorityNonceMempool[Priority] {
	return mempool.NewPriorityMempool(mempool.PriorityNonceMempoolConfig[Priority]{
		TxPriority: TxPriority(set),
		MaxTx:      maxTx,
	})
}
