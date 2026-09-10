package mempool

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

func selectionEncoder(tx sdk.Tx) ([]byte, error) { return encodePoolTx(tx), nil }

func TestSelectionReservationsAndOverflow(t *testing.T) {
	for _, lane := range []int8{LaneCommittee, LaneGovernance} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			p := newTestPool(100)
			normal := fixtureTx(1)
			require.NoError(t, admit(p, LaneNormal, 100, normal))
			for i := byte(2); i <= 4; i++ {
				tx := fixtureTx(i)
				require.NoError(t, admit(p, lane, 0, tx))
			}
			var phases []int8
			got := SelectEntries(p.Snapshot(), 10000, 4000, selectionEncoder, func(_ Entry, phase int8) bool { phases = append(phases, phase); return true })
			require.Len(t, got, 4)
			require.Equal(t, []int8{lane, lane, LaneNormal, LaneNormal}, phases)
			require.Equal(t, encodePoolTx(normal), got[2], "overflow competes with normal fees")
		})
	}
}

func TestSelectionIndependentResourceShares(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bytes, gas uint64
	}{{"bytes", 100, 9000}, {"gas", 9000, 100}} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(100)
			tx := fixtureTx(1)
			require.NoError(t, admit(p, LaneCommittee, 0, tx))
			shares := [3]serviceShares{LaneCommittee: {bytes: tc.bytes, gas: tc.gas}}
			var phases []int8
			got := selectEntries(p.Snapshot(), 1000, 1000, shares, selectionEncoder, func(_ Entry, phase int8) bool { phases = append(phases, phase); return true })
			require.Len(t, got, 1)
			require.Equal(t, []int8{LaneNormal}, phases)
		})
	}
}

func TestSelectionUsesSDKOrder(t *testing.T) {
	t.Run("ordinary phase retains upstream order", func(t *testing.T) {
		p := newTestPool(100)
		for i := byte(1); i < 20; i++ {
			require.NoError(t, admit(p, LaneNormal, int64(i%4), fixtureTx(i)))
		}
		entries := p.Snapshot()
		var want [][]byte
		for _, e := range entries {
			want = append(want, encodePoolTx(e.Tx))
		}
		require.Equal(t, want, SelectEntries(entries, math.MaxUint64, 0, selectionEncoder, func(Entry, int8) bool { return true }))
	})
}

func TestSelectionNoAdditionalSenderOrVoteQuota(t *testing.T) {
	for _, lane := range []int8{LaneCommittee, LaneGovernance} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			p := newTestPool(100)
			first := fixtureTx(1)
			for i := byte(1); i <= 5; i++ {
				tx := fixtureTx(i)
				tx.key = first.key
				tx.seq = uint64(i - 1)
				if lane == LaneGovernance {
					tx.msgs = []sdk.Msg{&gov.MsgVote{ProposalId: 1, Voter: sdk.AccAddress(first.key.Address()).String(), Option: gov.VoteOption_VOTE_OPTION_YES}}
				}
				require.NoError(t, admit(p, lane, 0, tx))
			}
			var phases []int8
			require.Len(t, SelectEntries(p.Snapshot(), 20000, 10000, selectionEncoder, func(_ Entry, phase int8) bool { phases = append(phases, phase); return true }), 5)
			require.Equal(t, []int8{lane, lane, lane, lane, lane}, phases)
		})
	}
}

func TestSelectionPredecessorAcrossLanes(t *testing.T) {
	t.Run("normal predecessor does not lose its privileged successor", func(t *testing.T) {
		p := newTestPool(100)
		first := fixtureTx(1)
		next := fixtureTx(2)
		next.key = first.key
		next.seq = 1
		require.NoError(t, admit(p, LaneNormal, 1, first))
		require.NoError(t, admit(p, LaneCommittee, 100, next))
		var sequence uint64
		got := SelectEntries(p.Snapshot(), 20000, 10000, selectionEncoder, func(e Entry, _ int8) bool {
			sigs, err := e.Tx.(interface{ GetSigners() ([][]byte, error) }).GetSigners()
			require.NoError(t, err)
			require.Len(t, sigs, 1)
			tx := e.Tx.(*poolTx)
			require.Equal(t, sequence, tx.seq, "do not validate a blocked preferential successor")
			sequence++
			return true
		})
		require.Equal(t, [][]byte{encodePoolTx(first), encodePoolTx(next)}, got)
	})
}

var _ sdk.Tx = (*poolTx)(nil)

func TestSelectionOrdinaryValidationBeforeResourceFiltering(t *testing.T) {
	for _, lane := range []int8{LaneNormal, LaneCommittee, LaneGovernance} {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			p := newTestPool(100)
			first, next, other := fixtureTx(1), fixtureTx(2), fixtureTx(3)
			first.size = 1000
			next.key, next.seq, next.size = first.key, 1, 20
			other.size = 20
			require.NoError(t, admit(p, lane, 10, first))
			require.NoError(t, admit(p, lane, 10, next))
			require.NoError(t, admit(p, LaneNormal, 0, other))
			var verified []byte
			got := SelectEntries(p.Snapshot(), 100, 1000, selectionEncoder, func(e Entry, phase int8) bool {
				require.Equal(t, LaneNormal, phase, "oversized preferential candidates must wait for ordinary service")
				verified = append(verified, e.Tx.(*poolTx).id)
				return true
			})
			require.Equal(t, []byte{1, 3}, verified, "SDK verifies the oversized predecessor, then skips its successor")
			require.Equal(t, [][]byte{encodePoolTx(other)}, got)
		})
	}
}
