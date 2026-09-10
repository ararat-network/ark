package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"

	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// RebaseReferenceState converts the tax cap, base-fee floor and live price, conversion factors, and
// exposure price anchor in the reference-change transaction. Every stored old-unit value must
// retain its meaning.
func (k Keeper) RebaseReferenceState(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	if params.ReferenceDenom != from {
		return errorsmod.Wrapf(
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
