package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// Pricings answers, for each denomination named, what it is worth in NOAH and
// on whose authority — or the reason nothing stands behind it. It is the
// registry's single valuation entry point: consumers apply policy to the
// verdicts it returns — defer, disclose, zero, route — and never re-derive the
// facts.
//
// Each entry carries the registry record too, where the denomination has one,
// so a caller that needs the asset does not read a row the fold has already
// read to price it. A denomination outside the registry, and the numeraire,
// carry a verdict and no record.
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

// PricedAssets prices every registered member, returning their denominations in
// registry key order alongside the pricings map that holds the records and
// verdicts. The order is returned separately because a map has none, and
// callers that fold over the registry — the liability partition discloses
// per-denomination exposure, the assets query lists it — must not vary with
// Go's map iteration. It answers exactly as Pricings
// does for a caller that named the whole registry, without that caller having
// to list it first and hand the denominations back — which reads every record
// twice, once to enumerate and once to price.
//
// Callers holding denominations rather than records — balances of a fee
// collector, say, which may name a denomination outside the registry entirely —
// want Pricings instead.
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

// price is the fold both entry points share: it seeds the numeraire, captures
// the rates the listed members need, reads the settlement plan of any suspended
// one, and records a verdict for each. Only how the members were arrived at
// differs above it, so the two entry points cannot drift on what a verdict
// means.
//
// pricings is filled in place. The numeraire is seeded here because every
// valuation flow carries it whatever named it, and no registered asset can
// collide with it: ValidatePricedDenom refuses NOAH as an asset denomination.
// The entry points contribute only what this fold cannot derive from the
// records it is given — a verdict for a denomination the registry does not
// list.
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

// attachLastKnownRates fills LastRate on the verdicts the fold recorded as
// feed-unavailable, which it names rather than rediscovering by rescanning the
// map it just filled. It amends the verdicts rather than feeding PriceVerdict
// so the verdict itself stays a pure function of status, rates, and plan, and
// the extra store read still happens only in the degraded case that needs it —
// a fully priced set hands over no denominations and asks the Oracle nothing.
//
// A denom the Oracle has never priced simply keeps a nil LastRate. That is the
// honest answer: there is no evidence of what it was worth, and inventing one
// would be worse than the exclusion it would paper over.
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
