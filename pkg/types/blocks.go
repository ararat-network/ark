package types

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
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return ((uint64)(sdkCtx.BlockHeight())+1)%blocksPerPeriod == 0
}
