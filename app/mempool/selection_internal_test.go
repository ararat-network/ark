package mempool

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

func TestSelectionReservationsAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name             string
		maxBytes, maxGas uint64
	}{{"gas limited", 100000, 1000}, {"bytes limited", 1050, 100000}} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(100, encodePoolTx)
			for i := 0; i < 12; i++ {
				lane := LaneNormal
				fee := int64(100)
				tx := fixtureTx(byte(i + 1))
				if i < 2 {
					lane = LaneCommittee
					fee = 0
					tx.gas = 5
					tx.size = 5
				}
				if i >= 2 && i < 4 {
					lane = LaneGovernance
					fee = 0
				}
				require.NoError(t, p.Insert(laneContext(lane).WithPriority(fee), tx))
			}
			selected := SelectEntries(p.Snapshot(), tc.maxBytes, tc.maxGas, func(Entry, int8) bool { return true })
			require.NotEmpty(t, selected)
			require.Equal(t, byte(1), selected[0][0])
			require.Greater(t, len(selected), 4)
			normal := 0
			for _, bz := range selected {
				if bz[0] >= 5 {
					normal++
				}
			}
			require.Greater(t, normal, 0)
		})
	}
	t.Run("oversized privileged transaction can use ordinary capacity", func(t *testing.T) {
		tx := fixtureTx(1)
		raw, _ := encodePoolTx(tx)
		entries := []Entry{{Tx: tx, Bytes: raw, Key: sha256.Sum256(raw), Lane: LaneCommittee}}
		require.Len(t, SelectEntries(entries, 1000, 100, func(Entry, int8) bool { return true }), 1)
	})
	t.Run("normal predecessor cannot be skipped", func(t *testing.T) {
		p := newTestPool(100, encodePoolTx)
		a := fixtureTx(1)
		b := fixtureTx(2)
		b.key = a.key
		b.seq = 1
		require.NoError(t, p.Insert(laneContext(0), a))
		require.NoError(t, p.Insert(laneContext(LaneCommittee), b))
		got := SelectEntries(p.Snapshot(), 10000, 10000, func(Entry, int8) bool { return true })
		require.Len(t, got, 2)
		require.Equal(t, byte(1), got[0][0])
	})
}

func TestGossipRotation(t *testing.T) {
	t.Run("rotates and rebroadcasts without draining", func(t *testing.T) {
		p := newTestPool(100, encodePoolTx)
		for i := 0; i < 3; i++ {
			require.NoError(t, p.Insert(laneContext(0), fixtureTx(byte(i+1))))
		}
		now := time.Now()
		a := p.Gossip(110, 0, now)
		b := p.Gossip(110, 0, now)
		c := p.Gossip(110, 0, now)
		require.Len(t, a, 1)
		require.Len(t, b, 1)
		require.Len(t, c, 1)
		require.NotEqual(t, a, b)
		require.NotEqual(t, b, c)
		require.Empty(t, p.Gossip(110, 0, now))
		require.NotEmpty(t, p.Gossip(110, 0, now.Add(6*time.Second)))
		require.Equal(t, 3, p.CountTx())
	})
}

func TestSelectionFairness(t *testing.T) {
	for _, tc := range []struct {
		name      string
		votes     bool
		protected int
	}{
		{"sender gas share", false, 5}, {"one protected vote per proposal", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(100, encodePoolTx)
			key := fixtureTx(1).key
			for i := 0; i < 6; i++ {
				tx := fixtureTx(byte(i + 1))
				tx.key = key
				tx.seq = uint64(i)
				if tc.votes {
					tx.msgs = []sdk.Msg{&gov.MsgVote{ProposalId: 1, Voter: sdk.AccAddress(key.Address()).String()}}
				}
				require.NoError(t, p.Insert(laneContext(LaneGovernance), tx))
			}
			protected := 0
			got := SelectEntries(p.Snapshot(), 100000, 40000, func(_ Entry, lane int8) bool {
				if lane == LaneGovernance {
					protected++
				}
				return true
			})
			require.Len(t, got, 6)
			require.Equal(t, tc.protected, protected)
		})
	}
	t.Run("lost eligibility retries in normal service", func(t *testing.T) {
		p := newTestPool(100, encodePoolTx)
		require.NoError(t, p.Insert(laneContext(LaneGovernance), fixtureTx(1)))
		var phases []int8
		got := SelectEntries(p.Snapshot(), 100000, 10000, func(_ Entry, lane int8) bool { phases = append(phases, lane); return lane == LaneNormal })
		require.Len(t, got, 1)
		require.Equal(t, []int8{LaneGovernance, LaneNormal}, phases)
	})
	t.Run("reverse fee chain preserves every nonce", func(t *testing.T) {
		key := fixtureTx(1).key
		entries := make([]Entry, 1000)
		for i := range entries {
			tx := &poolTx{key: key, seq: uint64(i), gas: 1}
			bz := make([]byte, 8)
			binary.BigEndian.PutUint64(bz, uint64(i))
			entries[i] = Entry{Tx: tx, Bytes: bz, Key: sha256.Sum256(bz), Fee: int64(i), Signers: []sdkmempool.SignerData{sdkmempool.NewSignerData(sdk.AccAddress(key.Address()), uint64(i))}}
		}
		got := SelectEntries(entries, 100000, 10000, func(Entry, int8) bool { return true })
		require.Len(t, got, len(entries))
		for i, bz := range got {
			require.Equal(t, uint64(i), binary.BigEndian.Uint64(bz))
		}
	})
}

func TestSelectionIndependentResourceShares(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bytes, gas uint64
		protected  int
	}{
		{"five percent gas", 500, 500, 5},
		{"gas changes independently", 500, 1000, 10},
		{"bytes change independently", 50, 1000, 4},
		{"zero bytes disables preferential service", 0, 1000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(400, encodePoolTx)
			for i := 0; i < 12; i++ {
				tx := fixtureTx(byte(i + 1))
				tx.gas = 1000
				require.NoError(t, p.Insert(laneContext(LaneGovernance), tx))
			}
			protected := 0
			got := selectEntries(p.Snapshot(), 100000, 100000, [3]serviceShares{LaneGovernance: {bytes: tc.bytes, gas: tc.gas}}, proposalLess, func(_ Entry, lane int8) bool {
				if lane == LaneGovernance {
					protected++
				}
				return true
			})
			require.Equal(t, tc.protected, protected)
			require.Len(t, got, 12, "overflow remains eligible for ordinary service")
		})
	}
}

// selectionChains assigns arrival IDs across chains in the supplied order.
func selectionChains(chains ...[]int64) []Entry {
	var entries []Entry
	for c, fees := range chains {
		signer := sdk.AccAddress([]byte(fmt.Sprintf("sender-%d", c)))
		for seq, fee := range fees {
			id := uint64(len(entries) + 1)
			bz := make([]byte, 8)
			binary.BigEndian.PutUint64(bz, id)
			entries = append(entries, Entry{
				Tx: &poolTx{gas: 1}, Bytes: bz, Key: sha256.Sum256(bz), Added: id, Fee: fee,
				Signers: []sdkmempool.SignerData{sdkmempool.NewSignerData(signer, uint64(seq))},
			})
		}
	}
	return entries
}

func TestSelectionFeeTies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chains [][]int64
		mutate func([]Entry)
		reject uint64
		budget uint64
		want   []uint64
	}{
		{name: "successor fee does not displace earlier arrival", chains: [][]int64{{5, 20}, {5, 99}}, want: []uint64{1, 2, 3, 4}},
		{name: "equal fee run", chains: [][]int64{{5, 20}, {5, 5, 99}}, want: []uint64{1, 2, 3, 4, 5}},
		{name: "newly ready higher fee precedes tied heads", chains: [][]int64{{5, 30}, {5, 20, 99}}, want: []uint64{1, 2, 3, 4, 5}},
		{name: "lower fee bottleneck", chains: [][]int64{{5}, {5, 1, 99}}, want: []uint64{1, 2, 3, 4}},
		{name: "higher fee precedes earlier arrival", chains: [][]int64{{6}, {5, 99}}, want: []uint64{1, 2, 3}},
		{name: "equal fees preserve arrival", chains: [][]int64{{5, 99}, {5, 99}}, want: []uint64{1, 2, 3, 4}},
		{name: "pending sequences retain relative order", chains: [][]int64{{5}, {5, 99}}, mutate: func(e []Entry) { e[2].Signers[0].Sequence = 2 }, want: []uint64{1, 2, 3}},
		{name: "unordered successor is independently ready", chains: [][]int64{{5}, {5, 99}}, mutate: func(e []Entry) { e[2].Unordered = true }, want: []uint64{3, 1, 2}},
		{name: "unordered head does not block ordered transaction", chains: [][]int64{{5}, {5, 99}}, mutate: func(e []Entry) { e[1].Unordered = true }, want: []uint64{3, 1, 2}},
		{name: "multiple signer successor follows predecessor", chains: [][]int64{{5}, {5, 99}}, mutate: func(e []Entry) { e[2].Signers = append(e[2].Signers, sdkmempool.NewSignerData([]byte("other"), 0)) }, want: []uint64{1, 2, 3}},
		{name: "same sequence group preserves arrival", chains: [][]int64{{5}, {5, 99, 99}}, mutate: func(e []Entry) { e[3].Signers[0].Sequence = 1 }, want: []uint64{1, 2, 3, 4}},
		{name: "rejected predecessor never releases successor", chains: [][]int64{{5, 20}, {5, 99}}, reject: 3, want: []uint64{1, 2}},
		{name: "rejected successor leaves space for another chain", chains: [][]int64{{5, 20}, {5, 99}}, reject: 4, want: []uint64{1, 2, 3}},
		{name: "two slots follow FIFO ties", chains: [][]int64{{5, 20}, {5, 99}}, budget: 2, want: []uint64{1, 2}},
		{name: "oversized successor cannot bypass budget", chains: [][]int64{{5, 20}, {5, 99}}, budget: 3, mutate: func(e []Entry) { e[3].Tx.(*poolTx).gas = 3 }, want: []uint64{1, 2, 3}},
		{name: "long tied run does not jump the queue", chains: [][]int64{{5, 20}, {5, 5, 99}}, budget: 2, want: []uint64{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := selectionChains(tc.chains...)
			if tc.mutate != nil {
				tc.mutate(entries)
			}
			// Arrival remains authoritative regardless of snapshot or map iteration order.
			rng := rand.New(rand.NewSource(42))
			for repeat := 0; repeat < 10; repeat++ {
				rng.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
				var got []uint64
				SelectEntries(entries, 100000, tc.budget, func(e Entry, _ int8) bool {
					if e.Added == tc.reject {
						return false
					}
					got = append(got, e.Added)
					return true
				})
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestSelectionRandomisedDependencies(t *testing.T) {
	rng := rand.New(rand.NewSource(73))
	for trial := 0; trial < 100; trial++ {
		t.Run(fmt.Sprint(trial), func(t *testing.T) {
			chains := make([][]int64, 8)
			for i := range chains {
				for j := 0; j < 20; j++ {
					chains[i] = append(chains[i], int64(rng.Intn(5)))
				}
			}
			entries := selectionChains(chains...)
			expected := map[string]uint64{}
			selected := map[[32]byte]bool{}
			rng.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
			out := SelectEntries(entries, 100000, 60, func(e Entry, _ int8) bool {
				signer := e.Signers[0]
				require.Equal(t, expected[string(signer.Signer)], signer.Sequence)
				require.False(t, selected[e.Key])
				for _, other := range entries {
					s := other.Signers[0]
					if !selected[other.Key] && s.Sequence == expected[string(s.Signer)] {
						require.GreaterOrEqual(t, e.Fee, other.Fee)
					}
				}
				selected[e.Key] = true
				expected[string(signer.Signer)]++
				return true
			})
			require.Len(t, out, 60)
		})
	}
}

func TestGossipIgnoresFees(t *testing.T) {
	t.Run("higher paying successor does not displace older broadcasts", func(t *testing.T) {
		p := newTestPool(100, encodePoolTx)
		a, b, c := fixtureTx(1), fixtureTx(2), fixtureTx(3)
		c.key, c.seq = b.key, 1
		for i, tx := range []*poolTx{a, b, c} {
			require.NoError(t, p.Insert(laneContext(LaneNormal).WithPriority([]int64{5, 5, 99}[i]), tx))
		}
		now := time.Now()
		for _, id := range []byte{1, 2, 3} {
			got := p.Gossip(110, 0, now)
			require.Len(t, got, 1)
			require.Equal(t, id, got[0][0])
		}
	})
}

// Compare against the former gossip preparation: an arrival-ordered snapshot,
// stable broadcast-time sort, and synthetic fee/arrival values for proposal order.
func TestSelectionGossipOrdering(t *testing.T) {
	for _, tc := range []struct {
		name             string
		maxBytes, maxGas uint64
		reject           uint64
		rejectPrivileged bool
	}{
		{name: "full batch", maxBytes: 10000},
		{name: "byte limited", maxBytes: 35},
		{name: "gas limited", maxBytes: 10000, maxGas: 3},
		{name: "independent limits", maxBytes: 300, maxGas: 100},
		{name: "rejected predecessor", maxBytes: 10000, reject: 1},
		{name: "rejected successor", maxBytes: 10000, reject: 2},
		{name: "privileged fallback", maxBytes: 10000, rejectPrivileged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			entries := selectionChains(
				[]int64{1, 99, 5, 5}, []int64{5, 1, 99}, []int64{99, 5, 1},
				[]int64{5, 99, 1}, []int64{1, 5, 99}, []int64{99, 1, 5},
			)
			for i := range entries {
				e := &entries[i]
				e.Lane = int8(i % 3)
				if i%4 != 0 {
					e.LastGossip = now.Add(-time.Duration(i%4) * 5 * time.Second)
				}
				if e.Lane == LaneGovernance {
					e.Tx.(*poolTx).msgs = []sdk.Msg{&gov.MsgVote{ProposalId: 1, Voter: "voter"}}
				}
			}
			entries[2].Unordered = true
			entries[6].Signers = append(entries[6].Signers, sdkmempool.NewSignerData(entries[0].Signers[0].Signer, 4))
			shares := [3]serviceShares{
				LaneCommittee:  {bytes: CommitteeGossipByteShare, gas: CommitteeGossipGasShare},
				LaneGovernance: {bytes: GovernanceGossipByteShare, gas: GovernanceGossipGasShare},
			}
			for _, elapsed := range []time.Duration{0, 0, 0, 4 * time.Second, 5 * time.Second, 10 * time.Second} {
				at := now.Add(elapsed)
				var due []Entry
				for _, e := range entries {
					if e.LastGossip.IsZero() || at.Sub(e.LastGossip) >= 5*time.Second {
						due = append(due, e)
					}
				}
				legacy := append([]Entry(nil), due...)
				sort.SliceStable(legacy, func(i, j int) bool { return legacy[i].LastGossip.Before(legacy[j].LastGossip) })
				for i := range legacy {
					legacy[i].Fee, legacy[i].Added = 0, uint64(i)
				}
				type attempt struct {
					id   uint64
					lane int8
				}
				var wantCalls, gotCalls []attempt
				verify := func(calls *[]attempt) func(Entry, int8) bool {
					return func(e Entry, lane int8) bool {
						id := binary.BigEndian.Uint64(e.Bytes)
						*calls = append(*calls, attempt{id, lane})
						return id != tc.reject && (!tc.rejectPrivileged || lane == LaneNormal)
					}
				}
				want := selectEntries(legacy, tc.maxBytes, tc.maxGas, shares, proposalLess, verify(&wantCalls))
				// The new comparator must work independently of snapshot slice order.
				rand.New(rand.NewSource(42)).Shuffle(len(due), func(i, j int) { due[i], due[j] = due[j], due[i] })
				before := append([]Entry(nil), due...)
				check := verify(&gotCalls)
				got := selectEntries(due, tc.maxBytes, tc.maxGas, shares, gossipLess, func(e Entry, lane int8) bool {
					id := binary.BigEndian.Uint64(e.Bytes)
					original := entries[id-1]
					require.Equal(t, original.Fee, e.Fee)
					require.Equal(t, original.Added, e.Added)
					return check(e, lane)
				})
				require.Equal(t, want, got)
				require.Equal(t, wantCalls, gotCalls)
				require.Equal(t, before, due, "selection preserves snapshot metadata and order")
				var p *Pool
				if tc.reject == 0 && !tc.rejectPrivileged {
					p = newTestPool(100, encodePoolTx)
					for _, e := range entries {
						p.entries[e.Key] = e
					}
					require.Equal(t, want, p.Gossip(tc.maxBytes, tc.maxGas, at))
				}
				for _, bz := range want {
					entries[binary.BigEndian.Uint64(bz)-1].LastGossip = at
				}
				if p != nil {
					require.Equal(t, entries, p.Snapshot(), "only selected gossip timestamps change")
				}
			}
		})
	}
}

func BenchmarkSelection(b *testing.B) {
	for _, count := range []int{1000, 10000, 45000} {
		for _, shape := range []string{"long_equal_run", "many_short_chains"} {
			b.Run(fmt.Sprintf("%s/%d", shape, count), func(b *testing.B) {
				var chains [][]int64
				if shape == "long_equal_run" {
					fees := make([]int64, count)
					fees[len(fees)-1] = 99
					chains = [][]int64{fees}
				} else {
					for i := 0; i < count/2; i++ {
						chains = append(chains, []int64{0, int64(i % 100)})
					}
				}
				entries := selectionChains(chains...)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					SelectEntries(entries, 10000000, 0, func(Entry, int8) bool { return true })
				}
			})
		}
	}
}

func TestGossipConfiguredTransactionSize(t *testing.T) {
	for _, size := range []int{127, 128, 2 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxTxBytes = size
			p := NewPool(cfg, encodePoolTx)
			tx := fixtureTx(1)
			tx.size = size
			require.NoError(t, p.Insert(laneContext(LaneNormal), tx))
			now := time.Now()
			require.Empty(t, p.Gossip(uint64(size), 0, now), "caller budget includes protobuf overhead")
			txs := p.Gossip(0, 0, now)
			require.Len(t, txs, 1, "default batch must fit the configured maximum")
			require.Len(t, txs[0], size)
		})
	}
}
