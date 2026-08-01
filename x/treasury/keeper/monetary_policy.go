package keeper

import (
	"context"
	"fmt"

	"ark/x/treasury/types"
)

// applyMonetaryPolicy validates one authorized candidate policy against live
// reward-funding capacity and stores it.
func (k *Keeper) applyMonetaryPolicy(ctx context.Context, policy types.MonetaryPolicy) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	funding, err := k.RewardFunding.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting reward funding state: %w", err)
	}
	if err := types.ValidateRewardTargetCapacity(params, funding, policy); err != nil {
		return err
	}
	if err := k.MonetaryPolicy.Set(ctx, policy); err != nil {
		return fmt.Errorf("setting monetary policy: %w", err)
	}
	return nil
}
