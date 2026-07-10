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
