package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

// Default parameter values
var (
	DefaultMinStabilitySpread = math.LegacyNewDecWithPrec(2, 2)  // 2%
	DefaultTobinTax           = math.LegacyNewDecWithPrec(25, 4) // 0.25%
)

// DefaultParams creates default market module parameters
func DefaultParams() Params {
	return Params{
		MinStabilitySpread: DefaultMinStabilitySpread,
		DefaultTobinTax:    DefaultTobinTax,
	}
}

// ValidateTobinTax checks one Tobin rate, the default or an override. The
// upper bound is exclusive: a rate of one would consume the entire conversion
// output, which is a refusal to convert expressed as a fee rather than a fee.
func ValidateTobinTax(tobinTax math.LegacyDec) error {
	if tobinTax.IsNil() {
		return errors.New("tobin tax must be set")
	}
	if tobinTax.IsNegative() || tobinTax.GTE(math.LegacyOneDec()) {
		return fmt.Errorf("tobin tax must be in [0, 1), is %s", tobinTax)
	}

	return nil
}

// Validate validates the set of params
func (p Params) Validate() error {
	if p.MinStabilitySpread.IsNil() {
		return errors.New("min stability spread must be set")
	}
	if p.MinStabilitySpread.IsNegative() || p.MinStabilitySpread.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min stability spread must be in [0, 1], is %s", p.MinStabilitySpread)
	}
	if err := ValidateTobinTax(p.DefaultTobinTax); err != nil {
		return fmt.Errorf("invalid default tobin tax: %w", err)
	}
	return nil
}
