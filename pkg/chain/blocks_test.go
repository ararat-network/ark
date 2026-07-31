package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

func TestIsPeriodLastBlock(t *testing.T) {
	tests := []struct {
		name            string
		height          int64
		blocksPerPeriod uint64
		expected        bool
	}{
		{
			name:            "first block is not period end",
			height:          0,
			blocksPerPeriod: 10,
			expected:        false,
		},
		{
			name:            "last block of first period",
			height:          9,
			blocksPerPeriod: 10,
			expected:        true,
		},
		{
			name:            "first block of second period",
			height:          10,
			blocksPerPeriod: 10,
			expected:        false,
		},
		{
			name:            "last block of later period",
			height:          29,
			blocksPerPeriod: 10,
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sdk.WrapSDKContext(sdk.Context{}.WithBlockHeight(tt.height))
			require.Equal(t, tt.expected, chain.IsPeriodLastBlock(ctx, tt.blocksPerPeriod))
		})
	}
}

func TestIsPeriodLastBlockPanicsForZeroPeriod(t *testing.T) {
	ctx := sdk.WrapSDKContext(sdk.Context{})

	require.Panics(t, func() {
		chain.IsPeriodLastBlock(ctx, 0)
	})
}

func TestIsPeriodLastBlockFrom(t *testing.T) {
	tests := []struct {
		name            string
		height          int64
		startHeight     uint64
		blocksPerPeriod uint64
		expected        bool
	}{
		{name: "before period start", height: 9, startHeight: 10, blocksPerPeriod: 4},
		{name: "first period block", height: 10, startHeight: 10, blocksPerPeriod: 4},
		{name: "last block of first anchored period", height: 13, startHeight: 10, blocksPerPeriod: 4, expected: true},
		{name: "first block of second anchored period", height: 14, startHeight: 10, blocksPerPeriod: 4},
		{name: "last block of later anchored period", height: 17, startHeight: 10, blocksPerPeriod: 4, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sdk.WrapSDKContext(sdk.Context{}.WithBlockHeight(tt.height))
			require.Equal(
				t,
				tt.expected,
				chain.IsPeriodLastBlockFrom(ctx, tt.startHeight, tt.blocksPerPeriod),
			)
		})
	}
}

func TestIsPeriodLastBlockFromPanicsForZeroPeriod(t *testing.T) {
	ctx := sdk.WrapSDKContext(sdk.Context{})

	require.Panics(t, func() {
		chain.IsPeriodLastBlockFrom(ctx, 0, 0)
	})
}

func TestLastPeriodBoundary(t *testing.T) {
	tests := []struct {
		name            string
		height          int64
		blocksPerPeriod uint64
		expected        int64
	}{
		{name: "negative height has no closed period", height: -1, blocksPerPeriod: 10, expected: -1},
		{name: "genesis height has no closed period", height: 0, blocksPerPeriod: 10, expected: -1},
		{name: "inside first period", height: 8, blocksPerPeriod: 10, expected: -1},
		{name: "first period boundary", height: 9, blocksPerPeriod: 10, expected: 9},
		{name: "block after first boundary", height: 10, blocksPerPeriod: 10, expected: 9},
		{name: "inside second period", height: 18, blocksPerPeriod: 10, expected: 9},
		{name: "second period boundary", height: 19, blocksPerPeriod: 10, expected: 19},
		{name: "every block closes a unit period", height: 7, blocksPerPeriod: 1, expected: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(
				t,
				tt.expected,
				chain.LastPeriodBoundary(tt.height, tt.blocksPerPeriod),
			)
		})
	}
}

// TestLastPeriodBoundaryAgreesWithIsPeriodLastBlock pins the two against each
// other: a height is its own boundary exactly when it closes a period.
func TestLastPeriodBoundaryAgreesWithIsPeriodLastBlock(t *testing.T) {
	const blocksPerPeriod = 4

	for height := int64(0); height < 20; height++ {
		ctx := sdk.Context{}.WithBlockHeight(height)
		require.Equal(
			t,
			chain.IsPeriodLastBlock(ctx, blocksPerPeriod),
			chain.LastPeriodBoundary(height, blocksPerPeriod) == height,
			"height %d", height,
		)
	}
}

func TestLastPeriodBoundaryPanicsForZeroPeriod(t *testing.T) {
	require.Panics(t, func() {
		_ = chain.LastPeriodBoundary(0, 0)
	})
}
