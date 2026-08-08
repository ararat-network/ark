package keeper

import (
	"context"

	arkmetrics "ark/pkg/metrics"
	"ark/x/treasury/types"
)

// BeginBlocker refreshes derived tax caps when owed and advances the
// reward-funding window before Distribution consumes the previous block's fees.
// Reward funding asks the registry for pricing verdicts rather than reading the
// membership list, and owns its own genesis-height skip.
//
// It no longer values liability. Conversion settlement is the only consumer of
// the aggregate that runs every block, and it runs at the end of one, where the
// valuation it needs can be built from final state — so an idle block now folds
// the registry not at all, and a busy one folds it exactly once.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.BeginBlock)()

	if err := k.refreshTaxCaps(ctx); err != nil {
		return err
	}

	return k.advanceRewardFunding(ctx)
}
