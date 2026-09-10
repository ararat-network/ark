package mempool

import (
	"context"
	"math"
	"slices"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type resources struct{ bytes, gas uint64 }

func blockAllowance(lane int8, maxBytes, maxGas uint64) resources {
	switch lane {
	case LaneCommittee:
		return resources{fraction(maxBytes, CommitteeBlockByteShare), fraction(maxGas, CommitteeBlockGasShare)}
	case LaneGovernance:
		return resources{fraction(maxBytes, GovernanceBlockByteShare), fraction(maxGas, GovernanceBlockGasShare)}
	default:
		return resources{maxBytes, maxGas}
	}
}

// txSelector only accounts for selected resources. The SDK proposal handler owns
// verification, encoding, all-signer sequences and unordered transaction handling.
type txSelector struct {
	lanes    map[senderNonce]int8
	used     resources
	laneUsed [3]resources
	selected [][]byte
}

var _ baseapp.TxSelector = (*txSelector)(nil)

func (s *txSelector) SelectedTxs(context.Context) [][]byte { return slices.Clone(s.selected) }
func (s *txSelector) Clear() {
	s.used = resources{}
	s.laneUsed = [3]resources{}
	s.selected = nil
}

func (s *txSelector) SelectTxForProposal(_ context.Context, maxBytes, maxGas uint64, tx sdk.Tx, bz []byte) bool {
	// The SDK casts the consensus -1 (unlimited) gas setting to uint64.
	if maxGas > math.MaxInt64 {
		maxGas = 0
	}
	key, err := identity(tx)
	if err != nil {
		return false
	} // The SDK verifier rejects malformed signers.
	lane := s.lanes[key]
	limit := blockAllowance(lane, maxBytes, maxGas)
	size := uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{bz}))
	gas := tx.(sdk.FeeTx).GetGas()
	used := s.laneUsed[lane]
	if size <= maxBytes-s.used.bytes && size <= limit.bytes-used.bytes &&
		(maxGas == 0 || (gas <= maxGas-s.used.gas && gas <= limit.gas-used.gas)) {
		s.used.bytes += size
		s.laneUsed[lane].bytes += size
		if maxGas > 0 {
			s.used.gas += gas
			s.laneUsed[lane].gas += gas
		}
		s.selected = append(s.selected, bz)
	}
	// A full privileged lane only skips candidates; it must not stop other lanes.
	return s.used.bytes == maxBytes || (maxGas > 0 && s.used.gas == maxGas)
}
