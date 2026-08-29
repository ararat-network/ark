package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// refreshConversionFactors re-derives the factor table from whatever rates
// this block can serve, every block: a member with a servable rate is
// re-derived, one without keeps the factor it was last derived with — its
// outstanding supply stays transferable and taxable — and an uncovered
// arrival is seeded at one so it is taxable the block it is minted. Running
// every block is what retired the cadence machinery: every block is the
// retry, so no boundary, pending flag, or owed judgement exists.
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
	reference := params.ReferenceTaxCap.Denom

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
				// No servable rate this block — a dark feed, or a cross the
				// block's rates cannot represent, which is a property of a
				// rate pair redrawn every block and never Treasury's error. A
				// held factor is kept untouched; an uncovered arrival is
				// seeded at one, because Market mints a member against its
				// own feed alone, so coverage cannot wait for whichever rate
				// this derivation is missing.
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
		if err == nil && stored.Factor.Equal(factor) {
			continue
		}
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting conversion factor for %s: %w", denom, err)
		}
		entry := types.ConversionFactor{Denom: denom, Factor: factor, DerivedHeight: height}
		if err := k.ConversionFactors.Set(ctx, denom, entry); err != nil {
			return fmt.Errorf("setting conversion factor for %s: %w", denom, err)
		}
		changed = append(changed, entry)
	}

	if len(changed) == 0 {
		return nil
	}
	// Changed factors only: the pass runs every block, so re-deriving a factor
	// to the value it already held is the steady state and not news.
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventConversionFactorsRefreshed{
		ConversionFactors: changed,
	}); err != nil {
		return fmt.Errorf("emitting Treasury conversion-factor refresh event: %w", err)
	}
	return nil
}

// rescaleConversionFactors re-expresses every stored factor in the new
// reference unit. A factor is member units per one reference unit, so the
// whole table rescales by a single cross — old-reference units per
// new-reference unit — rather than per-member rates: the next block's refresh
// re-derives every servable member from live rates anyway, and the uniform
// rescale is what keeps a dark member's kept factor meaning what it meant,
// in the new unit, until its feed returns.
//
// An unservable cross keeps the table untouched: the same degrade class as
// the cap floor in RebaseTaxCap, chosen over wedging the re-point on a rate
// pair no retry can mend. Servable members mend next block; dark ones stay
// priced in the old unit until their feed returns, which the derived cap
// inherits.
func (k Keeper) rescaleConversionFactors(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	cross, err := rates.Convert(sdk.NewDecCoin(to, math.OneInt()), from)
	if err != nil {
		return nil
	}
	if !cross.Amount.IsPositive() {
		return nil
	}

	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	rescaled := make([]types.ConversionFactor, 0)
	if err := k.ConversionFactors.Walk(ctx, nil, func(_ string, entry types.ConversionFactor) (bool, error) {
		entry.Factor = entry.Factor.Mul(cross.Amount)
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
