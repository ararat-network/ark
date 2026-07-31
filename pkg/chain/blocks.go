package chain

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	BlocksPerMinute = uint64(10)
	BlocksPerHour   = BlocksPerMinute * 60
	BlocksPerDay    = BlocksPerHour * 24
	BlocksPerWeek   = BlocksPerDay * 7
	BlocksPerMonth  = BlocksPerDay * 30
	BlocksPerYear   = BlocksPerDay * 365
)

// IsPeriodLastBlock returns true if we are at the last block of the period
func IsPeriodLastBlock(ctx context.Context, blocksPerPeriod uint64) bool {
	return IsPeriodLastBlockFrom(ctx, 0, blocksPerPeriod)
}

// IsPeriodLastBlockFrom returns true at the end of a period anchored at
// startHeight. Heights before the anchor are outside the period.
func IsPeriodLastBlockFrom(ctx context.Context, startHeight, blocksPerPeriod uint64) bool {
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height < 0 || uint64(height) < startHeight {
		return false
	}

	return (uint64(height)-startHeight+1)%blocksPerPeriod == 0
}

// LastPeriodBoundary returns the most recent height at or before height that
// closes a period anchored at zero — the heights IsPeriodLastBlock reports
// true for — or -1 when no period has closed yet.
//
// It exists for work that is due once per period but may fail on the boundary
// block itself. Recording the height of the last success and rebuilding
// whenever it falls behind this boundary retries every block until the work
// succeeds, which a bare IsPeriodLastBlock check cannot do: its boundary block
// passes once and does not come back.
//
// A zero period panics, matching IsPeriodLastBlockFrom.
func LastPeriodBoundary(height int64, blocksPerPeriod uint64) int64 {
	if height < 0 {
		return -1
	}

	closed := (uint64(height) + 1) / blocksPerPeriod
	if closed == 0 {
		return -1
	}

	return int64(closed*blocksPerPeriod - 1)
}
