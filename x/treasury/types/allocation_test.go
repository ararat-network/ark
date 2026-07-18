package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/x/treasury/types"
)

func TestExpansionAllocationTotalBurn(t *testing.T) {
	allocation := types.ExpansionAllocation{
		SpreadAndDustBurn: math.NewInt(10),
		OverflowBurn:      math.NewInt(20),
	}

	require.Equal(t, math.NewInt(30), allocation.TotalBurn())
}

func TestBufferDrawFields(t *testing.T) {
	draw := types.BufferDraw{
		AggregateLiabilityNoah: math.LegacyNewDec(100),
		RedeemedLiabilityNoah:  math.LegacyNewDec(25),
		BufferPaid:             math.NewInt(10),
		ValuationComplete:      true,
	}

	require.Equal(t, math.LegacyNewDec(100), draw.AggregateLiabilityNoah)
	require.Equal(t, math.LegacyNewDec(25), draw.RedeemedLiabilityNoah)
	require.Equal(t, math.NewInt(10), draw.BufferPaid)
	require.True(t, draw.ValuationComplete)
}
