package types

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// NumeraireVerdict prices NOAH at one by definition. It carries no asset record because NOAH is not
// a registered asset.
func NumeraireVerdict() PricedAsset {
	rate := math.LegacyOneDec()

	return PricedAsset{
		Rate:   &rate,
		Source: PriceSource_PRICE_SOURCE_NUMERAIRE,
	}
}

// PriceVerdict maps lifecycle status, rates, and an optional plan to a verdict carrying its asset
// record. Status overrides feed rates. A suspended asset's plan values supply even before
// redemption activates. Oracle authority must agree with IsOraclePriced; see x/asset/README.md.
func PriceVerdict(asset Asset, rates oracletypes.RateSet, plan *SettlementPlan) PricedAsset {
	switch {
	case asset.IsOraclePriced():
		if rate, ok := rates[asset.Denom]; ok {
			return PricedAsset{Asset: asset, Rate: &rate, Source: PriceSource_PRICE_SOURCE_ORACLE}
		}
		return PricedAsset{Asset: asset, Reason: UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE}
	case asset.Status == AssetStatus_ASSET_STATUS_SUSPENDED:
		if plan == nil {
			return PricedAsset{Asset: asset, Reason: UnpricedReason_UNPRICED_REASON_UNTRUSTED}
		}
		// Copied rather than aliased: the verdict outlives this call, and a plan
		// the caller still holds must not be able to change what was decided.
		rate := plan.RedemptionRate

		return PricedAsset{Asset: asset, Rate: &rate, Source: PriceSource_PRICE_SOURCE_SETTLEMENT}
	case asset.Status == AssetStatus_ASSET_STATUS_WRITTEN_OFF:
		return PricedAsset{Asset: asset, Reason: UnpricedReason_UNPRICED_REASON_WRITTEN_OFF}
	case asset.Status == AssetStatus_ASSET_STATUS_RETIRED:
		return PricedAsset{Asset: asset, Reason: UnpricedReason_UNPRICED_REASON_RETIRED}
	default:
		return PricedAsset{Asset: asset, Reason: UnpricedReason_UNPRICED_REASON_UNRECOGNISED}
	}
}

// IsPriced reports whether a rate stands behind the verdict. It reads the
// source rather than a stored flag so the two can never disagree: naming an
// authority is what being priced means.
func (p PricedAsset) IsPriced() bool {
	return p.Source != PriceSource_PRICE_SOURCE_UNSPECIFIED
}

// AssetPricings maps denominations to verdicts and optional registry records. NOAH and unregistered
// denominations have no record. Missing map entries have no verdict; callers branching on Reason
// must check presence.
type AssetPricings map[string]PricedAsset

// Convert delegates offer-to-ask arithmetic to the rate set using priced verdicts only. Missing or
// unpriced denominations fail as unknown rates.
func (p AssetPricings) Convert(offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	return p.convert(offerCoin, askDenom, false)
}

// ConvertLastKnown also admits stale rates for unavailable feeds, solely to total outstanding
// supply. Other unpriced reasons remain errors. Quotes, minting, and payments must use Convert.
func (p AssetPricings) ConvertLastKnown(offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	return p.convert(offerCoin, askDenom, true)
}

func (p AssetPricings) convert(offerCoin sdk.DecCoin, askDenom string, admitLastKnown bool) (sdk.DecCoin, error) {
	rates := make(oracletypes.RateSet, 2)
	for _, denom := range [2]string{offerCoin.Denom, askDenom} {
		verdict, ok := p[denom]
		switch {
		case !ok:
		case verdict.IsPriced() && verdict.Rate != nil:
			rates[denom] = *verdict.Rate
		case admitLastKnown &&
			verdict.Reason == UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE &&
			verdict.LastRate != nil &&
			verdict.LastRate.IsPositive():
			rates[denom] = *verdict.LastRate
		}
	}
	return rates.Convert(offerCoin, askDenom)
}
