package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

var DefaultTobinTax = math.LegacyNewDecWithPrec(25, 4) // 0.25%

// DefaultParams creates default market module parameters
func DefaultParams() Params {
	return Params{
		DefaultTobinTax: DefaultTobinTax,
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if err := ValidateTobinTax(p.DefaultTobinTax); err != nil {
		return fmt.Errorf("invalid default tobin tax: %w", err)
	}
	return nil
}

// ValidateTobinTax checks one Tobin rate, the default or an override.
func ValidateTobinTax(tobinTax math.LegacyDec) error {
	if tobinTax.IsNil() {
		return errors.New("tobin tax must be set")
	}
	if tobinTax.IsNegative() || tobinTax.GTE(math.LegacyOneDec()) {
		return fmt.Errorf("tobin tax must be in [0, 1), is %s", tobinTax)
	}

	return nil
}
