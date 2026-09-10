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

// GetTaxCap derives reference cap * stored factor in base units. Zero reference means uncapped;
// positive sub-unit products become one. Missing factors and NOAH return collections.ErrNotFound,
// meaning untaxed to callers.
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

// deriveTaxCap shares read-time arithmetic between cap queries. An unrepresentable product uses the
// uncapped sentinel because its true ceiling exceeds the supported decimal range.
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
		// Clamp a positive sub-unit cap to one so rounding cannot turn a finite ceiling into the
		// zero uncapped sentinel or block a reference rebase.
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
