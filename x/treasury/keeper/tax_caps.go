package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// refreshTaxCaps re-derives the caps when a refresh is owed, reading
// membership once so the caps derived are exactly the caps judged owed. A
// denom that leaves the oracle-priced set keeps the cap it was last derived
// with — its outstanding supply stays transferable and taxable — so an
// arrival is a refresh trigger and a departure is not. The pass is partial:
// it derives what this block's rates can serve, covers the rest, and the
// pending flag stays raised until a pass derives every member.
func (k Keeper) refreshTaxCaps(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	denoms, err := k.assetKeeper.OraclePricedDenoms(ctx)
	if err != nil {
		return fmt.Errorf("getting oracle-priced denominations: %w", err)
	}

	owed, err := k.taxCapRefreshOwed(ctx, params, denoms)
	if err != nil {
		return err
	}
	if !owed {
		return nil
	}

	caps, underived, err := k.buildTaxCaps(ctx, params, denoms)
	if err != nil {
		return fmt.Errorf("building tax caps: %w", err)
	}
	if len(underived) > 0 {
		k.Logger(ctx).Warn(
			"Treasury tax-cap refresh incomplete; retrying until rates return",
			"underived", underived,
		)
	}

	// Derived caps are upserted rather than replacing the stored set, so a cap
	// left behind by a departed member survives untouched.
	for _, cap := range caps {
		if err := k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
			return fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
		}
	}
	if err := k.TaxCapRefreshPending.Set(ctx, len(underived) > 0); err != nil {
		return fmt.Errorf("recording pending tax cap refresh: %w", err)
	}
	if len(caps) == 0 {
		return nil
	}
	// The event carries the denoms this pass wrote — derived and seeded — not
	// the whole stored set: kept caps are unchanged by definition, so
	// reporting them would describe an update that did not happen.
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTaxCapsUpdated{
		TaxCaps: caps,
	}); err != nil {
		return fmt.Errorf("emitting Treasury tax-cap update event: %w", err)
	}
	return nil
}

// taxCapRefreshOwed reports whether a refresh is owed this block. A cadence
// boundary is an instant rather than a state, so crossing one raises
// TaxCapRefreshPending durably before any derivation is attempted, and only a
// pass that derives every member lowers it. A member holding no cap owes a
// refresh immediately: the tax reads a missing entry as exemption, so
// deferring an arrival to the next boundary would leave it collecting nothing
// for a whole period.
func (k Keeper) taxCapRefreshOwed(ctx context.Context, params types.Params, denoms []string) (bool, error) {
	pending, err := k.TaxCapRefreshPending.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return false, fmt.Errorf("getting pending tax cap refresh: %w", err)
	}
	if pending {
		return true, nil
	}
	if chain.IsPeriodLastBlock(ctx, params.TaxCapRefreshPeriodBlocks) {
		if err := k.TaxCapRefreshPending.Set(ctx, true); err != nil {
			return false, fmt.Errorf("recording pending tax cap refresh: %w", err)
		}
		return true, nil
	}
	// Coverage asks containment of the members rather than equality with them:
	// a stored cap naming no member is the expected residue of a departure, so
	// this walks the membership and never the whole cap set, stopping at the
	// first member found holding nothing.
	for _, denom := range denoms {
		covered, err := k.TaxCaps.Has(ctx, denom)
		if err != nil {
			return false, fmt.Errorf("checking tax cap for %s: %w", denom, err)
		}
		if !covered {
			return true, nil
		}
	}

	return false, nil
}

// buildTaxCaps derives one cap per member from whichever rates this block can
// serve, and covers the members it cannot derive: one holding a cap keeps it
// (nothing is returned for it), one holding none is seeded at the unconverted
// reference amount, and the returned underived list names both so the caller
// keeps the refresh owed until it is empty. Seeding rather than skipping is
// what keeps an arrival taxable, because Market mints a member against its
// own feed alone, so coverage cannot wait for whichever rate the derivation
// is missing. An unrepresentable cross is covered exactly like a missing rate
// — it is a property of a rate pair redrawn every block — while any other
// failure is Treasury's own and fails the caller.
func (k Keeper) buildTaxCaps(ctx context.Context, params types.Params, denoms []string) ([]types.TaxCap, []string, error) {
	referenceTaxCap := params.ReferenceTaxCap

	caps := make([]types.TaxCap, 0, len(denoms))
	// A zero cap is the uncapped sentinel, not a zero ceiling: the tax clamp
	// only applies positive caps, so a zero reference lifts every member's
	// ceiling rather than zeroing its tax.
	if referenceTaxCap.IsZero() {
		for _, denom := range denoms {
			caps = append(caps, types.TaxCap{Denom: denom, TaxCap: math.ZeroInt()})
		}
		return caps, nil, nil
	}
	if len(denoms) == 0 {
		return caps, nil, nil
	}
	if len(denoms) == 1 && denoms[0] == referenceTaxCap.Denom {
		return []types.TaxCap{{Denom: referenceTaxCap.Denom, TaxCap: referenceTaxCap.Amount}}, nil, nil
	}

	capture := slices.Clone(denoms)
	if !slices.Contains(capture, referenceTaxCap.Denom) {
		capture = append(capture, referenceTaxCap.Denom)
	}
	rates, err := k.oracleKeeper.GetAvailableRateSet(ctx, capture...)
	if err != nil {
		return nil, nil, fmt.Errorf("capturing tax-cap rates: %w", err)
	}

	reference := sdk.NewDecCoinFromCoin(referenceTaxCap)
	var underived []string
	for _, denom := range denoms {
		if denom == reference.Denom {
			caps = append(caps, types.TaxCap{Denom: denom, TaxCap: referenceTaxCap.Amount})
			continue
		}
		converted, err := rates.Convert(reference, denom)
		switch {
		case err == nil:
			coin, _ := converted.TruncateDecimal()
			amount := coin.Amount
			// A sub-unit result floors at one rather than storing the zero it
			// truncates to, which would read as uncapped and lift the ceiling
			// a small reference cap was asking to tighten. Lopsided rates are
			// a legal steady state, so this is a degrade and never an error.
			if !amount.IsPositive() {
				amount = math.OneInt()
			}
			caps = append(caps, types.TaxCap{Denom: denom, TaxCap: amount})
			continue
		case !isUnusableRateInput(err):
			return nil, nil, fmt.Errorf("converting tax cap from %s to %s: %w", reference.Denom, denom, err)
		}

		underived = append(underived, denom)
		covered, err := k.TaxCaps.Has(ctx, denom)
		if err != nil {
			return nil, nil, fmt.Errorf("checking tax cap for %s: %w", denom, err)
		}
		if !covered {
			caps = append(caps, types.TaxCap{Denom: denom, TaxCap: referenceTaxCap.Amount})
		}
	}
	return caps, underived, nil
}

// isUnusableRateInput reports whether an error blames the rates a derivation
// was handed rather than Treasury's own state: missing, stale, invalid, or a
// cross that leaves the representable domain. All four are statements about
// this block's price inputs, so the affected member is covered and asked
// about again until rates return. It deliberately does not widen to
// arithmetic generally — a Treasury store or codec failure is not a price and
// must not be covered over.
func isUnusableRateInput(err error) bool {
	return errors.Is(err, oracletypes.ErrUnknownDenom) ||
		errors.Is(err, oracletypes.ErrStaleExchangeRate) ||
		errors.Is(err, oracletypes.ErrInvalidExchangeRate) ||
		errors.Is(err, oracletypes.ErrConversionOutOfRange)
}
