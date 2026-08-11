package lanes_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"ark/abci/lanes"
)

// The set under test is arbitrary — this package is mechanism, and the chain's
// real privileged surface is wiring policy owned by app/mempool.go.
func testSet() lanes.Set {
	return lanes.NewSet(&govv1.MsgVote{}, &govv1.MsgDeposit{})
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
		set  lanes.Set
		msgs []sdk.Msg
		want int8
	}{
		{
			name: "a set member rides the priority lane",
			set:  testSet(),
			msgs: []sdk.Msg{&govv1.MsgVote{}},
			want: lanes.LanePriority,
		},
		{
			name: "all-member multi-message transaction qualifies",
			set:  testSet(),
			msgs: []sdk.Msg{&govv1.MsgVote{}, &govv1.MsgDeposit{}},
			want: lanes.LanePriority,
		},
		{
			name: "mixed transaction is towed back to the normal lane",
			set:  testSet(),
			msgs: []sdk.Msg{&govv1.MsgVote{}, &banktypes.MsgSend{}},
			want: lanes.LaneNormal,
		},
		{
			name: "non-member stays in the normal lane",
			set:  testSet(),
			msgs: []sdk.Msg{&banktypes.MsgSend{}},
			want: lanes.LaneNormal,
		},
		{
			name: "empty transaction stays in the normal lane",
			set:  testSet(),
			msgs: nil,
			want: lanes.LaneNormal,
		},
		{
			name: "the zero set privileges nothing",
			set:  lanes.Set{},
			msgs: []sdk.Msg{&govv1.MsgVote{}},
			want: lanes.LaneNormal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.set.Classify(stubTx{msgs: tc.msgs}))
		})
	}
}

func TestSetURLsCopiesMembership(t *testing.T) {
	set := testSet()

	urls := set.URLs()
	require.Len(t, urls, 2)
	require.Contains(t, urls, sdk.MsgTypeURL(&govv1.MsgVote{}))

	delete(urls, sdk.MsgTypeURL(&govv1.MsgVote{}))
	require.True(t, set.Has(&govv1.MsgVote{}), "URLs must not alias the set")
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a    lanes.Priority
		b    lanes.Priority
		want int
	}{
		{
			name: "zero-fee priority lane beats any normal-lane fee",
			a:    lanes.Priority{Lane: lanes.LanePriority, Fee: 0},
			b:    lanes.Priority{Lane: lanes.LaneNormal, Fee: 1 << 40},
			want: 1,
		},
		{
			name: "normal lane loses to the priority lane",
			a:    lanes.Priority{Lane: lanes.LaneNormal, Fee: 1 << 40},
			b:    lanes.Priority{Lane: lanes.LanePriority, Fee: 0},
			want: -1,
		},
		{
			name: "fee orders within a lane",
			a:    lanes.Priority{Lane: lanes.LanePriority, Fee: 10},
			b:    lanes.Priority{Lane: lanes.LanePriority, Fee: 7},
			want: 1,
		},
		{
			name: "equal keys tie",
			a:    lanes.Priority{Lane: lanes.LaneNormal, Fee: 3},
			b:    lanes.Priority{Lane: lanes.LaneNormal, Fee: 3},
			want: 0,
		},
		{
			name: "minimum value sorts below every real key",
			a:    lanes.TxPriority(testSet()).MinValue,
			b:    lanes.Priority{Lane: lanes.LaneNormal, Fee: -(1 << 40)},
			want: -1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, lanes.Compare(tc.a, tc.b))
		})
	}
}

func TestGetTxPriorityCarriesAnteFeePriority(t *testing.T) {
	ctx := sdk.Context{}.WithPriority(42)

	got := lanes.TxPriority(testSet()).GetTxPriority(ctx, stubTx{msgs: []sdk.Msg{&govv1.MsgVote{}}})

	require.Equal(t, lanes.Priority{Lane: lanes.LanePriority, Fee: 42}, got)
}
