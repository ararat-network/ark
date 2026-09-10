package keeper

import (
	"context"

	"github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/x/treasury/types"
)

// BeginBlocker refreshes conversion factors from PreBlock-applied rates. Liability valuation
// belongs to active conversion settlement in EndBlock, so this hook performs no liability fold.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer metrics.RecordModuleMethodLatency(ctx, types.ModuleName, metrics.BeginBlock)()

	if err := k.refreshConversionFactors(ctx); err != nil {
		return err
	}

	// Last, and cheap on every block that is not a cadence boundary: one bool
	// read. The recomputation it gates folds the registry, which is why it runs
	// on a governed period rather than per block — the samples it consumes are
	// already being taken in settlement.
	return k.refreshExposure(ctx)
}

// EndBlocker accrues and settles reward funding from this block's fees, then updates the base fee
// from its complete gas tally. Distribution consumes funded fees at the next block's start.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer metrics.RecordModuleMethodLatency(ctx, types.ModuleName, metrics.EndBlock)()

	if err := k.advanceRewardFunding(ctx); err != nil {
		return err
	}

	return k.updateBaseGasPrice(ctx)
}
