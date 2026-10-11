// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/treasury/abci.go.
// Modified for Ark: chain-specific block lifecycle.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"

	"github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/x/treasury/types"
)

// BeginBlocker refreshes exposure from PreBlock-applied rates. Liability valuation belongs to active
// conversion settlement in EndBlock, so this hook performs no liability fold.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer metrics.RecordModuleMethodLatency(ctx, types.ModuleName, metrics.BeginBlock)()

	// Cheap on every block that is not a cadence boundary: one bool read. The recomputation it gates
	// folds the registry, which is why it runs on a governed period rather than per block — the
	// samples it consumes are already being taken in settlement.
	return k.refreshExposure(ctx)
}

// EndBlocker accrues and settles reward funding from this block's fees, updates the base fee from
// its complete gas tally, then refreshes conversion factors from this block's rates. Both fee inputs
// are then fixed for the next block when this one commits, so its proposal check, recheck, and
// execution price a fee and its tax alike. Distribution consumes funded fees at the next block's
// start.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer metrics.RecordModuleMethodLatency(ctx, types.ModuleName, metrics.EndBlock)()

	if err := k.advanceRewardFunding(ctx); err != nil {
		return err
	}
	if err := k.updateBaseGasPrice(ctx); err != nil {
		return err
	}

	return k.refreshConversionFactors(ctx)
}
