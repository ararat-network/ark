package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
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

	// The numeraire's own cross — NOAH units per reference unit — derives in
	// the same pass into the same table; GetTaxCap excludes it from the tax
	// base by denomination. It has no arrival seed: a member is minted
	// against its own feed and must be taxable the block it arrives, while
	// nothing forces gas to be paid in NOAH, so before the first servable
	// reference rate the entry simply does not exist and the fee-denom gate
	// refuses NOAH rather than mispricing it.
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

// rescaleConversionFactors re-expresses every stored factor in the new
// reference unit. A factor is member units per one reference unit, so the
// whole table rescales by a single cross — old-reference units per
// new-reference unit — rather than per-member rates: the next block's refresh
// re-derives every servable member from live rates anyway, and the uniform
// rescale is what keeps a dark member's kept factor meaning what it meant,
// in the new unit, until its feed returns.
//
// An unservable cross keeps the table untouched: the same degrade class as
// the cap floor in RebaseReferenceState, chosen over wedging the re-point on
// a rate pair no retry can mend. A product past the Dec domain holds that
// entry alone — refresh's own degrade for the same figure — since a dark
// member that fits has only this rescale to move it. Servable members mend
// next block; dark ones stay priced in the old unit until their feed
// returns, which the derived cap inherits.
func (k Keeper) rescaleConversionFactors(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
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
