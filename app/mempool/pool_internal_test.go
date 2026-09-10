package mempool

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
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
)

func newTestPool(maxTxs int) *Pool { cfg := DefaultConfig(); cfg.MaxTxs = maxTxs; return NewPool(cfg) }

func admissionContext(tx sdk.Tx, lane int8, fee int64) sdk.Context {
	return WithLane(sdk.Context{}.WithContext(context.Background()).WithTxBytes(encodePoolTx(tx)).WithPriority(fee), lane)
}

func admit(p *Pool, lane int8, fee int64, tx sdk.Tx) error {
	return p.Insert(admissionContext(tx, lane, fee), tx)
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

func encodePoolTx(tx sdk.Tx) []byte {
	t := tx.(*poolTx)
	bz := make([]byte, t.size)
	bz[0] = t.id
	return bz
}

func fixtureTx(id byte) *poolTx {
	return &poolTx{id: id, size: 100, gas: 100, key: secp256k1.GenPrivKey().PubKey()}
}

func TestPoolCapacityIsolation(t *testing.T) {
	for _, lane := range []int8{LaneNormal, LaneGovernance, LaneCommittee} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			p := newTestPool(20)
			for i := range 18 {
				require.NoError(t, admit(p, LaneNormal, 0, fixtureTx(byte(i+1))))
			}
			require.ErrorIs(t, admit(p, LaneNormal, 0, fixtureTx(40)), ErrCapacity)
			for _, reserved := range []int8{LaneGovernance, LaneCommittee} {
				require.NoError(t, admit(p, reserved, 0, fixtureTx(byte(50+reserved))))
			}
			require.ErrorIs(t, admit(p, lane, 0, fixtureTx(60)), sdkmempool.ErrMempoolTxMaxCapacity)
			counts, _ := p.Usage()
			require.Equal(t, [3]int{18, 1, 1}, counts)
		})
	}
}

func TestPoolByteReservations(t *testing.T) {
	for _, lane := range []int8{LaneGovernance, LaneCommittee} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxTxs = 100
			cfg.MaxTxsBytes = 2000
			cfg.MaxTxBytes = 1800
			p := NewPool(cfg)
			normal := fixtureTx(1)
			normal.size = 1800
			require.NoError(t, admit(p, LaneNormal, 0, normal))
			reserved := fixtureTx(2)
			reserved.size = 101
			require.ErrorIs(t, admit(p, lane, 0, reserved), ErrCapacity)
			reserved.size = 100
			require.NoError(t, admit(p, lane, 0, reserved))
			require.ErrorIs(t, admit(p, lane, 0, fixtureTx(3)), ErrCapacity)
			require.NoError(t, p.Remove(reserved))
			require.NoError(t, admit(p, lane, 0, fixtureTx(3)))
			require.ErrorIs(t, p.Remove(reserved), sdkmempool.ErrTxNotFound)
		})
	}
}

func TestPoolReservationOverflow(t *testing.T) {
	for _, lane := range []int8{LaneGovernance, LaneCommittee} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			p := newTestPool(100)
			first := fixtureTx(1)
			for i := range 6 {
				tx := fixtureTx(byte(i + 1))
				tx.key = first.key
				tx.seq = uint64(i)
				require.NoError(t, admit(p, lane, 0, tx))
			}
			counts, _ := p.Usage()
			require.Equal(t, 5, counts[lane])
			require.Equal(t, 1, counts[LaneNormal])
		})
	}
}

func TestPoolSDKOrderingAndReplacement(t *testing.T) {
	for _, name := range []string{"fee ties", "mixed fees", "unordered", "replacement"} {
		t.Run(name, func(t *testing.T) {
			p := newTestPool(1000)
			cfg := sdkmempool.DefaultPriorityNonceMempoolConfig()
			cfg.MaxTx = 1000
			upstream := sdkmempool.NewPriorityMempool(cfg)
			rng := rand.New(rand.NewSource(42))
			senders := make([]*poolTx, 8)
			for i := range senders {
				senders[i] = fixtureTx(byte(i + 1))
			}
			for i := range 80 {
				tx := fixtureTx(byte(i + 1))
				tx.key = senders[i%8].key
				tx.seq = uint64(i / 8)
				fee := int64(7)
				if name == "mixed fees" {
					fee = rng.Int63n(100)
				}
				if name == "unordered" && i%3 == 0 {
					tx.unordered = true
					tx.seq = 0
					tx.timeout = time.Unix(100+int64(i), 0)
				}
				if name == "replacement" {
					tx.seq %= 3
					fee = int64(i)
				}
				ctx := admissionContext(tx, LaneNormal, fee)
				require.NoError(t, upstream.Insert(ctx, tx))
				require.NoError(t, p.Insert(ctx, tx))
			}
			var want []sdk.Tx
			upstream.SelectBy(context.Background(), nil, func(tx sdk.Tx) bool { want = append(want, tx); return true })
			var got []sdk.Tx
			for _, e := range p.Snapshot() {
				got = append(got, e.Tx)
			}
			require.Equal(t, want, got)
			require.Equal(t, upstream.CountTx(), p.CountTx())
			counts, sizes := p.Usage()
			require.Equal(t, len(want), counts[0])
			require.Equal(t, int64(len(want)*100), sizes[0])
			for _, tx := range want {
				require.NoError(t, p.Remove(tx))
				require.NoError(t, upstream.Remove(tx))
			}
			require.Zero(t, p.CountTx())
			counts, sizes = p.Usage()
			require.Equal(t, [3]int{}, counts)
			require.Equal(t, [3]int64{}, sizes)
		})
	}
}

func TestPoolReplacementMetadata(t *testing.T) {
	t.Run("replacement changes hash and allocation without keeping a second transaction", func(t *testing.T) {
		p := newTestPool(100)
		old := fixtureTx(1)
		replacement := fixtureTx(2)
		replacement.key = old.key
		require.NoError(t, admit(p, LaneCommittee, 1, old))
		require.NoError(t, admit(p, LaneNormal, 2, replacement))
		require.False(t, p.Has(encodePoolTx(old)))
		require.True(t, p.Has(encodePoolTx(replacement)))
		counts, sizes := p.Usage()
		require.Equal(t, [3]int{1, 0, 0}, counts)
		require.Equal(t, [3]int64{100, 0, 0}, sizes)
	})
}

func TestPoolCapacityRounding(t *testing.T) {
	for _, count := range []int{1, 19, 20, 21, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			p := newTestPool(count)
			for i := range count - count/20 {
				require.NoError(t, admit(p, LaneCommittee, 0, fixtureTx(byte(i+1))))
			}
			counts, _ := p.Usage()
			require.Equal(t, count/20, counts[LaneCommittee])
		})
	}
}

func TestPoolRecordsWireSize(t *testing.T) {
	t.Run("admitted wire length, not the encoder", func(t *testing.T) {
		p := newTestPool(20)
		ctx := WithLane(sdk.Context{}.WithContext(context.Background()).WithTxBytes([]byte{9, 8, 7}), LaneNormal)
		require.NoError(t, p.Insert(ctx, fixtureTx(1)))
		require.Equal(t, 3, p.Snapshot()[0].Size)
		_, sizes := p.Usage()
		require.Equal(t, [3]int64{3, 0, 0}, sizes)
	})
}

func BenchmarkPoolAdmission(b *testing.B) {
	benchmarkPoolAdmission(b, func() sdkmempool.Mempool { return newTestPool(MaxTxLimit) })
}

func BenchmarkSDKPoolAdmission(b *testing.B) {
	benchmarkPoolAdmission(b, func() sdkmempool.Mempool {
		cfg := sdkmempool.DefaultPriorityNonceMempoolConfig()
		cfg.MaxTx = MaxTxLimit
		return sdkmempool.NewPriorityMempool(cfg)
	})
}

func benchmarkPoolAdmission(b *testing.B, newPool func() sdkmempool.Mempool) {
	b.Helper()
	for _, count := range []int{1000, 5000, 10000, 45000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			key := fixtureTx(1).key
			txs := make([]*poolTx, count)
			contexts := make([]sdk.Context, count)
			for i := range txs {
				txs[i] = &poolTx{key: key, seq: uint64(i), gas: 1}
				raw := make([]byte, 8)
				binary.BigEndian.PutUint64(raw, uint64(i))
				contexts[i] = WithLane(sdk.Context{}.WithContext(context.Background()).WithTxBytes(raw), LaneNormal)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				p := newPool()
				for i, tx := range txs {
					if err := p.Insert(contexts[i], tx); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
