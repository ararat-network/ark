package mempool

import (
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// Gossip rotates through unsent and due transactions, without removing them.
// A periodic rebroadcast repairs temporary remote refusals and peer changes.
func (p *Pool) Gossip(maxBytes, maxGas uint64, now time.Time) [][]byte {
	// A batch must fit a maximum-sized transaction plus its protobuf field
	// tag and length. Compute in uint64 to avoid overflowing an int-sized limit.
	txBytes := uint64(p.config.MaxTxBytes)
	batchBytes := txBytes + 1 + uint64(protowire.SizeVarint(txBytes))
	if maxBytes == 0 || maxBytes > batchBytes {
		maxBytes = batchBytes
	}
	entries := p.Snapshot()
	var due []Entry
	for _, e := range entries {
		if e.LastGossip.IsZero() || now.Sub(e.LastGossip) >= 5*time.Second {
			due = append(due, e)
		}
	}
	return selectEntries(due, maxBytes, maxGas, [3]serviceShares{
		LaneCommittee:  {bytes: CommitteeGossipByteShare, gas: CommitteeGossipGasShare},
		LaneGovernance: {bytes: GovernanceGossipByteShare, gas: GovernanceGossipGasShare},
	}, gossipLess, func(e Entry, _ int8) bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		current, ok := p.entries[e.Key]
		if !ok {
			return false
		}
		current.LastGossip = now
		p.entries[e.Key] = current
		return true
	})
}

// Oldest broadcasts go first within each service class so high fees cannot
// monopolise rebroadcast batches. Equal broadcast times retain arrival order.
func gossipLess(x, y *Entry) bool {
	if !x.LastGossip.Equal(y.LastGossip) {
		return x.LastGossip.Before(y.LastGossip)
	}
	return arrivalLess(x, y)
}
