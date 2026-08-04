package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

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
		// A positive cap truncating to zero would silently become the uncapped
		// sentinel, and unlimited taxation by rounding accident is not a unit
		// change. Flooring at one base unit is the same degrade the derived
		// caps apply: the tightest finite ceiling, where refusing would wedge
		// the re-point on a rate pair no retry can mend.
		if !coin.Amount.IsPositive() {
			coin.Amount = math.OneInt()
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
