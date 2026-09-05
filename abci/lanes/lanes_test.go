package lanes_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/abci/lanes"
)

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
