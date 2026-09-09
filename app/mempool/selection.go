package mempool

import (
	"container/heap"
	"fmt"

	cmttypes "github.com/cometbft/cometbft/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	legacygov "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
)

// serviceShares supplies independent resource shares for one scheduling use.
type serviceShares struct {
	bytes uint64
	gas   uint64
}

// SelectEntries gives each privileged class bounded service, then lets all
// remaining transactions compete by fee. verify is called only after resource
// and predecessor checks, with the service lane being used. It must commit state
// only for accepted transactions. A refused privileged candidate can be retried
// in the normal phase, where loss of eligibility does not imply invalidity.
func SelectEntries(entries []Entry, maxBytes, maxGas uint64, verify func(Entry, int8) bool) [][]byte {
	return selectEntries(entries, maxBytes, maxGas, [3]serviceShares{
		LaneCommittee:  {bytes: CommitteeBlockByteShare, gas: CommitteeBlockGasShare},
		LaneGovernance: {bytes: GovernanceBlockByteShare, gas: GovernanceBlockGasShare},
	}, proposalLess, verify)
}

func selectEntries(entries []Entry, maxBytes, maxGas uint64, shares [3]serviceShares, less func(*Entry, *Entry) bool, verify func(Entry, int8) bool) [][]byte {
	var usedBytes, usedGas uint64
	selected := make(map[[32]byte]bool)
	protectedVotes := make(map[string]bool)
	schedule := newNonceSchedule(entries)
	var out [][]byte
	for _, lane := range []int8{LaneCommittee, LaneGovernance, LaneNormal} {
		// Set this phase's total and per-sender resource budgets.
		limitBytes, limitGas := maxBytes, maxGas
		if lane != LaneNormal {
			limitBytes = fraction(maxBytes, shares[lane].bytes)
			if maxGas > 0 {
				limitGas = fraction(maxGas, shares[lane].gas)
			}
		}
		var laneBytes, laneGas uint64
		senderBytes := make(map[string]uint64)
		senderGas := make(map[string]uint64)

		// Seed the queue with eligible transactions whose predecessors are selected.
		queue := &readyHeap{entries: entries, less: less}
		queued := make([]bool, len(entries))
		enqueue := func(i int) {
			e := entries[i]
			if !queued[i] && !selected[e.Key] && (lane == LaneNormal || e.Lane == lane) && schedule.ready(e) {
				queued[i] = true
				heap.Push(queue, i)
			}
		}
		for i := range entries {
			enqueue(i)
		}

		// Check preference and resource limits before running caller validation.
		for queue.Len() > 0 {
			e := entries[heap.Pop(queue).(int)]
			if lane == LaneGovernance {
				seen := make(map[string]bool)
				repeat := false
				for _, key := range voteKeys(e) {
					if protectedVotes[key] || seen[key] {
						repeat = true
					}
					seen[key] = true
				}
				if repeat {
					continue
				}
			}
			bytes := uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{e.Bytes}))
			gasTx, ok := e.Tx.(sdk.FeeTx)
			if !ok {
				continue
			}
			gas := gasTx.GetGas()
			if bytes > maxBytes-usedBytes || bytes > limitBytes-laneBytes {
				continue
			}
			if maxGas > 0 && (gas > maxGas-usedGas || gas > limitGas-laneGas) {
				continue
			}
			sender := string(e.Key[:])
			if len(e.Signers) > 0 {
				sender = string(e.Signers[0].Signer)
			}
			// Each sender receives at most a quarter of preferential
			// service. Larger actions and additional messages still
			// compete in the ordinary phase.
			if lane != LaneNormal && (bytes > limitBytes/4-senderBytes[sender] || (maxGas > 0 && gas > limitGas/4-senderGas[sender])) {
				continue
			}
			if !verify(e, lane) {
				continue
			}

			// Charge accepted transactions and release their nonce successors.
			selected[e.Key] = true
			if lane == LaneGovernance {
				for _, key := range voteKeys(e) {
					protectedVotes[key] = true
				}
			}
			usedBytes += bytes
			usedGas += gas
			laneBytes += bytes
			laneGas += gas
			senderBytes[sender] += bytes
			senderGas[sender] += gas
			out = append(out, e.Bytes)
			for _, i := range schedule.release(e) {
				enqueue(i)
			}
		}
	}
	return out
}

// fraction cannot overflow even when the caller supplies MaxUint64.
func fraction(n, bps uint64) uint64 {
	return n/10000*bps + n%10000*bps/10000
}

func voteKeys(e Entry) []string {
	var out []string
	for _, msg := range e.Tx.GetMsgs() {
		switch m := msg.(type) {
		case *gov.MsgVote:
			out = append(out, fmt.Sprintf("%d/%s", m.ProposalId, m.Voter))
		case *gov.MsgVoteWeighted:
			out = append(out, fmt.Sprintf("%d/%s", m.ProposalId, m.Voter))
		case *legacygov.MsgVote:
			out = append(out, fmt.Sprintf("%d/%s", m.ProposalId, m.Voter))
		case *legacygov.MsgVoteWeighted:
			out = append(out, fmt.Sprintf("%d/%s", m.ProposalId, m.Voter))
		}
	}
	return out
}

func proposalLess(x, y *Entry) bool {
	if x.Fee != y.Fee {
		return x.Fee > y.Fee
	}
	return arrivalLess(x, y)
}
