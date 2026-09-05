package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// GetTaxCap derives one denomination's tax cap from its stored conversion
// factor: ReferenceTaxCap × factor, truncated to base units. A zero reference
// is the uncapped sentinel and derives zero for every member; a positive
// reference whose product truncates below one floors at one rather than
// producing the zero that would read as uncapped, which would lift the
// ceiling a small reference cap was asking to tighten. A missing entry
// returns collections.ErrNotFound: the factor set minus the numeraire is the
// tax base, and absence means untaxed to the callers that own that judgement.
func (k Keeper) GetTaxCap(ctx context.Context, denom string) (math.Int, error) {
	if denom == chain.NoahBaseDenom {
		return math.Int{}, collections.ErrNotFound
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.Int{}, fmt.Errorf("getting params: %w", err)
	}
	return k.taxCap(ctx, params, denom)
}

// taxCap is GetTaxCap with the params read hoisted to the caller, so a
// transaction prices every cap through one read.
func (k Keeper) taxCap(ctx context.Context, params types.Params, denom string) (math.Int, error) {
	// NOAH's entry is fee-pricing state, never tax base: the numeraire is not
	// taxed, and this accessor is where that exclusion lives, so it hands the
	// callers the same verdict absence would.
	if denom == chain.NoahBaseDenom {
		return math.Int{}, collections.ErrNotFound
	}
	entry, err := k.ConversionFactors.Get(ctx, denom)
	if err != nil {
		return math.Int{}, err
	}
	return deriveTaxCap(params, entry), nil
}

// deriveTaxCap is the one place the cap arithmetic lives; GetTaxCap and the
// TaxCaps query both price through it. The product is checked because this
// runs at read time, where a lopsided factor meets whatever reference a later
// governance vote chose: a ceiling too large for the decimal domain is no
// ceiling, so an unrepresentable product derives the uncapped sentinel rather
// than panicking a read.
func deriveTaxCap(params types.Params, entry types.ConversionFactor) math.Int {
	if params.ReferenceTaxCap.IsZero() {
		return math.ZeroInt()
	}
	product, err := decimal.Mul(entry.Factor, math.LegacyNewDecFromInt(params.ReferenceTaxCap))
	if err != nil {
		return math.ZeroInt()
	}
	cap := product.TruncateInt()
	if !cap.IsPositive() {
		cap = math.OneInt()
	}
	return cap
}

// rescaleTaxCap re-expresses the reference tax cap in the new unit and emits
// the rebase event, returning the converted amount for the caller's single
// params write. Conversion failure fails the re-point: the handed set prices
// both legs by the caller's contract.
func (k Keeper) rescaleTaxCap(ctx context.Context, params types.Params, to string, rates oracletypes.RateSet) (math.Int, error) {
	oldCap := sdk.NewCoin(params.ReferenceDenom, params.ReferenceTaxCap)
	newCap := sdk.NewCoin(to, params.ReferenceTaxCap)
	if params.ReferenceTaxCap.IsPositive() {
		converted, err := rates.Convert(sdk.NewDecCoinFromCoin(oldCap), to)
		if err != nil {
			return math.Int{}, err
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
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: oldCap,
		NewCap: newCap,
	}); err != nil {
		return math.Int{}, fmt.Errorf("emitting Treasury reference tax cap rebase event: %w", err)
	}
	return newCap.Amount, nil
}
