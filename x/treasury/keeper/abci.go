package keeper

import (
	"context"
	"fmt"

	arkmetrics "ark/pkg/metrics"
	"ark/x/treasury/types"
)

// BeginBlocker primes the block's liability valuation, refreshes derived tax
// caps when owed, and advances the reward-funding window before Distribution
// consumes the previous block's fees. Priming runs first and unconditionally:
// it needs this block's prices, feed set, and lifecycle statuses to be final,
// which every PreBlocker has settled before any BeginBlocker runs. Reward
// funding asks the registry for pricing verdicts rather than reading the
// membership list, and owns its own genesis-height skip.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.BeginBlock)()

	if err := k.PrimeLiabilitySnapshot(ctx); err != nil {
		return fmt.Errorf("priming liability snapshot: %w", err)
	}

	if err := k.refreshTaxCaps(ctx); err != nil {
		return err
	}

	return k.advanceRewardFunding(ctx)
}
