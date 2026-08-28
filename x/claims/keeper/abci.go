package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	arkmetrics "github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/x/claims/types"
)

// EndBlocker settles every claim whose cancellation period closed at or before
// this height.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.EndBlock)()

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := uint64(sdkCtx.BlockHeight())
	iterator, err := k.DueClaims.Iterate(ctx, collections.NewPrefixUntilPairRange[uint64, uint64](height))
	if err != nil {
		return fmt.Errorf("iterating due claims: %w", err)
	}
	defer iterator.Close()

	due, err := iterator.Keys()
	if err != nil {
		return fmt.Errorf("reading due claim keys: %w", err)
	}

	for _, key := range due {
		if err := k.DueClaims.Remove(ctx, key); err != nil {
			return fmt.Errorf("clearing settlement index for claim %d: %w", key.K2(), err)
		}
		claim, err := k.Claims.Get(ctx, key.K2())
		if err != nil {
			return fmt.Errorf("getting claim %d: %w", key.K2(), err)
		}
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
