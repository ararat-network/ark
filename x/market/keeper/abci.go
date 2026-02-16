package keeper

import (
	"context"
	"noah/x/market/types"

	"github.com/cosmos/cosmos-sdk/telemetry"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, telemetry.Now(), telemetry.MetricKeyEndBlocker) //nolint:staticcheck // TODO: switch to OpenTelemetry
	return k.ReplenishPools(ctx)
}
