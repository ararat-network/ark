package mempool

import (
	cmttypes "github.com/cometbft/cometbft/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
)

type serviceShares struct{ bytes, gas uint64 }

// SelectEntries allocates bounded preferential service over an SDK-ordered
// snapshot, then offers all remaining entries ordinary service. verify applies
// SDK validation, including every signer's sequence, before accepting an entry.
func SelectEntries(entries []Entry, maxBytes, maxGas uint64, encode sdk.TxEncoder, verify func(Entry, int8) bool) [][]byte {
	return selectEntries(entries, maxBytes, maxGas, [3]serviceShares{
		LaneCommittee:  {bytes: CommitteeBlockByteShare, gas: CommitteeBlockGasShare},
		LaneGovernance: {bytes: GovernanceBlockByteShare, gas: GovernanceBlockGasShare},
	}, encode, verify)
}

func selectEntries(entries []Entry, maxBytes, maxGas uint64, shares [3]serviceShares, encode sdk.TxEncoder, verify func(Entry, int8) bool) [][]byte {
	var usedBytes, usedGas uint64
	selected := make(map[[32]byte]bool)
	failed := make(map[[32]byte]bool)
	var out [][]byte
	encoded := make([][]byte, len(entries))
	selectedSequences := make(map[string]uint64)
	for _, lane := range []int8{LaneCommittee, LaneGovernance, LaneNormal} {
		limitBytes, limitGas := maxBytes, maxGas
		if lane != LaneNormal {
			limitBytes = fraction(maxBytes, shares[lane].bytes)
			limitGas = fraction(maxGas, shares[lane].gas)
		}
		var laneBytes, laneGas uint64
		blocked := make(map[string]bool)
		for i, e := range entries {
			if selected[e.Key] {
				continue
			}
			signers, err := sdkmempool.NewDefaultSignerExtractionAdapter().GetSigners(e.Tx)
			if err != nil || len(signers) == 0 {
				continue
			} // Indexed entries have valid signer metadata.
			unordered := false
			if tx, ok := e.Tx.(sdk.TxWithUnordered); ok {
				unordered = tx.GetUnordered()
			}
			// A phase must not pull an ordered successor past a predecessor
			// it skipped. The SDK snapshot already orders each primary sender.
			skip := func() {
				if !unordered && lane != LaneNormal {
					blocked[string(signers[0].Signer)] = true
				}
			}
			if failed[e.Key] || (!unordered && blocked[string(signers[0].Signer)]) || (lane != LaneNormal && e.Lane != lane) {
				skip()
				continue
			}
			// Match SDK proposal handling for every signer already selected.
			// A verified but unselected ordinary transaction also establishes its
			// expected sequence below, as in the SDK default proposal handler.
			contiguous := true
			if !unordered {
				for _, signer := range signers {
					if seq, ok := selectedSequences[string(signer.Signer)]; ok && seq+1 != signer.Sequence {
						contiguous = false
						break
					}
				}
			}
			if !contiguous {
				skip()
				continue
			}
			// Encode only candidates reached by selection, once across all phases.
			// The SDK encoding defines proposal bytes; admission retains wire bytes.
			if encoded[i] == nil {
				encoded[i], err = encode(e.Tx)
				if err != nil {
					failed[e.Key] = true
					skip()
					continue
				}
			}
			size := uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{encoded[i]}))
			gas := e.Tx.(sdk.FeeTx).GetGas()
			fits := size <= maxBytes-usedBytes && size <= limitBytes-laneBytes &&
				(maxGas == 0 || (gas <= maxGas-usedGas && gas <= limitGas-laneGas))
			// Preferential phases must leave skipped candidates untouched for a
			// later phase. Ordinary service retains SDK verification-before-filtering.
			if lane != LaneNormal && !fits {
				skip()
				continue
			}
			if !verify(e, lane) {
				failed[e.Key] = true
				skip()
				continue
			}
			if !unordered {
				for _, signer := range signers {
					key := string(signer.Signer)
					if fits {
						selectedSequences[key] = signer.Sequence
					} else if _, ok := selectedSequences[key]; !ok {
						selectedSequences[key] = signer.Sequence - 1
					}
				}
			}
			if !fits {
				continue
			}
			selected[e.Key] = true
			usedBytes += size
			usedGas += gas
			laneBytes += size
			laneGas += gas
			out = append(out, encoded[i])
			if usedBytes == maxBytes || (maxGas > 0 && usedGas == maxGas) {
				return out
			}
		}
	}
	return out
}

func fraction(n, bps uint64) uint64 { return n/10000*bps + n%10000*bps/10000 }
