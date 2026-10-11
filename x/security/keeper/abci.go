package keeper

import (
	"context"
	"fmt"

	"github.com/ararat-network/ark/x/security/types"
)

// BeginBlocker reconciles the committee's plan record with the upgrade slot. x/upgrade has no
// hook, so a governance cancel, a governance overwrite, or the upgrade's own execution leaves the
// record behind, where a later governance plan reusing its name and height would read as the
// committee's. Clearing it here costs one record read a block, and the slot only while a record
// stands. A cancel and a re-schedule of the same plan inside one governance EndBlock stay
// indistinguishable from the committee's own plan until the next block.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	record, err := k.CommitteePlan.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting committee plan record: %w", err)
	}
	if record.IsZero() {
		return nil
	}
	pending, hasPending, err := k.pendingUpgradePlan(ctx)
	if err != nil {
		return err
	}
	if hasPending && record.Matches(pending.Name, pending.Height) {
		return nil
	}
	if err := k.CommitteePlan.Set(ctx, types.CommitteePlan{}); err != nil {
		return fmt.Errorf("clearing committee plan record: %w", err)
	}
	return nil
}
