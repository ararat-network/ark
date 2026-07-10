package keeper

import (
	"context"

	arkmetrics "ark/pkg/metrics"
	"ark/x/market/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.EndBlock)()

	return k.ReplenishPools(ctx)
}
