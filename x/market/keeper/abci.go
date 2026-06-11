package keeper

import (
	"context"

	servicemetrics "noah/service/metrics"
	"noah/x/market/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer servicemetrics.RecordABCIMethodLatency(ctx, types.ModuleName, servicemetrics.EndBlock)()

	return k.ReplenishPools(ctx)
}
