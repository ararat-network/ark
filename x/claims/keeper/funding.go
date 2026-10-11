package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// funding is what a claim drew from uncommitted Insurance holdings: custody in its denomination, and
// the conversion that bought the rest, nil when custody covered it.
type funding struct {
	custody    math.Int
	conversion *conversion
}

// conversion is what a stablecoin claim bought: the coins and the NOAH they cost.
type conversion struct {
	offer  math.Int
	output sdk.Coin
}

// valuation prices a committee claim in the reference unit at the fresh rates Market quotes with,
// read once for the face value before the buy and the charge after it.
type valuation struct {
	reference string
	rates     oracletypes.RateSet
}

// validateDenom admits NOAH and any active registry asset as a claim's denomination.
func (k *Keeper) validateDenom(ctx context.Context, denom string) error {
	if denom == chain.NoahBaseDenom {
		return nil
	}
	asset, err := k.assetKeeper.GetAsset(ctx, denom)
	if err != nil {
		return fmt.Errorf("claim asset: %w", err)
	}
	if asset.Status != assettypes.AssetStatus_ASSET_STATUS_ACTIVE {
		return errors.New("a new claim requires an active asset")
	}
	return nil
}

// fund backs a stablecoin claim before it is reserved: uncommitted custody in its denomination
// first, then a buy of the shortfall through Market with Insurance NOAH, paying at most maxOffer.
// Market delivers at least the shortfall, sometimes a few base units more, which stay uncommitted.
// A NOAH claim draws its amount, whose coverage reserve checks. The message is atomic, so an offer
// Insurance cannot cover reverts the buy.
func (k *Keeper) fund(ctx context.Context, amount sdk.Coin, maxOffer math.Int) (funding, error) {
	if maxOffer.IsNil() {
		maxOffer = math.ZeroInt()
	}
	if err := chain.ValidateAmount(maxOffer, false); err != nil {
		return funding{}, fmt.Errorf("max_offer: %w", err)
	}
	if amount.Denom == chain.NoahBaseDenom {
		if !maxOffer.IsZero() {
			return funding{}, errors.New("a NOAH claim converts nothing")
		}
		return funding{custody: amount.Amount}, nil
	}
	custody, err := k.Uncommitted(ctx, amount.Denom)
	if err != nil {
		return funding{}, err
	}
	if !custody.LT(amount.Amount) {
		return funding{custody: amount.Amount}, nil
	}
	shortfall := sdk.NewCoin(amount.Denom, amount.Amount.Sub(custody))
	if maxOffer.IsZero() {
		return funding{}, fmt.Errorf("custody holds %s of %s; set max_offer to convert the rest", sdk.NewCoin(amount.Denom, custody), amount)
	}
	if k.marketKeeper == nil {
		return funding{}, errors.New("claims has no Market to convert through")
	}
	pool, err := k.Uncommitted(ctx, chain.NoahBaseDenom)
	if err != nil {
		return funding{}, err
	}
	offer, output, _, err := k.marketKeeper.Buy(ctx, k.insuranceAddress, shortfall, chain.NoahBaseDenom, chain.NoahCoin(maxOffer))
	if err != nil {
		return funding{}, fmt.Errorf("funding %s: %w", shortfall, err)
	}
	if offer.Amount.GT(pool) {
		return funding{}, fmt.Errorf("offer %s exceeds uncommitted Insurance NOAH", offer)
	}
	return funding{custody: custody, conversion: &conversion{offer: offer.Amount, output: output}}, nil
}

// valuation reads the reference and the fresh rates for denom, refusing a claim the oracle cannot
// value.
func (k *Keeper) valuation(ctx context.Context, denom string) (valuation, error) {
	reference, err := k.oracleKeeper.GetReferenceDenom(ctx)
	if err != nil {
		return valuation{}, err
	}
	if reference == "" {
		return valuation{}, errors.New("no reference denomination to value the claim in")
	}
	rates, err := k.oracleKeeper.GetRateSet(ctx, denom, reference)
	if err != nil {
		return valuation{}, fmt.Errorf("valuing the claim: %w", err)
	}
	return valuation{reference: reference, rates: rates}, nil
}

// charge values what a claim drew, custody in its denomination and the NOAH it converted, each
// rounded up, so a conversion's spread counts against the limit as well as its face value.
func (v valuation) charge(custody sdk.Coin, offer math.Int) (math.Int, error) {
	charge, err := v.rates.ConvertCeilSum(v.reference, custody, chain.NoahCoin(offer))
	if err != nil {
		return math.Int{}, fmt.Errorf("valuing the claim: %w", err)
	}
	if err := chain.ValidateAmount(charge, true); err != nil {
		return math.Int{}, fmt.Errorf("claim charge: %w", err)
	}
	return charge, nil
}
