package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// RebaseReferenceState re-expresses Treasury's reference-denominated state when
// governance re-points the protocol reference: the tax cap, the base-fee floor
// and live gas price, the conversion factors, and the exposure model's price
// anchor.
//
// Each travels with the cap because all are figures quoted in the old
// unit that keep their meaning only if converted in the same transaction the
// unit changes. Leaving it would make the next block read a new-unit price
// against an old-unit anchor and record the cross rate as a market move.
func (k Keeper) RebaseReferenceState(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	if params.ReferenceDenom != from {
		return sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"treasury reference denom is %s, not %s",
			params.ReferenceDenom,
			from,
		)
	}
	if from == to {
		return nil
	}

	// Each reference-quoted figure converts through its own helper; the two
	// params figures come back so the write below stays the only one.
	newCap, err := k.rescaleTaxCap(ctx, params, to, rates)
	if err != nil {
		return err
	}
	minGasPrice, err := k.rescaleBaseFee(ctx, params, to, rates)
	if err != nil {
		return err
	}

	params.ReferenceDenom = to
	params.ReferenceTaxCap = newCap
	params.MinBaseGasPrice = minGasPrice
	if err := k.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("setting rebased params: %w", err)
	}

	if err := k.rescaleConversionFactors(ctx, from, to, rates); err != nil {
		return err
	}

	return k.rescaleReferencePrice(ctx, from, to, rates)
}
