package keeper

import (
	"context"
	"fmt"

	arkmetrics "ark/pkg/metrics"
	"ark/x/treasury/types"
)

// BeginBlocker primes the block's liability valuation, refreshes derived tax
// caps when required, and advances the reward-funding window before
// Distribution consumes the previous block's fees.
//
// Priming runs first, and unconditionally. It needs this block's prices, feed
// set, and lifecycle statuses to be final, which every PreBlocker has settled
// before any BeginBlocker runs — the ABCI lifecycle supplies that ordering, so
// no call site has to maintain it. It stays outside the height gate below
// because the first block values liability exactly as every later one does.
//
// Only the cap refresh reads the membership list. Reward funding asks the
// registry for pricing verdicts instead, so it never sees a membership
// snapshot it would have to keep honest. It also owns its own genesis-height
// skip, so the hook states the block's work without restating either module's
// accrual rules.
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
