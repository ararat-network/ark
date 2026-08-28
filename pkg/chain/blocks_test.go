package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
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
			ctx := sdk.Context{}.WithBlockHeight(tt.height)
			require.Equal(t, tt.expected, chain.IsPeriodLastBlock(ctx, tt.blocksPerPeriod))
		})
	}
}

func TestIsPeriodLastBlockPanicsForZeroPeriod(t *testing.T) {
	ctx := sdk.Context{}

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
			ctx := sdk.Context{}.WithBlockHeight(tt.height)
			require.Equal(
				t,
				tt.expected,
				chain.IsPeriodLastBlockFrom(ctx, tt.startHeight, tt.blocksPerPeriod),
			)
		})
	}
}

func TestIsPeriodLastBlockFromPanicsForZeroPeriod(t *testing.T) {
	ctx := sdk.Context{}

	require.Panics(t, func() {
		chain.IsPeriodLastBlockFrom(ctx, 0, 0)
	})
}
