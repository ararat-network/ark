package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/x/market/types"
)

// applyConversionPolicy validates one already-authorized conversion candidate
// against live pool state and stores it.
//
// Both entry points converge here — governance unbounded, committee inside its
// corridor — so neither can reach the pool without the denomination guard, the
// delta rescale, and the effective-pool check. Depth is a claim about how much
// conversion the protocol absorbs before the spread widens, and the accumulated
// delta is measured against that depth, so a resize carries the delta with it:
// the gap keeps its proportion instead of silently meaning something new.
func (k Keeper) applyConversionPolicy(ctx context.Context, policy types.ConversionPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	current, err := k.ConversionPolicy.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting conversion policy: %w", err)
	}
	// The pool is denominated in the protocol reference, which x/oracle owns, so
	// its unit moves only through MsgSetReferenceDenom and RebaseBasePool. Accepting
	// a denomination here would let Market re-anchor behind the reference's
	// back and leave Treasury's cap expressed in a different unit.
	if policy.BasePool.Denom != current.BasePool.Denom {
		return sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"base pool denom is set by the protocol reference: re-point it with MsgSetReferenceDenom, not %s",
			policy.BasePool.Denom,
		)
	}

	oldDelta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting ark pool delta: %w", err)
	}
	newDelta, err := rescaleArkPoolDelta(oldDelta, current.BasePool.Amount, policy.BasePool.Amount)
	if err != nil {
		return err
	}
	if _, err := types.NewEffectivePools(policy.BasePool.Amount, newDelta); err != nil {
		return sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"invalid effective pools: %v",
			err,
		)
	}

	if err := k.ConversionPolicy.Set(ctx, policy); err != nil {
		return fmt.Errorf("setting conversion policy: %w", err)
	}
	if err := k.ArkPoolDelta.Set(ctx, newDelta); err != nil {
		return fmt.Errorf("setting ark pool delta: %w", err)
	}

	return sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventPoolUpdated{
		OldBasePoolDenom:  current.BasePool.Denom,
		OldBasePoolAmount: current.BasePool.Amount,
		NewBasePoolDenom:  policy.BasePool.Denom,
		NewBasePoolAmount: policy.BasePool.Amount,
		OldArkPoolDelta:   oldDelta,
		NewArkPoolDelta:   newDelta,
	})
}
