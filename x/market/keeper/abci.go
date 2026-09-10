package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/x/market/types"
)

// EndBlocker settles accumulated conversions against final liability, then replenishes pools.
// Market must lead other EndBlock mutations. Incomplete valuation parks principal; state or
// arithmetic errors fail the block. See x/market/README.md.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer metrics.RecordModuleMethodLatency(ctx, types.ModuleName, metrics.EndBlock)()

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
