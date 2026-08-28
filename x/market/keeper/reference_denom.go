package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/decimal"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// RebaseBasePool re-denominates the virtual pool when governance re-points the
// protocol reference. It implements the oracle module's MarketReferenceDenomKeeper,
// and MsgSetReferenceDenom is its only caller.
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

	updated := capacity
	updated.BasePool = rebased
	if err := k.ConversionPolicy.Set(ctx, updated); err != nil {
		return fmt.Errorf("setting rebased conversion policy: %w", err)
	}
	if err := k.ArkPoolDelta.Set(ctx, newDelta); err != nil {
		return fmt.Errorf("setting rebased ArkPoolDelta: %w", err)
	}

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
