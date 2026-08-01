package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// RebaseTaxCap re-expresses the reference tax cap when governance re-points
// the protocol reference.
func (k Keeper) RebaseTaxCap(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	oldCap := params.ReferenceTaxCap
	if oldCap.Denom != from {
		return sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"reference tax cap is denominated in %s, not %s",
			oldCap.Denom,
			from,
		)
	}
	if from == to {
		return nil
	}

	newCap := sdk.NewCoin(to, oldCap.Amount)
	if oldCap.Amount.IsPositive() {
		converted, err := rates.Convert(sdk.NewDecCoinFromCoin(oldCap), to)
		if err != nil {
			return err
		}
		coin, _ := converted.TruncateDecimal()
		// A positive cap truncating to zero would silently delete the finite
		// ceiling: zero is the explicit uncapped sentinel, and unlimited
		// taxation by rounding accident is not a unit change.
		if !coin.Amount.IsPositive() {
			return sdkerrors.Wrapf(
				oracletypes.ErrConversionOutOfRange,
				"converting positive reference tax cap from %s to %s truncated to zero",
				from,
				to,
			)
		}
		newCap = coin
	}

	params.ReferenceTaxCap = newCap
	if err := k.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("setting rebased params: %w", err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: oldCap,
		NewCap: newCap,
	}); err != nil {
		return fmt.Errorf("emitting Treasury reference tax cap rebase event: %w", err)
	}

	return nil
}
