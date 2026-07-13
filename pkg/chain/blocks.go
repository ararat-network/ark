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
