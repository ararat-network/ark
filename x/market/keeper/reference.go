package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/pkg/decimal"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

// RebaseBasePool re-denominates the virtual pool when governance re-points the
// protocol reference. It implements the asset module's MarketReferenceKeeper,
// and MsgSetReference is its only caller.
//
// The pool's depth is a claim about how much conversion the protocol will
// absorb before the spread widens, and that claim is expressed in reference
// units — so moving the reference has to carry the depth across at the current
// rate rather than leave a number that now means something else. The delta
// scales with it so the effective pools keep their ratio: a re-denomination is
// a change of unit, not a change of monetary stance.
//
// The rates are handed in by x/asset, which reads the pair once for both
// executors — the incoming fresh, the outgoing raw at any stored age — so
// Market never decides freshness policy for a governance action it does not
// own.
//
// x/asset owns the destination, so a `from` that disagrees with Market's own
// pool denomination means the two modules disagree about what unit the pool is
// in. That halts the proposal rather than silently re-anchoring: the wrong
// answer here mis-sizes every subsequent swap.
func (k Keeper) RebaseBasePool(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	capacity, err := k.ConversionPolicy.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting conversion policy: %w", err)
	}
	if capacity.BasePool.Denom != from {
		return sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"base pool is denominated in %s, not %s",
			capacity.BasePool.Denom,
			from,
		)
	}
	if from == to {
		return nil
	}
	oldDelta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting ArkPoolDelta: %w", err)
	}

	rebased, err := rates.Convert(capacity.BasePool, to)
	if err != nil {
		return marketRateError(err)
	}

	newDelta, err := rescaleArkPoolDelta(oldDelta, capacity.BasePool.Amount, rebased.Amount)
	if err != nil {
		return err
	}
	if _, err := types.NewEffectivePools(rebased.Amount, newDelta); err != nil {
		return sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"invalid effective pools after rebasing to %s: %v",
			to,
			err,
		)
	}

	// The conversion mandate is deliberately untouched. Its corridor is
	// denominated in the unit governance approved it in, so a rebase strands the
	// appointment until governance re-appoints with bounds in the new unit:
	// converting a delegation at one instant's rate would produce bounds no
	// proposal ever contained.
	updated := capacity
	updated.BasePool = rebased
	if err := k.ConversionPolicy.Set(ctx, updated); err != nil {
		return fmt.Errorf("setting rebased conversion policy: %w", err)
	}
	if err := k.ArkPoolDelta.Set(ctx, newDelta); err != nil {
		return fmt.Errorf("setting rebased ArkPoolDelta: %w", err)
	}
	// Whether this re-point stranded the conversion mandate is deliberately not
	// reported from here. Governance re-points and re-appoints in one proposal,
	// and this executor runs while that proposal is only half applied — the
	// re-appointment is a later message in the same transaction — so every
	// predicate available at this point calls the correct path stranded. A
	// warning that fires on the intended flow is the one that gets ignored on
	// the flow that matters. ConversionMandate answers the question instead, from
	// a vantage point where the whole proposal has landed.
	return sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventPoolUpdated{
		OldBasePoolDenom:  capacity.BasePool.Denom,
		OldBasePoolAmount: capacity.BasePool.Amount,
		NewBasePoolDenom:  rebased.Denom,
		NewBasePoolAmount: rebased.Amount,
		OldArkPoolDelta:   oldDelta,
		NewArkPoolDelta:   newDelta,
	})
}

// rescaleArkPoolDelta carries the signed pool gap across a depth change so the
// effective pools keep their ratio. A zero old depth cannot be scaled from and
// is rejected by params validation before this is reached.
func rescaleArkPoolDelta(delta, oldDepth, newDepth math.LegacyDec) (math.LegacyDec, error) {
	if newDepth.Equal(oldDepth) {
		return delta, nil
	}
	scaled, err := decimal.Mul(delta, newDepth)
	if err != nil {
		return math.LegacyDec{}, sdkerrors.Wrapf(
			types.ErrArithmeticOutOfRange,
			"rescaling ark pool delta: %v",
			err,
		)
	}
	rescaled, err := decimal.Quo(scaled, oldDepth)
	if err != nil {
		return math.LegacyDec{}, sdkerrors.Wrapf(
			types.ErrArithmeticOutOfRange,
			"rescaling ark pool delta: %v",
			err,
		)
	}

	return rescaled, nil
}
