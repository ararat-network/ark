package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/x/claims/types"
)

// EndBlocker settles claims whose cancellation period closed at or before this block's time, oldest
// first, at most MaxSettlementsPerBlock in a block; the rest stay due for the next.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	defer metrics.RecordModuleMethodLatency(ctx, types.ModuleName, metrics.EndBlock)()

	iterator, err := k.DueClaims.Iterate(ctx, collections.NewPrefixUntilPairRange[uint64, uint64](chain.BlockSeconds(ctx)))
	if err != nil {
		return fmt.Errorf("iterating due claims: %w", err)
	}
	var due []collections.Pair[uint64, uint64]
	for ; iterator.Valid() && len(due) < types.MaxSettlementsPerBlock; iterator.Next() {
		key, err := iterator.Key()
		if err != nil {
			iterator.Close()
			return fmt.Errorf("reading due claim key: %w", err)
		}
		due = append(due, key)
	}
	iterator.Close()

	for _, key := range due {
		if err := k.DueClaims.Remove(ctx, key); err != nil {
			return fmt.Errorf("clearing settlement index for claim %d: %w", key.K2(), err)
		}
		claim, err := k.Claims.Get(ctx, key.K2())
		if err != nil {
			return fmt.Errorf("getting claim %d: %w", key.K2(), err)
		}
		// Unreachable: every status change away from pending removes the key in the same write, and
		// import indexes only pending claims. Kept because settling a settled claim pays it twice.
		if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
			k.Logger(ctx).Error(
				"dropping settlement queue entry for an already-settled claim",
				"claim_id", claim.ClaimId,
				"status", claim.Status,
			)
			continue
		}

		if err := k.settleClaim(ctx, claim); err != nil {
			return fmt.Errorf("settling claim %d: %w", claim.ClaimId, err)
		}
	}

	return nil
}
