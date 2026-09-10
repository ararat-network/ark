package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// Pricings returns records and pricing verdicts for requested denominations, plus NOAH.
// Unregistered denominations and NOAH carry no asset record. Consumers apply their own policy to
// unpriced verdicts.
func (k Keeper) Pricings(ctx context.Context, denoms ...string) (types.AssetPricings, error) {
	pricings := make(types.AssetPricings, len(denoms)+1)

	// Membership is a point read per denomination rather than a registry scan:
	// the registry is keyed by denomination, so the denoms in hand bound the
	// work regardless of how many assets are listed. listed preserves the
	// caller's order so the plan reads below stay deterministic.
	seen := make(map[string]struct{}, len(denoms))
	listed := make([]types.Asset, 0, len(denoms))
	for _, denom := range denoms {
		if denom == chain.NoahBaseDenom {
			continue
		}
		if _, duplicate := seen[denom]; duplicate {
			continue
		}
		seen[denom] = struct{}{}

		asset, err := k.Assets.Get(ctx, denom)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				pricings[denom] = types.PricedAsset{
					Reason: types.UnpricedReason_UNPRICED_REASON_UNRECOGNISED,
				}
				continue
			}
			return nil, fmt.Errorf("getting asset %s for pricing: %w", denom, err)
		}
		listed = append(listed, asset)
	}

	if err := k.price(ctx, listed, pricings); err != nil {
		return nil, err
	}

	return pricings, nil
}

// PricedAssets returns every registered asset's verdict and its denominations in registry key
// order. Use the ordered list for deterministic folds; use Pricings when starting from specific
// denominations.
func (k Keeper) PricedAssets(ctx context.Context) ([]string, types.AssetPricings, error) {
	listed, err := k.ListAssets(ctx)
	if err != nil {
		return nil, nil, err
	}

	pricings := make(types.AssetPricings, len(listed)+1)
	if err := k.price(ctx, listed, pricings); err != nil {
		return nil, nil, err
	}

	denoms := make([]string, len(listed))
	for i, asset := range listed {
		denoms[i] = asset.Denom
	}

	return denoms, pricings, nil
}

// price fills the shared verdict map from asset records, captured rates, and settlement plans. It
// seeds NOAH's identity rate; callers supply unregistered-denomination verdicts.
func (k Keeper) price(
	ctx context.Context,
	listed []types.Asset,
	pricings types.AssetPricings,
) error {
	pricings[chain.NoahBaseDenom] = types.NumeraireVerdict()

	needed := make([]string, 0, len(listed))
	for _, asset := range listed {
		if asset.IsOraclePriced() {
			needed = append(needed, asset.Denom)
		}
	}

	// A set with no oracle-priced member asks the Oracle nothing and leaves
	// rates nil, which only the oracle-priced verdict branch would have read.
	var rates oracletypes.RateSet
	if len(needed) > 0 {
		captured, err := k.oracleKeeper.GetAvailableRateSet(ctx, needed...)
		if err != nil {
			return fmt.Errorf("capturing rates for pricing: %w", err)
		}
		rates = captured
	}

	// unavailable stays nil while every member prices, which is both the common
	// case and the one that should not allocate.
	var unavailable []string
	for _, asset := range listed {
		var plan *types.SettlementPlan
		if asset.Status == types.AssetStatus_ASSET_STATUS_SUSPENDED {
			stored, found, err := k.GetSettlementPlan(ctx, asset.Denom)
			if err != nil {
				return fmt.Errorf("getting settlement plan for %s: %w", asset.Denom, err)
			}
			if found {
				plan = &stored
			}
		}
		verdict := types.PriceVerdict(asset, rates, plan)
		if verdict.Reason == types.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE {
			unavailable = append(unavailable, asset.Denom)
		}
		pricings[asset.Denom] = verdict
	}

	return k.attachLastKnownRates(ctx, unavailable, pricings)
}

// attachLastKnownRates enriches feed-unavailable verdicts without making them priced. Only degraded
// denominations trigger the extra Oracle read; never-priced feeds retain nil LastRate.
func (k Keeper) attachLastKnownRates(
	ctx context.Context,
	unavailable []string,
	pricings types.AssetPricings,
) error {
	if len(unavailable) == 0 {
		return nil
	}

	lastKnown, err := k.oracleKeeper.GetLastKnownRateSet(ctx, unavailable...)
	if err != nil {
		return fmt.Errorf("capturing last known rates for pricing: %w", err)
	}
	for _, denom := range unavailable {
		rate, ok := lastKnown[denom]
		if !ok {
			continue
		}
		entry := pricings[denom]
		entry.LastRate = &rate
		pricings[denom] = entry
	}

	return nil
}
