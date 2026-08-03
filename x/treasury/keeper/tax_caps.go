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

// refreshTaxCaps rebuilds the derived caps when a rebuild is owed. Membership
// is read once and feeds both the trigger and the rebuild, so the caps derived
// are exactly the caps judged owed. Preblock lifecycle completions run before
// BeginBlock, so an asset activated this block is a member in this block's
// refresh.
//
// Coverage is containment, not equality. A denom that leaves the oracle-priced
// set keeps the cap it was last derived with, because its outstanding supply stays
// transferable and taxable — retirement itself can leave a residual — and its
// feed may never return to re-derive one. An arrival is therefore a rebuild
// trigger and a departure is not.
//
// A rebuild that cannot value its members skips rather than failing the block:
// the triggers survive a skip, so the refresh retries every block until rates
// return. Params are read before anything else because the cadence is one of
// them.
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

	caps, err := k.buildTaxCaps(ctx, params, denoms)
	if err != nil {
		if !isUnusableRateInput(err) {
			return fmt.Errorf("building tax caps: %w", err)
		}

		k.Logger(ctx).Warn("skipping Treasury tax-cap refresh", "error", err)
		return nil
	}

	// Derived caps are upserted rather than replacing the stored set, so a cap
	// left behind by a departed member survives untouched.
	for _, cap := range caps {
		if err := k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
			return fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
		}
	}
	// This rebuild derived every member from current inputs, which is exactly
	// what an outstanding cadence refresh was owed.
	if err := k.TaxCapRefreshPending.Set(ctx, false); err != nil {
		return fmt.Errorf("clearing pending tax cap refresh: %w", err)
	}
	// The event carries the denoms this rebuild derived, not the whole stored
	// set: kept caps are unchanged by definition, so reporting them would
	// describe an update that did not happen.
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTaxCapsUpdated{
		TaxCaps: caps,
	}); err != nil {
		return fmt.Errorf("emitting Treasury tax-cap update event: %w", err)
	}
	return nil
}

// taxCapRefreshOwed reports whether a rebuild is owed this block, recording
// the cadence edge durably as it goes. The cadence boundary is an instant
// rather than a state, so crossing one raises TaxCapRefreshPending before any
// rebuild is attempted — once this block commits, the work is owed in state
// even if every rebuild until rates return skips. Only a rebuild that reaches
// its cap writes lowers the flag.
//
// The membership trigger is not an eager cadence: a member holding no cap is
// untaxed rather than capped stale, because the tax reads a missing entry as
// exemption, so deferring an arrival to the next boundary would leave a fresh
// denomination collecting nothing for a whole period. It needs no flag of its
// own either — the registry is re-read every block, so a gap left open by a
// skipped rebuild re-asks on the next one. It reads ground truth rather than a
// version consumers must be told to bump, so nothing has to announce a move.
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

// buildTaxCaps derives one cap per given oracle-priced denomination from the
// reference cap. The reference unit is the protocol reference by invariant —
// genesis pins it, UpdateParams refuses to move it, and RebaseTaxCap is the
// only denom-moving path — so it is not re-checked here. It need not itself be
// a member: rates exist for any feed, so the cap converts into member units
// whether or not an asset is listed under the reference denomination.
func (k Keeper) buildTaxCaps(ctx context.Context, params types.Params, denoms []string) ([]types.TaxCap, error) {
	referenceTaxCap := params.ReferenceTaxCap

	caps := make([]types.TaxCap, 0, len(denoms))
	// A zero cap is the uncapped sentinel, not a zero ceiling: the tax clamp
	// only applies positive caps, so a zero reference lifts every member's
	// ceiling rather than zeroing its tax.
	if referenceTaxCap.IsZero() {
		for _, denom := range denoms {
			caps = append(caps, types.TaxCap{Denom: denom, TaxCap: math.ZeroInt()})
		}
		return caps, nil
	}
	if len(denoms) == 0 {
		return caps, nil
	}
	if len(denoms) == 1 && denoms[0] == referenceTaxCap.Denom {
		return []types.TaxCap{{Denom: referenceTaxCap.Denom, TaxCap: referenceTaxCap.Amount}}, nil
	}

	capture := slices.Clone(denoms)
	if !slices.Contains(capture, referenceTaxCap.Denom) {
		capture = append(capture, referenceTaxCap.Denom)
	}
	rates, err := k.oracleKeeper.GetRateSet(ctx, capture...)
	if err != nil {
		return nil, fmt.Errorf("capturing tax-cap rates: %w", err)
	}

	reference := sdk.NewDecCoinFromCoin(referenceTaxCap)
	for _, denom := range denoms {
		if denom == reference.Denom {
			caps = append(caps, types.TaxCap{Denom: denom, TaxCap: referenceTaxCap.Amount})
			continue
		}
		converted, err := rates.Convert(reference, denom)
		if err != nil {
			return nil, fmt.Errorf("converting tax cap from %s to %s: %w", reference.Denom, denom, err)
		}
		coin, _ := converted.TruncateDecimal()
		amount := coin.Amount
		if !amount.IsPositive() {
			amount = math.OneInt()
		}
		caps = append(caps, types.TaxCap{Denom: denom, TaxCap: amount})
	}
	return caps, nil
}

// isUnusableRateInput reports whether an error blames the rates a rebuild was
// handed rather than Treasury's own state. Prices can be unusable in four ways
// — missing, stale, invalid, or a pair whose cross leaves the representable
// domain — and all four are statements about this block's price inputs, not
// about anything Treasury holds.
//
// Representability belongs here because of what the cap conversion multiplies:
// a governance-set reference amount by one rate over another. An out-of-range
// result is therefore a property of the rate pair, which is redrawn every
// block, and not of any quantity the protocol minted. That is the whole of the
// difference from the liability scan, which multiplies supply the protocol
// issued and treats the same overflow as fatal — there the number is ours, so
// exceeding the domain is our bug; here the numbers arrive from consensus
// pricing and the honest response is to keep the previous map and ask again.
//
// Keeping it deliberately does not widen to arithmetic generally: a Treasury
// store or codec failure is not a price and must not be skipped.
func isUnusableRateInput(err error) bool {
	return errors.Is(err, oracletypes.ErrUnknownDenom) ||
		errors.Is(err, oracletypes.ErrStaleExchangeRate) ||
		errors.Is(err, oracletypes.ErrInvalidExchangeRate) ||
		errors.Is(err, oracletypes.ErrConversionOutOfRange)
}
