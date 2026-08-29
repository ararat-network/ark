package keeper

import (
	"context"

	arkmetrics "github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/x/treasury/types"
)

// BeginBlocker refreshes the conversion-factor table from this block's rates,
// so the block's transactions tax under the rates its own vote extensions
// applied.
//
// It no longer values liability. Conversion settlement is the only consumer of
// the aggregate that runs every block, and it runs at the end of one, where the
// valuation it needs can be built from final state — so an idle block now folds
// the registry not at all, and a busy one folds it exactly once.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.BeginBlock)()

	if err := k.refreshConversionFactors(ctx); err != nil {
		return err
	}

	// Last, and cheap on every block that is not a cadence boundary: one bool
	// read. The recomputation it gates folds the registry, which is why it runs
	// on a governed period rather than per block — the samples it consumes are
	// already being taken in settlement.
	return k.refreshExposure(ctx)
}

// EndBlocker advances the reward-funding window and runs the base-fee
// controller's update. Both are end-of-block reads by construction: the fee
// collector holds this block's own fees here — Distribution sweeps it at the
// next block's start, allocating any settlement top-up with them — so each
// block's accrual values the fees it earned; and the gas tally the controller
// reads is written by the ante as the block's transactions execute and clears
// at commit, so no other hook ever sees it complete.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.EndBlock)()

	if err := k.advanceRewardFunding(ctx); err != nil {
		return err
	}

	return k.updateBaseGasPrice(ctx)
}
