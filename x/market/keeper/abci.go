package keeper

import (
	"context"

	noahmetrics "noah/pkg/metrics"
	"noah/x/market/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer noahmetrics.RecordABCIMethodLatency(ctx, types.ModuleName, noahmetrics.EndBlock)()

	return k.ReplenishPools(ctx)
}
