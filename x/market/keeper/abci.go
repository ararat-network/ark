package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	arkmetrics "ark/pkg/metrics"
	"ark/x/market/types"
)

// EndBlocker settles the block's conversions and then replenishes the pools.
//
// Settlement runs first and runs here rather than inside each conversion: every
// mint and burn the block performed has landed, so Treasury values liability
// once against final state and places the whole block's principal against that
// one figure. Market leads the EndBlock order for the same reason the ordering
// note in app config gives — every later actor must see settled funds.
//
// An error fails the block. There is no transaction left to abort by the time
// settlement runs, and every condition a conversion could cause was refused when
// it was recorded, so what remains is state corruption or an arithmetic
// impossibility. A valuation that could not cover every recognised liability is
// not one of them: Treasury parks the principal and discloses, and the block
// continues.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.EndBlock)()

	totals, err := k.conversionTotals(ctx)
	if err != nil {
		return fmt.Errorf("reading the block's conversion totals: %w", err)
	}
	// A block that converted nothing is handed over rather than filtered here:
	// SettleConversions returns on zero totals before it values anything, so an
	// idle chain still folds the registry at no point.
	burn, err := k.treasuryKeeper.SettleConversions(ctx, totals)
	if err != nil {
		return fmt.Errorf("settling the block's conversions: %w", err)
	}
	// The figure is not re-validated. SettleConversions accumulates it from
	// zero over an overflow that is a remainder of non-negative subtractions
	// and a draw capped by the output it funds, and returns an error rather
	// than a figure on every path that could leave it unset.
	if burn.IsPositive() {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, chain.NoahCoins(burn)); err != nil {
			return fmt.Errorf("burning settled conversions %s: %w", sdk.NewCoins(chain.NoahCoin(burn)), err)
		}
	}

	return k.ReplenishPools(ctx)
}
