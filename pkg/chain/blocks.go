// SPDX-License-Identifier: Apache-2.0
// Adapted from Terra Classic, types/util/blocks.go.
// Modified for Ark: context handling and anchored block periods.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package chain

import (
	"context"
	"math"

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

// MaxBlockSeconds is the last block time the header's int64 carries: a chain instant past it is one
// no block reaches.
const MaxBlockSeconds = uint64(math.MaxInt64)

// BlockSeconds is the block's Unix time in whole seconds. A time before the epoch, which only a test
// harness produces, reads as the epoch.
func BlockSeconds(ctx context.Context) uint64 {
	return uint64(max(sdk.UnwrapSDKContext(ctx).BlockTime().Unix(), 0))
}

// BlockHeight is the block's height. No header carries a negative one; the clamp keeps the
// conversion total.
func BlockHeight(ctx context.Context) uint64 {
	return uint64(max(sdk.UnwrapSDKContext(ctx).BlockHeight(), 0))
}

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
