package lanes_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/abci/lanes"
)

// The set under test is arbitrary — this package is mechanism, and the chain's
// real privileged surface is wiring policy owned by app/mempool.go.
func testSet() lanes.Set {
	return lanes.NewSet(lanes.Privilege{
		Msgs:  []sdk.Msg{&govv1.MsgVote{}, &govv1.MsgDeposit{}},
		Vouch: func(sdk.Context, sdk.Msg) error { return nil },
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

// The vouch is the privilege's admission check: a privileged message runs
// it, and a message outside the set passes without one because it earns no
// lane.
func TestVouch(t *testing.T) {
	refused := errors.New("not the appointed committee")
	set := lanes.NewSet(
		lanes.Privilege{
			Msgs:  []sdk.Msg{&govv1.MsgVote{}},
			Vouch: func(sdk.Context, sdk.Msg) error { return nil },
		},
		lanes.Privilege{
			Msgs:  []sdk.Msg{&govv1.MsgDeposit{}},
			Vouch: func(sdk.Context, sdk.Msg) error { return refused },
		},
	)

	require.NoError(t, set.Vouch(sdk.Context{}, &govv1.MsgVote{}))
	require.ErrorIs(t, set.Vouch(sdk.Context{}, &govv1.MsgDeposit{}), refused)
	require.NoError(t, set.Vouch(sdk.Context{}, &banktypes.MsgSend{}))
	require.NoError(t, lanes.Set{}.Vouch(sdk.Context{}, &govv1.MsgDeposit{}))
}

// A privilege is a surface and a vouch together: a set cannot be built with
// one missing, and a message cannot carry two.
func TestNewSetRejectsBadWiring(t *testing.T) {
	pass := func(sdk.Context, sdk.Msg) error { return nil }

	require.PanicsWithValue(t, "lanes: privilege without a vouch", func() {
		lanes.NewSet(lanes.Privilege{Msgs: []sdk.Msg{&govv1.MsgVote{}}})
	})
	require.PanicsWithValue(t, "lanes: /cosmos.gov.v1.MsgVote privileged twice", func() {
		lanes.NewSet(
			lanes.Privilege{Msgs: []sdk.Msg{&govv1.MsgVote{}}, Vouch: pass},
			lanes.Privilege{Msgs: []sdk.Msg{&govv1.MsgVote{}}, Vouch: pass},
		)
	})
}
