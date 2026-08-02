package types

import (
	"fmt"

	"github.com/cosmos/gogoproto/proto"

	chain "ark/pkg/chain"
)

// Validate checks the asset's identity, metadata, and local lifecycle fields.
//
// The denomination is also the key of the oracle feed pricing the asset, which
// is why the identity check is the priced-denomination rule: conversion is the
// chain's only native mint path and conversion needs a rate, so an asset that
// could not be priced could never acquire supply.
//
// Metadata is derived from the denomination at registration and never amended,
// so the only metadata an asset may carry is the metadata its own denomination
// produces. Comparing the whole record against that derivation subsumes the
// base, display, unit, exponent, and alias rules at once — none of them can
// fail on their own once the record matches — and it closes the one door a
// per-field rule left open: a hand-written genesis naming its own description,
// symbol, or display name.
func (a Asset) Validate() error {
	if err := chain.ValidatePricedDenom(a.Denom); err != nil {
		return fmt.Errorf("asset %w", err)
	}
	derived := chain.NativeAssetMetadata(a.Denom)
	if !proto.Equal(&a.Metadata, &derived) {
		return fmt.Errorf(
			"asset %s metadata must equal the metadata derived from its denomination",
			a.Denom,
		)
	}
	if a.Status != AssetStatus_ASSET_STATUS_ACTIVE &&
		a.Status != AssetStatus_ASSET_STATUS_ISSUANCE_HALTED &&
		a.Status != AssetStatus_ASSET_STATUS_SUSPENDED &&
		a.Status != AssetStatus_ASSET_STATUS_WRITTEN_OFF &&
		a.Status != AssetStatus_ASSET_STATUS_RETIRED {
		return fmt.Errorf("asset status must be specified and known: %d", a.Status)
	}
	if a.Version == 0 {
		return fmt.Errorf("asset version must be positive")
	}

	return nil
}

// IsOraclePriced reports whether the Oracle is the asset's price authority.
// Every asset is keyed to a feed by its own denomination, so this is purely a
// status predicate: it is exactly the statuses PriceVerdict answers with
// PRICE_SOURCE_ORACLE, and holding the two in step is what keeps a consumer
// asking about membership and a consumer reading a verdict from disagreeing.
//
// It names an authority, not an outcome. A member whose feed is unavailable is
// still a member and is still unpriced this block, and a suspended asset under
// a settlement plan is priced without being a member — the plan is the
// authority there, not the Oracle.
//
// It is also exactly the convertible set, because conversion is the only native
// mint path and membership is convertibility by invariant.
func (a Asset) IsOraclePriced() bool {
	return a.Status == AssetStatus_ASSET_STATUS_ACTIVE ||
		a.Status == AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
}
