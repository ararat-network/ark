// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/market/types/genesis.go.
// Modified for Ark: genesis construction, validation, and state integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params,
	arkPoolDelta math.LegacyDec,
	tobinTaxOverrides []TobinTaxOverride,
	conversionPolicy ConversionPolicy,
	conversionMandate ConversionMandate,
) *GenesisState {
	return &GenesisState{
		Params:            params,
		ArkPoolDelta:      arkPoolDelta,
		TobinTaxOverrides: tobinTaxOverrides,
		ConversionPolicy:  conversionPolicy,
		ConversionMandate: conversionMandate,
	}
}

// DefaultGenesisState returns the default Market state with no per-denomination Tobin overrides.
// All assets inherit the default until an override is set.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		ArkPoolDelta:      math.LegacyZeroDec(),
		Params:            DefaultParams(),
		TobinTaxOverrides: []TobinTaxOverride{},
		ConversionPolicy:  DefaultConversionPolicy(),
		ConversionMandate: DefaultConversionMandate(),
	}
}

// Validate validates the provided market genesis state
func (gs GenesisState) Validate() error {
	if gs.ArkPoolDelta.IsNil() {
		return errors.New("ark pool delta must not be nil")
	}

	if err := gs.Params.Validate(); err != nil {
		return err
	}

	if err := gs.ConversionPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid conversion policy: %w", err)
	}

	if _, err := NewEffectivePools(gs.ConversionPolicy.BasePool.Amount, gs.ArkPoolDelta); err != nil {
		return err
	}

	if err := gs.ConversionMandate.Validate(); err != nil {
		return err
	}
	// Genesis permits corridor/pool denomination mismatch because reference rebasing creates that
	// reachable state. Policy updates remain unauthorised until units match again; expiry likewise
	// does not make an export invalid.

	// Overrides are a sparse map in storage, so genesis carries them in
	// deterministic order: sorted by unique denomination.
	for i, override := range gs.TobinTaxOverrides {
		if err := chain.ValidatePricedDenom(override.Denom); err != nil {
			return fmt.Errorf("tobin tax override %w", err)
		}
		if err := ValidateTobinTax(override.TobinTax); err != nil {
			return fmt.Errorf("tobin tax override for %s is invalid: %w", override.Denom, err)
		}
		if i > 0 && override.Denom <= gs.TobinTaxOverrides[i-1].Denom {
			return errors.New("tobin tax overrides must be sorted by unique denom")
		}
	}

	return nil
}
