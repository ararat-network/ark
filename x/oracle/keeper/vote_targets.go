package keeper

import (
	"context"
	"fmt"
	"slices"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

// ScheduleVoteTargets stages one target epoch for a future ExtendVote height.
func (k Keeper) ScheduleVoteTargets(ctx context.Context, denoms []string) error {
	voteTargets, err := k.VoteTargets.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting vote targets: %w", err)
	}

	denoms = slices.Clone(denoms)
	slices.Sort(denoms)
	if voteTargets.Pending != nil {
		if slices.Equal(voteTargets.Pending.Denoms, denoms) {
			return nil
		}
		return fmt.Errorf(
			"vote-target transition to version %d is already pending activation at height %d",
			voteTargets.Pending.Version,
			voteTargets.Pending.ActivationVoteHeight,
		)
	}
	if slices.Equal(voteTargets.Denoms, denoms) {
		return nil
	}

	voteTargets.Pending = &types.PendingVoteTargets{
		Denoms:               denoms,
		Version:              voteTargets.Version + 1,
		ActivationVoteHeight: sdk.UnwrapSDKContext(ctx).BlockHeight() + types.VoteTargetActivationDelayBlocks,
	}
	if err := voteTargets.Validate(); err != nil {
		return fmt.Errorf("validating scheduled vote targets: %w", err)
	}
	if err := k.VoteTargets.Set(ctx, voteTargets); err != nil {
		return fmt.Errorf("scheduling vote targets: %w", err)
	}

	return nil
}

// AdvanceVoteTargets activates a due target epoch and prunes exchange rates
// that were needed only by the previous epoch.
func (k Keeper) AdvanceVoteTargets(ctx context.Context) error {
	voteTargets, err := k.VoteTargets.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting vote targets: %w", err)
	}
	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if voteTargets.Pending == nil || blockHeight < voteTargets.Pending.ActivationVoteHeight {
		return nil
	}

	for _, denom := range voteTargets.Denoms {
		if _, found := slices.BinarySearch(voteTargets.Pending.Denoms, denom); found {
			continue
		}
		if err := k.ExchangeRate.Remove(ctx, denom); err != nil {
			return fmt.Errorf("removing exchange rate for vote target %s: %w", denom, err)
		}
	}

	voteTargets.Denoms = voteTargets.Pending.Denoms
	voteTargets.Version = voteTargets.Pending.Version
	voteTargets.Pending = nil
	if err := k.VoteTargets.Set(ctx, voteTargets); err != nil {
		return fmt.Errorf("advancing vote targets: %w", err)
	}

	return nil
}
