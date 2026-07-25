package types

import (
	"fmt"

	chain "ark/pkg/chain"
)

// Validate checks the asset's identity, metadata, and local lifecycle fields.
func (a Asset) Validate() error {
	if err := validateAssetDenom(a.Denom); err != nil {
		return fmt.Errorf("asset %w", err)
	}
	if a.Metadata.Base != a.Denom {
		return fmt.Errorf(
			"asset metadata base denom %s must match asset denom %s",
			a.Metadata.Base,
			a.Denom,
		)
	}
	for i, unit := range a.Metadata.DenomUnits {
		if unit == nil {
			return fmt.Errorf("asset metadata denomination unit %d must not be nil", i)
		}
	}
	if len(a.Metadata.DenomUnits) < 2 {
		return fmt.Errorf(
			"asset metadata must contain at least base and display denomination units: %d",
			len(a.Metadata.DenomUnits),
		)
	}
	if err := a.Metadata.Validate(); err != nil {
		return fmt.Errorf("asset metadata is invalid: %w", err)
	}
	if a.Metadata.Display != a.Denom[1:] {
		return fmt.Errorf(
			"asset display denom must be %s: %s",
			a.Denom[1:],
			a.Metadata.Display,
		)
	}
	for _, unit := range a.Metadata.DenomUnits {
		if len(unit.Aliases) != 0 {
			return fmt.Errorf("asset denomination unit %s must not define aliases", unit.Denom)
		}
		if unit.Denom == a.Metadata.Display &&
			unit.Exponent != chain.NativeDisplayExponent {
			return fmt.Errorf(
				"asset display exponent must be %d: %d",
				chain.NativeDisplayExponent,
				unit.Exponent,
			)
		}
	}
	switch a.Status {
	case AssetStatus_ASSET_STATUS_PENDING,
		AssetStatus_ASSET_STATUS_ACTIVE,
		AssetStatus_ASSET_STATUS_RETIRING,
		AssetStatus_ASSET_STATUS_RETIRED:
	case AssetStatus_ASSET_STATUS_REMOVAL_PENDING,
		AssetStatus_ASSET_STATUS_DELISTING,
		AssetStatus_ASSET_STATUS_DELISTED,
		AssetStatus_ASSET_STATUS_SETTLING,
		AssetStatus_ASSET_STATUS_RELISTING,
		AssetStatus_ASSET_STATUS_WRITTEN_OFF:
		if !a.OracleRequired {
			return fmt.Errorf(
				"%s asset must require Oracle pricing",
				a.Status,
			)
		}
	default:
		return fmt.Errorf("asset status must be specified and known: %d", a.Status)
	}
	if a.Version == 0 {
		return fmt.Errorf("asset version must be positive")
	}

	return nil
}

// Validate checks that an asset lock has a governable asset denom and known
// dependency kind.
func (l AssetLock) Validate() error {
	if err := validateAssetDenom(l.Denom); err != nil {
		return fmt.Errorf("asset lock %w", err)
	}
	if !validAssetLockKind(l.Kind) {
		return fmt.Errorf("asset lock kind must be specified and known: %d", l.Kind)
	}

	return nil
}

func validateAssetDenom(denom string) error {
	if err := chain.ValidateNativeBaseDenom(denom); err != nil {
		return err
	}
	if denom == chain.NoahBaseDenom {
		return fmt.Errorf("must not use native denom %s", denom)
	}

	return nil
}

func validAssetLockKind(kind AssetLockKind) bool {
	switch kind {
	case AssetLockKind_ASSET_LOCK_KIND_MARKET_ASSET_POLICY,
		AssetLockKind_ASSET_LOCK_KIND_MARKET_BASE_POOL,
		AssetLockKind_ASSET_LOCK_KIND_TREASURY_STABLE_POLICY,
		AssetLockKind_ASSET_LOCK_KIND_TREASURY_REFERENCE_TAX_CAP:
		return true
	default:
		return false
	}
}
