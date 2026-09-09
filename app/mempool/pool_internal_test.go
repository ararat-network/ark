package mempool

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

func newTestPool(maxTxs int, encode sdk.TxEncoder) *Pool {
	cfg := DefaultConfig()
	cfg.MaxTxs = maxTxs
	return NewPool(cfg, encode)
}

type poolTx struct {
	msgs      []sdk.Msg
	id        byte
	size      int
	gas       uint64
	seq       uint64
	key       cryptotypes.PubKey
	signers   []sdkmempool.SignerData
	unordered bool
	timeout   time.Time
}

func (t *poolTx) GetMsgs() []sdk.Msg {
	if t.msgs != nil {
		return t.msgs
	}
	return []sdk.Msg{&bank.MsgSend{}}
}
func (t *poolTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (t *poolTx) GetGas() uint64                        { return t.gas }
func (t *poolTx) GetFee() sdk.Coins                     { return nil }
func (t *poolTx) FeePayer() []byte                      { return t.key.Address() }
func (t *poolTx) FeeGranter() []byte                    { return nil }
func (t *poolTx) GetSigners() ([][]byte, error) {
	if t.signers == nil {
		return [][]byte{t.key.Address()}, nil
	}
	out := make([][]byte, len(t.signers))
	for i, signer := range t.signers {
		out[i] = signer.Signer
	}
	return out, nil
}

func (t *poolTx) GetUnordered() bool             { return t.unordered }
func (t *poolTx) GetTimeoutTimeStamp() time.Time { return t.timeout }

func (t *poolTx) GetPubKeys() ([]cryptotypes.PubKey, error) { return []cryptotypes.PubKey{t.key}, nil }

func (t *poolTx) GetSignaturesV2() ([]signing.SignatureV2, error) {
	if t.signers != nil {
		out := make([]signing.SignatureV2, len(t.signers))
		for i, signer := range t.signers {
			out[i] = signing.SignatureV2{PubKey: t.key, Sequence: signer.Sequence}
		}
		return out, nil
	}
	return []signing.SignatureV2{{PubKey: t.key, Sequence: t.seq}}, nil
}

func encodePoolTx(tx sdk.Tx) ([]byte, error) {
	t := tx.(*poolTx)
	bz := make([]byte, t.size)
	bz[0] = t.id
	return bz, nil
}

func fixtureTx(id byte) *poolTx {
	return &poolTx{id: id, size: 100, gas: 100, key: secp256k1.GenPrivKey().PubKey()}
}

func laneContext(lane int8) sdk.Context {
	return WithLane(sdk.Context{}.WithContext(context.Background()), lane)
}

func TestPoolCapacityIsolation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lane  int8
		count int
	}{{"normal full", LaneNormal, 18}, {"governance full", LaneGovernance, 1}, {"committee full", LaneCommittee, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(20, encodePoolTx)
			for i := 0; i < 18; i++ {
				require.NoError(t, p.Insert(laneContext(LaneNormal), fixtureTx(byte(i+1))))
			}
			for i := 0; i < tc.count && tc.lane != LaneNormal; i++ {
				require.NoError(t, p.Insert(laneContext(tc.lane), fixtureTx(byte(i+30))))
			}
			require.ErrorIs(t, p.Insert(laneContext(tc.lane), fixtureTx(80)), ErrCapacity)
			for _, other := range []int8{LaneGovernance, LaneCommittee} {
				if other != tc.lane {
					require.NoError(t, p.Insert(laneContext(other), fixtureTx(byte(90+other))))
				}
			}
		})
	}
}

func TestPoolByteBoundAndRemoval(t *testing.T) {
	t.Run("zero selects a bounded default with a separate byte limit", func(t *testing.T) {
		p := newTestPool(0, encodePoolTx)
		for i := 0; i < 57; i++ {
			tx := fixtureTx(byte(i + 1))
			tx.size = 1 << 20
			require.NoError(t, p.Insert(laneContext(0), tx))
		}
		tx := fixtureTx(90)
		tx.size = 1 << 20
		require.ErrorIs(t, p.Insert(laneContext(0), tx), ErrCapacity)
		tx.size = 768 << 10 // fits the sender byte reserve, exceeds remaining normal bytes
		require.NoError(t, p.Insert(laneContext(LaneGovernance), tx))
		require.NoError(t, p.Remove(tx))
		counts, bz := p.Usage()
		require.Zero(t, counts[LaneGovernance])
		require.Zero(t, bz[LaneGovernance])
	})
	t.Run("optimistic execution does not retire a transaction", func(t *testing.T) {
		p := newTestPool(20, encodePoolTx)
		tx := fixtureTx(1)
		require.NoError(t, p.Insert(laneContext(0), tx))
		require.NoError(t, p.RemoveWithReason(context.Background(), tx, sdkmempool.RemoveReason{Caller: sdkmempool.CallerRunTxFinalize}))
		require.Equal(t, 1, p.CountTx())
		raw, _ := encodePoolTx(tx)
		p.RemoveBytes(raw)
		require.Zero(t, p.CountTx())
	})
}

func TestPoolSenderProtection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		votes    bool
		reserved int
	}{
		{"sender quarter", false, 5}, {"repeated vote", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(400, encodePoolTx)
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
			counts, _ := p.Usage()
			require.Equal(t, tc.reserved, counts[LaneGovernance])
			require.Equal(t, 6-tc.reserved, counts[LaneNormal])
		})
	}
}

func TestPoolCapacityRounding(t *testing.T) {
	for _, capacity := range []int{1, 19, 21} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			p := newTestPool(capacity, encodePoolTx)
			normal := capacity - capacity*CommitteeAdmissionCountShare/10000 - capacity*GovernanceAdmissionCountShare/10000
			for i := 0; i < normal; i++ {
				require.NoError(t, p.Insert(laneContext(LaneNormal), fixtureTx(byte(i+1))))
			}
			require.ErrorIs(t, p.Insert(laneContext(LaneNormal), fixtureTx(99)), ErrCapacity)
		})
	}
}

func TestPoolReservationRelease(t *testing.T) {
	for _, tc := range []struct {
		name          string
		lane          int8
		votes         bool
		duplicateVote bool
		large         bool
	}{
		{name: "committee sender count", lane: LaneCommittee},
		{name: "governance sender count", lane: LaneGovernance},
		{name: "sender bytes", lane: LaneGovernance, large: true},
		{name: "voter and proposal", lane: LaneGovernance, votes: true},
		{name: "repeated vote in one transaction", lane: LaneGovernance, votes: true, duplicateVote: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(80, encodePoolTx) // One reserved slot per sender in each class.
			if tc.large || tc.votes {
				p = newTestPool(400, encodePoolTx) // Count capacity cannot explain byte or vote refusals.
			}
			key := fixtureTx(1).key
			makeTx := func(id byte) *poolTx {
				tx := fixtureTx(id)
				tx.key, tx.seq = key, uint64(id)
				if tc.large {
					tx.size = 768 << 10
				}
				if tc.votes {
					tx.msgs = []sdk.Msg{&gov.MsgVote{ProposalId: 1, Voter: sdk.AccAddress(key.Address()).String()}}
					if tc.duplicateVote {
						tx.msgs = append(tx.msgs, tx.msgs[0])
					}
				}
				return tx
			}
			ctx := laneContext(tc.lane)
			first, overflow := makeTx(1), makeTx(2)
			require.NoError(t, p.Insert(ctx, first))
			require.NoError(t, p.Insert(ctx, overflow))
			require.Equal(t, LaneNormal, p.Snapshot()[1].Slot)
			require.NoError(t, p.Remove(overflow))
			require.NoError(t, p.Insert(ctx, makeTx(3)))
			require.Equal(t, LaneNormal, p.Snapshot()[1].Slot, "removing normal overflow must not free reserved resources")
			require.NoError(t, p.Remove(first))
			require.NoError(t, p.Insert(ctx, makeTx(4)))
			require.Equal(t, tc.lane, p.Snapshot()[1].Slot, "removing the reserved entry frees its sender and vote allocation")
			p.Reset()
			require.NoError(t, p.Insert(ctx, first))
			require.Equal(t, tc.lane, p.Snapshot()[0].Slot, "reset releases all admission indexes")
		})
	}
}

func TestPoolNonceConflict(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset=%t", reset), func(t *testing.T) {
			p := newTestPool(20, encodePoolTx)
			first, conflict := fixtureTx(1), fixtureTx(2)
			conflict.key = first.key
			ctx := laneContext(LaneNormal)
			require.NoError(t, p.Insert(ctx, first))
			require.ErrorIs(t, p.Insert(ctx, first), ErrDuplicate)
			require.ErrorContains(t, p.Insert(ctx, conflict), "sender sequence already pending")
			require.Equal(t, 1, p.CountTx())
			if reset {
				p.Reset()
			} else {
				require.NoError(t, p.Remove(first))
				require.ErrorIs(t, p.Remove(first), sdkmempool.ErrTxNotFound)
			}
			require.NoError(t, p.Insert(ctx, conflict))
		})
	}
}

func TestPoolSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(a, b, c *poolTx) ([3]int8, [3]int64)
		want  []byte
	}{
		{
			name: "committee then governance then fees",
			setup: func(_, _, _ *poolTx) ([3]int8, [3]int64) {
				return [3]int8{LaneGovernance, LaneNormal, LaneCommittee}, [3]int64{0, 100, 0}
			},
			want: []byte{3, 1, 2},
		},
		{
			name:  "fee ties preserve arrival order",
			setup: func(_, _, _ *poolTx) ([3]int8, [3]int64) { return [3]int8{}, [3]int64{42, 42, 42} },
			want:  []byte{1, 2, 3},
		},
		{
			name: "fee tie ignores the higher paying successor",
			setup: func(_, b, c *poolTx) ([3]int8, [3]int64) {
				c.key, c.seq = b.key, 1
				return [3]int8{}, [3]int64{5, 5, 99}
			},
			want: []byte{1, 2, 3},
		},
		{
			name: "message type alone earns no priority",
			setup: func(a, _, _ *poolTx) ([3]int8, [3]int64) {
				a.msgs = []sdk.Msg{&gov.MsgVote{ProposalId: 1}}
				return [3]int8{}, [3]int64{0, 100, 50}
			},
			want: []byte{2, 3, 1},
		},
		{
			name: "all signer predecessors precede a privileged successor",
			setup: func(a, b, _ *poolTx) ([3]int8, [3]int64) {
				a.signers = []sdkmempool.SignerData{
					sdkmempool.NewSignerData(sdk.AccAddress(a.key.Address()), 0),
					sdkmempool.NewSignerData(sdk.AccAddress(b.key.Address()), 1),
				}
				return [3]int8{LaneGovernance, LaneNormal, LaneNormal}, [3]int64{100, 10, 50}
			},
			want: []byte{3, 2, 1},
		},
		{
			name: "unordered transaction has no ordered predecessor",
			setup: func(a, b, _ *poolTx) ([3]int8, [3]int64) {
				a.key, a.unordered, a.timeout = b.key, true, time.Unix(100, 0)
				return [3]int8{}, [3]int64{100, 10, 50}
			},
			want: []byte{1, 3, 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(100, encodePoolTx)
			txs := []*poolTx{fixtureTx(1), fixtureTx(2), fixtureTx(3)}
			lanes, fees := tc.setup(txs[0], txs[1], txs[2])
			for i, tx := range txs {
				require.NoError(t, p.Insert(laneContext(lanes[i]).WithPriority(fees[i]), tx))
			}
			var got []byte
			for it := p.Select(context.Background(), nil); it != nil; it = it.Next() {
				got = append(got, it.Tx().(*poolTx).id)
			}
			require.Equal(t, tc.want, got)
			got = nil
			for _, bz := range SelectEntries(p.Snapshot(), math.MaxUint64, 0, func(Entry, int8) bool { return true }) {
				got = append(got, bz[0])
			}
			require.Equal(t, tc.want, got)
			calls := 0
			p.SelectBy(context.Background(), nil, func(tx sdk.Tx) bool {
				calls++
				require.Equal(t, tc.want[0], tx.(*poolTx).id)
				require.NoError(t, p.Remove(tx), "callbacks must run outside the pool lock")
				return false
			})
			require.Equal(t, 1, calls)
		})
	}
}

// Exercise large pending chains so regressions to a full scan per insertion
// are visible independently of ante cost. 45,000 fits the maximum normal lane.
func BenchmarkPoolAdmission(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000, 45000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			key := fixtureTx(1).key
			txs := make([]*poolTx, count)
			for i := range txs {
				txs[i] = &poolTx{key: key, seq: uint64(i), gas: 1}
			}
			encode := func(tx sdk.Tx) ([]byte, error) {
				bz := make([]byte, 8)
				binary.BigEndian.PutUint64(bz, tx.(*poolTx).seq)
				return bz, nil
			}
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				p := newTestPool(MaxTxLimit, encode)
				for _, tx := range txs {
					if err := p.Insert(laneContext(LaneNormal), tx); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func TestPoolPreservesContextBytes(t *testing.T) {
	t.Run("SDK insertion retains original wire identity", func(t *testing.T) {
		tx := fixtureTx(1)
		wire := []byte{9, 8, 7}
		p := newTestPool(20, func(sdk.Tx) ([]byte, error) { t.Fatal("must not re-encode SDK transaction bytes"); return nil, nil })
		require.NoError(t, p.Insert(laneContext(LaneNormal).WithTxBytes(wire), tx))
		require.True(t, p.Has(wire))
		wire[0] = 0
		require.Equal(t, []byte{9, 8, 7}, p.Snapshot()[0].Bytes, "stored bytes are immutable")
		p.RemoveBytes([]byte{9, 8, 7})
		require.Zero(t, p.CountTx())
	})
}

func TestConfiguredTransactionSize(t *testing.T) {
	for _, limit := range []int{512, 2 << 20} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxTxBytes = limit
			p := NewPool(cfg, encodePoolTx)
			tx := fixtureTx(1)
			tx.size = limit
			require.NoError(t, p.Insert(laneContext(LaneNormal), tx))
			oversized := fixtureTx(2)
			oversized.size = limit + 1
			require.ErrorContains(t, p.Insert(laneContext(LaneNormal), oversized), "transaction exceeds")
			require.Equal(t, 1, p.CountTx())
		})
	}
}

func TestConfiguredPoolByteReservations(t *testing.T) {
	for _, lane := range []int8{LaneGovernance, LaneCommittee} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxTxsBytes = 8000
			p := NewPool(cfg, encodePoolTx)
			normal := fixtureTx(1)
			normal.size = 7200
			require.NoError(t, p.Insert(laneContext(LaneNormal), normal))
			require.False(t, p.MayFit(LaneNormal, 1))
			require.ErrorIs(t, p.Insert(laneContext(LaneNormal), fixtureTx(2)), ErrCapacity)
			// Each privileged lane owns 400 bytes. One sender owns at most 100.
			for i := byte(3); i < 7; i++ {
				tx := fixtureTx(i)
				tx.size = 101
				require.ErrorIs(t, p.Insert(laneContext(lane), tx), ErrCapacity)
				tx.size = 100
				require.NoError(t, p.Insert(laneContext(lane), tx))
				next := *tx
				next.id += 10
				next.seq++
				require.ErrorIs(t, p.Insert(laneContext(lane), &next), ErrCapacity)
			}
			require.False(t, p.MayFit(lane, 1))
			p.RemoveBytes([]byte{2}) // An unknown transaction frees no capacity.
			bytes, err := encodePoolTx(normal)
			require.NoError(t, err)
			p.RemoveBytes(bytes)
			require.True(t, p.MayFit(LaneNormal, 7200))
			require.NoError(t, p.Insert(laneContext(lane), fixtureTx(20)))
			entry := p.Snapshot()[4]
			require.Equal(t, lane, entry.Lane, "overflow retains priority eligibility")
			require.Equal(t, LaneNormal, entry.Slot)
		})
	}
}

func TestLargePoolByteBudgetDoesNotOverflow(t *testing.T) {
	for _, lane := range []int8{LaneNormal, LaneGovernance, LaneCommittee} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxTxsBytes = math.MaxInt64
			p := NewPool(cfg, encodePoolTx)
			require.NoError(t, p.Insert(laneContext(lane), fixtureTx(1)))
			require.Equal(t, lane, p.Snapshot()[0].Slot)
			require.True(t, p.MayFit(lane, 1))
		})
	}
}
