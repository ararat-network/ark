package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

// DefaultExposureState returns the calm-market starting state: no price
// sampled, both series empty, and a multiplier of one, which every target
// consumer scales by without effect.
func DefaultExposureState() ExposureState {
	return ExposureState{
		LastReferencePrice: math.LegacyZeroDec(),
		VolatilityVariance: math.LegacyZeroDec(),
		FlowPressure:       math.LegacyZeroDec(),
		LiabilityRatio:     math.LegacyZeroDec(),
		FlowRatio:          math.LegacyZeroDec(),
		Multiplier:         math.LegacyOneDec(),
	}
}

// Validate performs context-free validation of the stored risk state. The cap
// is not checked here: it lives in Params, and a state imported beside a
// lowered cap is clamped by the next update rather than refused at genesis.
func (s ExposureState) Validate() error {
	// Price, NOAH flow, and the two ratios require non-negative values without field-specific
	// ceilings. Variance has a [0, 1] range and is validated separately.
	for _, field := range []struct {
		name  string
		value math.LegacyDec
	}{
		{"LastReferencePrice", s.LastReferencePrice},
		{"FlowPressure", s.FlowPressure},
		{"LiabilityRatio", s.LiabilityRatio},
		{"FlowRatio", s.FlowRatio},
	} {
		if field.value.IsNil() {
			return fmt.Errorf("treasury exposure state %s must be set", field.name)
		}
		if !field.value.IsInValidRange() {
			return fmt.Errorf("treasury exposure state %s is not representable", field.name)
		}
		if field.value.IsNegative() {
			return fmt.Errorf("treasury exposure state %s must be zero or positive: %s", field.name, field.value)
		}
	}

	if s.VolatilityVariance.IsNil() {
		return errors.New("treasury exposure state VolatilityVariance must be set")
	}
	if !s.VolatilityVariance.IsInValidRange() {
		return errors.New("treasury exposure state VolatilityVariance is not representable")
	}
	// The sample clamp bounds the series to [0, 1] by induction, so a value
	// outside it is either a corrupted export or a hand-edited genesis; above
	// one it would inflate annualised volatility without bound.
	if s.VolatilityVariance.IsNegative() || s.VolatilityVariance.GT(math.LegacyOneDec()) {
		return fmt.Errorf(
			"treasury exposure state VolatilityVariance must be between zero and one: %s",
			s.VolatilityVariance,
		)
	}

	if s.Multiplier.IsNil() {
		return errors.New("treasury exposure state Multiplier must be set")
	}
	if !s.Multiplier.IsInValidRange() {
		return errors.New("treasury exposure state Multiplier is not representable")
	}
	if s.Multiplier.LT(math.LegacyOneDec()) {
		return fmt.Errorf(
			"treasury exposure state Multiplier must be at least one: %s",
			s.Multiplier,
		)
	}

	return nil
}
