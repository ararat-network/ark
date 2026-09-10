package types

import (
	"fmt"

	"github.com/cosmos/gogoproto/proto"

	"github.com/ararat-network/ark/pkg/chain"
)

// Validate checks identity, status, and exact denomination-derived Bank metadata. Asset
// denominations must be priceable; matching the canonical metadata also validates units, exponent,
// aliases, and display fields.
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

// IsOraclePriced reports Oracle pricing authority by status, not current rate availability. It must
// match PriceVerdict's Oracle arm and ordinary conversion eligibility; settlement pricing is
// separate.
func (a Asset) IsOraclePriced() bool {
	return a.Status == AssetStatus_ASSET_STATUS_ACTIVE ||
		a.Status == AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
}
