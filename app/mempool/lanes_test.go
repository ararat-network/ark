package mempool_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app/mempool"
)

// The set under test is arbitrary — this package is mechanism, and the chain's
// real privileged surface is wiring policy owned by app/mempool.go.
func testSet() mempool.Set {
	return mempool.NewSet(mempool.Privilege{
		Lane:  mempool.LaneGovernance,
		Msgs:  []sdk.Msg{&govv1.MsgVote{}, &govv1.MsgDeposit{}},
		Vouch: func(sdk.Context, sdk.Msg) (bool, error) { return true, nil },
	})
}

type stubTx struct {
	msgs []sdk.Msg
}

var _ sdk.Tx = stubTx{}

func (t stubTx) GetMsgs() []sdk.Msg { return t.msgs }

func (t stubTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		set  mempool.Set
		msgs []sdk.Msg
		want int8
	}{
		{
			name: "a set member rides the priority lane",
			set:  testSet(),
			msgs: []sdk.Msg{&govv1.MsgVote{}},
			want: mempool.LaneGovernance,
		},
		{
			name: "all-member multi-message transaction qualifies",
			set:  testSet(),
			msgs: []sdk.Msg{&govv1.MsgVote{}, &govv1.MsgDeposit{}},
			want: mempool.LaneGovernance,
		},
		{
			name: "mixed transaction is towed back to the normal lane",
			set:  testSet(),
			msgs: []sdk.Msg{&govv1.MsgVote{}, &banktypes.MsgSend{}},
			want: mempool.LaneNormal,
		},
		{
			name: "mixed privileged classes stay normal",
			set: mempool.NewSet(
				mempool.Privilege{Lane: mempool.LaneGovernance, Msgs: []sdk.Msg{&govv1.MsgVote{}}, Vouch: func(sdk.Context, sdk.Msg) (bool, error) { return true, nil }},
				mempool.Privilege{Lane: mempool.LaneCommittee, Msgs: []sdk.Msg{&govv1.MsgDeposit{}}, Vouch: func(sdk.Context, sdk.Msg) (bool, error) { return true, nil }},
			),
			msgs: []sdk.Msg{&govv1.MsgVote{}, &govv1.MsgDeposit{}},
			want: mempool.LaneNormal,
		},
		{
			name: "non-member stays in the normal lane",
			set:  testSet(),
			msgs: []sdk.Msg{&banktypes.MsgSend{}},
			want: mempool.LaneNormal,
		},
		{
			name: "empty transaction stays in the normal lane",
			set:  testSet(),
			msgs: nil,
			want: mempool.LaneNormal,
		},
		{
			name: "the zero set privileges nothing",
			set:  mempool.Set{},
			msgs: []sdk.Msg{&govv1.MsgVote{}},
			want: mempool.LaneNormal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.set.Classify(sdk.Context{}, stubTx{msgs: tc.msgs})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSetURLsCopiesMembership(t *testing.T) {
	set := testSet()

	urls := set.URLs()
	require.Len(t, urls, 2)
	require.Contains(t, urls, sdk.MsgTypeURL(&govv1.MsgVote{}))

	delete(urls, sdk.MsgTypeURL(&govv1.MsgVote{}))
	require.Contains(t, set.URLs(), sdk.MsgTypeURL(&govv1.MsgVote{}), "URLs must not alias the set")
}

func TestVouchAndClassify(t *testing.T) {
	failure := errors.New("store unavailable")
	for _, tc := range []struct {
		name     string
		eligible bool
		err      error
		lane     int8
	}{
		{"eligible", true, nil, mempool.LaneGovernance},
		{"ineligible", false, nil, mempool.LaneNormal},
		{"internal failure", false, failure, mempool.LaneNormal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			set := mempool.NewSet(mempool.Privilege{
				Lane:  mempool.LaneGovernance,
				Msgs:  []sdk.Msg{&govv1.MsgVote{}},
				Vouch: func(sdk.Context, sdk.Msg) (bool, error) { calls++; return tc.eligible, tc.err },
			})
			got, err := set.Classify(sdk.Context{}, stubTx{msgs: []sdk.Msg{&govv1.MsgVote{}}})
			require.Equal(t, tc.lane, got)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, 1, calls)
			got, err = set.Classify(sdk.Context{}, stubTx{msgs: []sdk.Msg{&govv1.MsgVote{}, &banktypes.MsgSend{}}})
			require.NoError(t, err)
			require.Equal(t, mempool.LaneNormal, got)
			require.Equal(t, 1, calls, "mixed batches must not run vouches")
			eligible, err := set.Vouch(sdk.Context{}, &banktypes.MsgSend{})
			require.NoError(t, err)
			require.False(t, eligible)
		})
	}
}

// A privilege is a surface and a vouch together: a set cannot be built with
// one missing, and a message cannot carry two.
func TestNewSetRejectsBadWiring(t *testing.T) {
	pass := func(sdk.Context, sdk.Msg) (bool, error) { return true, nil }

	require.PanicsWithValue(t, "lanes: privilege without a vouch", func() {
		mempool.NewSet(mempool.Privilege{Lane: mempool.LaneGovernance, Msgs: []sdk.Msg{&govv1.MsgVote{}}})
	})
	require.PanicsWithValue(t, "lanes: /cosmos.gov.v1.MsgVote privileged twice", func() {
		mempool.NewSet(
			mempool.Privilege{Lane: mempool.LaneGovernance, Msgs: []sdk.Msg{&govv1.MsgVote{}}, Vouch: pass},
			mempool.Privilege{Lane: mempool.LaneGovernance, Msgs: []sdk.Msg{&govv1.MsgVote{}}, Vouch: pass},
		)
	})
}
