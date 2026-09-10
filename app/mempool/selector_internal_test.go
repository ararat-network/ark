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
			var want [][]byte
			for _, lane := range []int8{LaneCommittee, LaneGovernance} {
				first := fixtureTx(byte(10 * lane))
				for i := range 6 {
					tx := fixtureTx(byte(10*lane) + byte(i))
					tx.key, tx.seq = first.key, uint64(i)
					tx.size = 98 // 100 bytes including protobuf framing.
					require.NoError(t, admit(p, lane, 0, tx))
					if i < 5 {
						want = append(want, encodePoolTx(tx))
					}
				}
			}
			want = append(want, encodePoolTx(normal))
			maxBytes, maxGas := int64(100000), int64(10000)
			if resource == "bytes" {
				maxBytes, maxGas = 10000, 100000
			}
			require.Equal(t, want, prepareEntries(t, p, maxBytes, maxGas, nil))
			counts, _ := p.Usage()
			require.Equal(t, [3]int{3, 5, 5}, counts, "overflow storage does not avoid service caps")
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

// A saturated lane bounds proposal verification: entries past the allowance
// never reach the SDK loop, which verifies before it can decline.
func TestSelectorSaturatedLaneBoundsVerification(t *testing.T) {
	for _, resource := range []string{"gas", "bytes"} {
		t.Run(resource, func(t *testing.T) {
			p := newTestPool(1000)
			maxBytes, maxGas := int64(100000), int64(10000)
			if resource == "bytes" {
				maxBytes, maxGas = 10000, 100000
			}
			// Forty voters; the allowance holds five 100-gas, 100-byte votes.
			var want [][]byte
			for i := range 40 {
				tx := fixtureTx(byte(i + 1))
				tx.size = 98
				require.NoError(t, admit(p, LaneGovernance, int64(40-i), tx))
				if i < 5 {
					want = append(want, encodePoolTx(tx))
				}
			}
			normal := fixtureTx(50)
			require.NoError(t, admit(p, LaneNormal, 0, normal))
			want = append(want, encodePoolTx(normal))
			var verified int
			got := prepareEntries(t, p, maxBytes, maxGas, func(sdk.Tx) error { verified++; return nil })
			require.Equal(t, want, got)
			require.Equal(t, len(want), verified, "entries past the allowance are not verified")
		})
	}
}

// An ordered successor of an entry left out waits with it, unverified; an
// unordered action from the same sender is independent and still served.
func TestSelectorWaitingSenderSuccessors(t *testing.T) {
	p := newTestPool(1000)
	voter := fixtureTx(1)
	var want [][]byte
	for i := range 6 {
		tx := fixtureTx(byte(i + 1))
		tx.key, tx.seq, tx.size = voter.key, uint64(i), 98
		require.NoError(t, admit(p, LaneGovernance, 0, tx))
		if i < 5 {
			want = append(want, encodePoolTx(tx))
		}
	}
	send := fixtureTx(10)
	send.key, send.seq = voter.key, 6
	require.NoError(t, admit(p, LaneNormal, 5, send))
	unordered := fixtureTx(11)
	unordered.key, unordered.unordered, unordered.timeout = voter.key, true, time.Unix(1_800_000_000, 0)
	require.NoError(t, admit(p, LaneNormal, 3, unordered))
	other := fixtureTx(12)
	require.NoError(t, admit(p, LaneNormal, 1, other))
	want = append(want, encodePoolTx(unordered), encodePoolTx(other))
	var verified []byte
	got := prepareEntries(t, p, 100000, 10000, func(tx sdk.Tx) error { verified = append(verified, tx.(*poolTx).id); return nil })
	require.Equal(t, want, got, "the ordered successor waits with the sixth vote; the unordered action does not")
	require.Equal(t, []byte{1, 2, 3, 4, 5, 11, 12}, verified)
}
