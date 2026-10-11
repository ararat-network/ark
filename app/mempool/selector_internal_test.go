package mempool

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type selectorVerifier struct{ verify func(sdk.Tx) error }

func (v selectorVerifier) TxEncode(tx sdk.Tx) ([]byte, error) { return encodePoolTx(tx), nil }
func (v selectorVerifier) TxDecode([]byte) (sdk.Tx, error)    { return nil, fmt.Errorf("unused decoder") }

func (v selectorVerifier) ProcessProposalVerifyTx([]byte) (sdk.Tx, error) {
	return nil, fmt.Errorf("unused process verifier")
}

func (v selectorVerifier) PrepareProposalVerifyTx(tx sdk.Tx) ([]byte, error) {
	if v.verify != nil {
		if err := v.verify(tx); err != nil {
			return nil, err
		}
	}
	return v.TxEncode(tx)
}

func prepareEntries(t *testing.T, p *Pool, maxBytes, maxGas int64, verify func(sdk.Tx) error) [][]byte {
	t.Helper()
	ctx := sdk.Context{}.WithContext(context.Background()).WithConsensusParams(cmtproto.ConsensusParams{
		Block: &cmtproto.BlockParams{MaxGas: maxGas},
	})
	res, err := p.PrepareProposalHandler(selectorVerifier{verify})(ctx, &abci.RequestPrepareProposal{MaxTxBytes: maxBytes})
	require.NoError(t, err)
	return res.Txs
}

func TestSelectorLaneCaps(t *testing.T) {
	for _, resource := range []string{"gas", "bytes"} {
		t.Run(resource, func(t *testing.T) {
			p := newTestPool(100)
			normal := fixtureTx(1)
			require.NoError(t, admit(p, LaneNormal, math.MaxInt64, normal))
			var want, spills [][]byte
			for _, lane := range []int8{LaneCommittee, LaneGovernance} {
				first := fixtureTx(byte(10 * lane))
				for i := range 6 {
					tx := fixtureTx(byte(10*lane) + byte(i))
					tx.key, tx.seq = first.key, uint64(i)
					tx.size = 98 // 100 bytes including protobuf framing.
					require.NoError(t, admit(p, lane, 0, tx))
					if i < 5 {
						want = append(want, encodePoolTx(tx))
					} else {
						spills = append(spills, encodePoolTx(tx))
					}
				}
			}
			want = append(want, encodePoolTx(normal))
			maxBytes, maxGas := int64(100000), int64(10000)
			if resource == "bytes" {
				maxBytes, maxGas = 10000, 100000
			}
			// The sixth of each lane finds its allowance spent and spills to ordinary fee
			// priority, behind the higher-fee normal transaction.
			got := prepareEntries(t, p, maxBytes, maxGas, nil)
			require.Len(t, got, len(want)+len(spills))
			require.Equal(t, want, got[:len(want)])
			require.ElementsMatch(t, spills, got[len(want):])
			counts, _ := p.Usage()
			require.Equal(t, [3]int{3, 5, 5}, counts, "each class's sixth entry overflowed into normal storage")
		})
	}
}

func TestSelectorOversizedUsesOrdinaryFees(t *testing.T) {
	for _, resource := range []string{"gas", "bytes"} {
		t.Run(resource, func(t *testing.T) {
			p := newTestPool(100)
			large, normal := fixtureTx(1), fixtureTx(2)
			maxBytes, maxGas := int64(10000), int64(10000)
			if resource == "bytes" {
				large.size = 499
			} else {
				large.gas = 501
			}
			require.NoError(t, admit(p, LaneCommittee, 1, large))
			require.NoError(t, admit(p, LaneNormal, 2, normal))
			before := p.Snapshot()
			require.Equal(t, [][]byte{encodePoolTx(normal), encodePoolTx(large)}, prepareEntries(t, p, maxBytes, maxGas, nil))
			require.Equal(t, before, p.Snapshot(), "proposal demotion does not alter pending priority or storage")
			require.Equal(t, [][]byte{encodePoolTx(large), encodePoolTx(normal)}, prepareEntries(t, p, maxBytes*2, maxGas*2, nil), "a larger allowance restores privileged service")
		})
	}
}

func TestSelectorSDKPredecessors(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(fmt.Sprint(oversized), func(t *testing.T) {
			p := newTestPool(100)
			first, next, other := fixtureTx(1), fixtureTx(2), fixtureTx(3)
			next.key, next.seq = first.key, 1
			if oversized {
				first.size = 11000
			}
			require.NoError(t, admit(p, LaneNormal, 1, first))
			require.NoError(t, admit(p, LaneCommittee, 0, next))
			require.NoError(t, admit(p, LaneNormal, 0, other))
			var verified []byte
			got := prepareEntries(t, p, 10000, 10000, func(tx sdk.Tx) error { verified = append(verified, tx.(*poolTx).id); return nil })
			if oversized {
				require.Equal(t, []byte{1, 3}, verified, "the SDK skips the unselected predecessor's successor")
				require.Equal(t, [][]byte{encodePoolTx(other)}, got)
			} else {
				require.Equal(t, []byte{1, 2, 3}, verified)
				require.Equal(t, [][]byte{encodePoolTx(first), encodePoolTx(next), encodePoolTx(other)}, got)
			}
		})
	}
}

func TestSelectorUnboundedGasAndEmptyBudget(t *testing.T) {
	for _, gas := range []int64{-1, 0} {
		t.Run(fmt.Sprint(gas), func(t *testing.T) {
			p := newTestPool(100)
			for i := range 2 {
				tx := fixtureTx(byte(i + 1))
				tx.gas = math.MaxUint64
				require.NoError(t, admit(p, LaneCommittee, int64(2-i), tx))
			}
			require.Len(t, prepareEntries(t, p, 10000, gas, nil), 2)
			require.Empty(t, prepareEntries(t, p, 0, gas, nil))
		})
	}
}

// Entries past a lane's allowance compete by fee with normal traffic, and a full
// block bounds proposal verification for them as it does for normal transactions.
func TestSelectorSpillCompetesByFee(t *testing.T) {
	for _, resource := range []string{"gas", "bytes"} {
		t.Run(resource, func(t *testing.T) {
			p := newTestPool(1000)
			maxBytes, maxGas := int64(100000), int64(10000)
			if resource == "bytes" {
				maxBytes, maxGas = 10000, 100000
			}
			// 120 voters against a block of 100 entries whose allowance holds five votes.
			var votes [][]byte
			for i := range 120 {
				tx := fixtureTx(byte(i + 1))
				tx.size = 98
				require.NoError(t, admit(p, LaneGovernance, int64(10000-10*i), tx))
				votes = append(votes, encodePoolTx(tx))
			}
			normal := fixtureTx(200)
			normal.size = 98
			require.NoError(t, admit(p, LaneNormal, 9905, normal))
			// Five votes ride the lane; spilled votes outbidding the normal entry precede it,
			// the rest follow it until the block is full.
			want := append(append(append([][]byte{}, votes[:10]...), encodePoolTx(normal)), votes[10:99]...)
			var verified int
			got := prepareEntries(t, p, maxBytes, maxGas, func(sdk.Tx) error { verified++; return nil })
			require.Equal(t, want, got)
			require.Equal(t, len(want), verified, "entries past a full block are not verified")
		})
	}
}

// A spilled entry's ordered successors follow it to ordinary priority even when
// they would fit, so none outranks its predecessor; the room left goes to others.
// The sender's unordered entries carry no nonce and stay independent.
func TestSelectorSpilledSenderSuccessors(t *testing.T) {
	p := newTestPool(1000)
	a, b := fixtureTx(1), fixtureTx(2)
	var spender []*poolTx
	for i, gas := range []uint64{300, 300, 100} {
		tx := fixtureTx(byte(10 + i))
		tx.key, tx.seq, tx.gas = a.key, uint64(i), gas
		require.NoError(t, admit(p, LaneGovernance, 2, tx))
		spender = append(spender, tx)
	}
	b.gas = 100
	require.NoError(t, admit(p, LaneGovernance, 1, b))
	unordered := fixtureTx(3)
	unordered.key, unordered.unordered, unordered.timeout = a.key, true, time.Unix(1_800_000_000, 0)
	require.NoError(t, admit(p, LaneGovernance, 0, unordered))
	other := fixtureTx(4)
	require.NoError(t, admit(p, LaneNormal, 1, other))
	var verified []byte
	// The allowance is 500 gas: the first entry fits, the second spills, the third
	// follows it, the other sender takes 100 of the 200 left, and the spilling
	// sender's unordered entry keeps the lane for the last 100. Demoted, it would
	// trail the normal entry instead of preceding it.
	got := prepareEntries(t, p, 100000, 10000, func(tx sdk.Tx) error { verified = append(verified, tx.(*poolTx).id); return nil })
	require.Equal(t, [][]byte{encodePoolTx(spender[0]), encodePoolTx(b), encodePoolTx(spender[1]), encodePoolTx(spender[2]), encodePoolTx(unordered), encodePoolTx(other)}, got)
	require.Equal(t, []byte{10, 2, 11, 12, 3, 4}, verified)
}
