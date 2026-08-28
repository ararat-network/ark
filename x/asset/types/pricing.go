package types

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// NumeraireVerdict is NOAH's verdict: one, by definition rather than by
// observation. Every valuation flow carries it, because it is the unit every
// other verdict is quoted against.
//
// It carries no record. NOAH is priced by definition rather than by
// registration, so it is not a registered asset and ValidatePricedDenom
// refuses it as an asset denomination.
func NumeraireVerdict() PricedAsset {
	rate := math.LegacyOneDec()

	return PricedAsset{
		Rate:   &rate,
		Source: PriceSource_PRICE_SOURCE_NUMERAIRE,
	}
}

// PriceVerdict folds one registered asset and the inputs in hand into its
// pricing verdict. It is the registry's whole authority table — what a
// denomination is worth and on whose authority — as a pure function of
// lifecycle status, the rates captured for this flow, and the asset's
// settlement plan when it has one.
//
// Status is answered before any rate is read. A suspended asset's feed keeps
// running, so a rate is usually present and must lose to the status: the market
// price is exactly what stopped being trustworthy.
//
// Every status maps to exactly one authority here, and IsOraclePriced is the
// preimage of the first arm — consumers that need the set of denominations the
// Oracle prices ask that predicate rather than folding a verdict and reading
// Source back off it. The two must move together.
//
// A settlement plan prices suspended supply because it is a standing commitment
// to convert the asset to NOAH at its stored rate, quoted in the same
// units-per-NOAH orientation as every Oracle rate: the protocol stands behind
// that number in the only sense valuation needs. Activation height is
// deliberately not consulted — a commitment values the coin whether or not
// redemption has opened, and reading it would make consumers disagree for the
// length of the activation delay. Whether redemption is *open* is an
// action-eligibility question each consumer answers for itself.
//
// plan is nil when the asset has none; it is meaningful only for suspended
// supply, because a plan exists only while its asset is suspended.
//
// The record the verdict was folded from is carried back out with it, so a
// caller cannot file a verdict against the wrong asset: the pairing is made
// here, where the two are known to belong together, rather than at each call
// site.
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

// AssetPricings maps denominations to what the registry knows about them for
// one valuation flow: the record, where there is one, and the verdict.
//
// It is keyed by denomination rather than by asset because the denominations a
// flow asks about outnumber the registry: NOAH is priced by definition and is
// not a registered asset, and a flow that starts from balances — a fee
// collector's, say — can name denominations the registry does not list at all.
// Those entries carry a verdict and no record, which is the honest answer and
// the reason nothing reads Asset without having established the denomination is
// a member.
//
// A denomination absent from the map is not a verdict. Its zero value names no
// source and no reason, which is the wire's meaning of an unset field rather
// than a claim about the denomination, so consumers that branch on Reason must
// check presence instead of indexing blind.
type AssetPricings map[string]PricedAsset

// Convert converts an offer coin into the ask denom through the priced
// verdicts. Only the two denominations in play are projected, so conversion
// arithmetic and its validations live in exactly one place — the rate set.
// An unpriced or missing verdict converts like an unknown denomination:
// callers that tolerate unpriced coins check Priced before converting, and
// a verdict without a rate has nothing honest to convert through.
func (p AssetPricings) Convert(offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	return p.convert(offerCoin, askDenom, false)
}

// ConvertLastKnown converts as Convert does, but additionally admits the last
// known rate of a denomination whose feed is unavailable. It exists for one
// purpose: totalling supply already outstanding, where excluding what this
// block could not price would understate the total and hand the excluded
// holders' share to whoever is transacting.
//
// It is not a relaxed Convert. Every other unpriced reason still converts like
// an unknown denomination, because those are statements about the obligation
// itself — written off, or suspended with no committed rate — where no rate,
// however recent, is the honest answer. Only an unavailable feed leaves the
// obligation intact and the evidence merely old. Callers that pay, mint, or
// quote against the result must use Convert.
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
