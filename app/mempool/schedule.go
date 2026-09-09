package mempool

import (
	"bytes"
	"sort"
)

func arrivalLess(x, y *Entry) bool {
	if x.Added != y.Added {
		return x.Added < y.Added
	}
	// Deterministic fallback for snapshots without unique arrival IDs.
	return bytes.Compare(x.Key[:], y.Key[:]) < 0
}

// readyHeap ranks eligible transactions using the caller's ordering policy.
// Nonce successors enter only when all predecessors have been selected, avoiding
// quadratic rescans of a long sender chain whose fees increase with sequence.
type readyHeap struct {
	indices []int
	entries []Entry
	less    func(*Entry, *Entry) bool
}

func (h readyHeap) Len() int {
	return len(h.indices)
}

func (h readyHeap) Less(i, j int) bool {
	a, b := h.indices[i], h.indices[j]
	return h.less(&h.entries[a], &h.entries[b])
}

func (h readyHeap) Swap(i, j int) {
	h.indices[i], h.indices[j] = h.indices[j], h.indices[i]
}

func (h *readyHeap) Push(x any) {
	h.indices = append(h.indices, x.(int))
}

func (h *readyHeap) Pop() any {
	old := h.indices
	x := old[len(old)-1]
	h.indices = old[:len(old)-1]
	return x
}

type nonceGroup struct {
	sequence uint64
	pending  int
	entries  []int
}
type nonceQueue struct {
	groups []nonceGroup
	head   int
}
type nonceSchedule map[string]*nonceQueue

func newNonceSchedule(entries []Entry) nonceSchedule {
	groups := make(map[string]map[uint64][]int)
	for i, e := range entries {
		if e.Unordered {
			continue
		}
		for _, signer := range e.Signers {
			key := string(signer.Signer)
			if groups[key] == nil {
				groups[key] = make(map[uint64][]int)
			}
			groups[key][signer.Sequence] = append(groups[key][signer.Sequence], i)
		}
	}
	schedule := make(nonceSchedule, len(groups))
	for key, sequences := range groups {
		q := &nonceQueue{}
		for seq, indices := range sequences {
			q.groups = append(q.groups, nonceGroup{sequence: seq, pending: len(indices), entries: indices})
		}
		sort.Slice(q.groups, func(i, j int) bool { return q.groups[i].sequence < q.groups[j].sequence })
		schedule[key] = q
	}
	return schedule
}

func (s nonceSchedule) ready(e Entry) bool {
	if e.Unordered {
		return true
	}
	for _, signer := range e.Signers {
		q := s[string(signer.Signer)]
		if q.head < len(q.groups) && q.groups[q.head].sequence < signer.Sequence {
			return false
		}
	}
	return true
}

func (s nonceSchedule) release(e Entry) []int {
	if e.Unordered {
		return nil
	}
	var next []int
	for _, signer := range e.Signers {
		q := s[string(signer.Signer)]
		q.groups[q.head].pending--
		if q.groups[q.head].pending == 0 {
			q.head++
			if q.head < len(q.groups) {
				next = append(next, q.groups[q.head].entries...)
			}
		}
	}
	return next
}
