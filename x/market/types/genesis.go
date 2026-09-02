package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	chain "github.com/ararat-network/ark/pkg/chain"
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

// DefaultGenesisState returns raw genesis raw message for testing.
//
// The launch set carries no override. Terra's factoring seeded one for MNT at
// eight times the default, an illiquidity premium; every asset Ark launches
// with is a top-15 currency, so the default rate covers all of them and the
// override map stays a governance tool rather than a launch value.
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
	// The corridor is deliberately not required to share the pool's
	// denomination. A reference re-point rebases the pool and leaves the bounds
	// in the unit governance appointed them in, so a chain exported between that
	// re-point and the next re-appointment carries exactly this mismatch — and a
	// chain must be able to start from its own export. Requiring the two to
	// agree here would assert the one invariant the rebase is designed to break.
	//
	// Nothing is lost by accepting it: ValidatePolicy refuses every candidate
	// while the units disagree, so a stranded corridor authorises nothing until
	// governance re-appoints. Genesis already admits the other shape of
	// un-actable mandate — one whose window has closed — for the same reason.

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
