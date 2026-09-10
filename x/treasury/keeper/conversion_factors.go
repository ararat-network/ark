package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// refreshConversionFactors updates derivable member cross-rates each block, retains unavailable
// factors, and seeds uncovered members at one. This keeps outstanding member transfers taxable
// during feed outages.
func (k Keeper) refreshConversionFactors(ctx context.Context) error {
	denoms, err := k.assetKeeper.OraclePricedDenoms(ctx)
	if err != nil {
		return fmt.Errorf("getting oracle-priced denominations: %w", err)
	}
	if len(denoms) == 0 {
		return nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	reference := params.ReferenceDenom

	// The reference prices every cross's second leg without necessarily being
	// a member. A non-member reference is appended to the capture only:
	// appended to the membership instead, it would store a factor and enter
	// the tax base.
	capture := denoms
	if !slices.Contains(capture, reference) {
		capture = append(capture, reference)
	}
	rates, err := k.oracleKeeper.GetAvailableRateSet(ctx, capture...)
	if err != nil {
		return fmt.Errorf("capturing conversion-factor rates: %w", err)
	}

	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	one := sdk.NewDecCoin(reference, math.OneInt())
	changed := make([]types.ConversionFactor, 0, len(denoms))
	for _, denom := range denoms {
		var factor math.LegacyDec
		if denom == reference {
			factor = math.LegacyOneDec()
		} else {
			converted, err := rates.Convert(one, denom)
			if err != nil && !isUnusableRateInput(err) {
				return fmt.Errorf("deriving conversion factor for %s: %w", denom, err)
			}
			if err == nil && converted.Amount.IsPositive() {
				factor = converted.Amount
			} else {
				// Retain held factors when feeds or cross-rates are unavailable. Seed uncovered
				// members at one because their minting eligibility does not require the reference
				// rate needed for this cross.
				held, err := k.ConversionFactors.Has(ctx, denom)
				if err != nil {
					return fmt.Errorf("checking conversion factor for %s: %w", denom, err)
				}
				if held {
					continue
				}
				factor = math.LegacyOneDec()
			}
		}

		stored, err := k.ConversionFactors.Get(ctx, denom)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting conversion factor for %s: %w", denom, err)
		}
		if err != nil || !stored.Factor.Equal(factor) {
			entry := types.ConversionFactor{Denom: denom, Factor: factor, DerivedHeight: height}
			if err := k.ConversionFactors.Set(ctx, denom, entry); err != nil {
				return fmt.Errorf("setting conversion factor for %s: %w", denom, err)
			}
			changed = append(changed, entry)
		}
	}

	// Store NOAH units per reference unit alongside member factors, excluding NOAH from tax in
	// GetTaxCap. NOAH has no arrival seed; genesis supplies its initial factor until a live
	// reference rate refreshes it.
	converted, err := rates.Convert(one, chain.NoahBaseDenom)
	if err != nil && !isUnusableRateInput(err) {
		return fmt.Errorf("deriving the NOAH conversion factor: %w", err)
	}
	if err == nil && converted.Amount.IsPositive() {
		stored, err := k.ConversionFactors.Get(ctx, chain.NoahBaseDenom)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting the NOAH conversion factor: %w", err)
		}
		if err != nil || !stored.Factor.Equal(converted.Amount) {
			entry := types.ConversionFactor{
				Denom:         chain.NoahBaseDenom,
				Factor:        converted.Amount,
				DerivedHeight: height,
			}
			if err := k.ConversionFactors.Set(ctx, chain.NoahBaseDenom, entry); err != nil {
				return fmt.Errorf("setting the NOAH conversion factor: %w", err)
			}
			changed = append(changed, entry)
		}
	}

	if len(changed) == 0 {
		return nil
	}
	// Changed factors only: the pass runs every block, so re-deriving a factor
	// to the value it already held is the steady state and not news. The NOAH
	// entry, when present, is last.
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventConversionFactorsRefreshed{
		ConversionFactors: changed,
	}); err != nil {
		return fmt.Errorf("emitting Treasury conversion-factor refresh event: %w", err)
	}
	return nil
}

// rescaleConversionFactors converts stored crosses uniformly into new reference units. An unusable
// cross preserves the table; an overflowing product preserves only that entry. Fresh members
// recover on refresh, while dark entries retain their old-unit values. See x/treasury/README.md.
func (k Keeper) rescaleConversionFactors(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	// A per-reference figure rebases like the exposure anchor, not like a
	// quantity: the new unit is the offer (D76).
	cross, err := rates.Convert(sdk.NewDecCoin(to, math.OneInt()), from)
	if err != nil {
		k.Logger(ctx).Warn(
			"holding conversion factors across reference move",
			"from", from,
			"to", to,
			"error", err,
		)
		return nil
	}
	if !cross.Amount.IsPositive() {
		k.Logger(ctx).Warn(
			"holding conversion factors across reference move",
			"from", from,
			"to", to,
			"cross", cross.Amount,
		)
		return nil
	}

	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	rescaled := make([]types.ConversionFactor, 0)
	if err := k.ConversionFactors.Walk(ctx, nil, func(denom string, entry types.ConversionFactor) (bool, error) {
		// Positive, not merely representable — refresh's own write gate: an
		// overflowing product errors, a sub-precision one rounds to zero, and
		// a zero factor is a state genesis validation rightly refuses.
		factor, err := decimal.Mul(entry.Factor, cross.Amount)
		if err != nil || !factor.IsPositive() {
			k.Logger(ctx).Warn(
				"holding conversion factor across reference move",
				"from", from,
				"to", to,
				"denom", denom,
				"error", err,
			)
			return false, nil
		}
		entry.Factor = factor
		entry.DerivedHeight = height
		rescaled = append(rescaled, entry)
		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating conversion factors for rebase: %w", err)
	}
	for _, entry := range rescaled {
		if err := k.ConversionFactors.Set(ctx, entry.Denom, entry); err != nil {
			return fmt.Errorf("rescaling conversion factor %s: %w", entry.Denom, err)
		}
	}

	if len(rescaled) == 0 {
		return nil
	}
	// The rescale reports through the table's one stream, written entries
	// only in table order, as refresh does: refresh emits on change only,
	// and next block it re-derives to the values written here — so a
	// consumer missing this write would hold old-unit factors forever.
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventConversionFactorsRefreshed{
		ConversionFactors: rescaled,
	}); err != nil {
		return fmt.Errorf("emitting Treasury conversion-factor rescale event: %w", err)
	}
	return nil
}

// isUnusableRateInput reports whether a conversion failed on the block's rate
// inputs rather than on Treasury's own arithmetic.
func isUnusableRateInput(err error) bool {
	return errors.Is(err, oracletypes.ErrUnknownDenom) ||
		errors.Is(err, oracletypes.ErrStaleExchangeRate) ||
		errors.Is(err, oracletypes.ErrInvalidExchangeRate) ||
		errors.Is(err, oracletypes.ErrConversionOutOfRange)
}
