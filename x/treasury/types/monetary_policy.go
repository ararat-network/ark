package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

// DefaultMonetaryPolicy returns the disabled launch policy values.
func DefaultMonetaryPolicy() MonetaryPolicy {
	return MonetaryPolicy{
		StabilityTaxRate:            math.LegacyZeroDec(),
		ValidatorBlockRewardTarget:  math.ZeroInt(),
		OracleBlockRewardTarget:     math.ZeroInt(),
		RedemptionBufferTargetRatio: math.LegacyZeroDec(),
		StrategicReserveTargetRatio: math.LegacyZeroDec(),
		InsuranceTargetRatio:        math.LegacyZeroDec(),
	}
}

// Validate performs context-free validation of one reversible policy.
func (policy MonetaryPolicy) Validate() error {
	if policy.StabilityTaxRate.IsNil() {
		return errors.New("treasury parameter StabilityTaxRate must be set")
	}
	if !policy.StabilityTaxRate.IsInValidRange() {
		return errors.New("treasury parameter StabilityTaxRate is not representable")
	}
	if policy.StabilityTaxRate.IsNegative() || policy.StabilityTaxRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter StabilityTaxRate must be between zero and one: %s", policy.StabilityTaxRate)
	}

	if policy.ValidatorBlockRewardTarget.IsNil() {
		return errors.New("treasury parameter ValidatorBlockRewardTarget must be set")
	}
	if policy.ValidatorBlockRewardTarget.IsNegative() {
		return fmt.Errorf("treasury parameter ValidatorBlockRewardTarget must be zero or positive: %s", policy.ValidatorBlockRewardTarget)
	}
	if policy.OracleBlockRewardTarget.IsNil() {
		return errors.New("treasury parameter OracleBlockRewardTarget must be set")
	}
	if policy.OracleBlockRewardTarget.IsNegative() {
		return fmt.Errorf("treasury parameter OracleBlockRewardTarget must be zero or positive: %s", policy.OracleBlockRewardTarget)
	}
	if policy.RedemptionBufferTargetRatio.IsNil() {
		return errors.New("treasury parameter RedemptionBufferTargetRatio must be set")
	}
	if !policy.RedemptionBufferTargetRatio.IsInValidRange() {
		return errors.New("treasury parameter RedemptionBufferTargetRatio is not representable")
	}
	if policy.RedemptionBufferTargetRatio.IsNegative() || policy.RedemptionBufferTargetRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter RedemptionBufferTargetRatio must be between zero and one: %s", policy.RedemptionBufferTargetRatio)
	}

	if policy.StrategicReserveTargetRatio.IsNil() {
		return errors.New("treasury parameter StrategicReserveTargetRatio must be set")
	}
	if !policy.StrategicReserveTargetRatio.IsInValidRange() {
		return errors.New("treasury parameter StrategicReserveTargetRatio is not representable")
	}
	if policy.StrategicReserveTargetRatio.IsNegative() || policy.StrategicReserveTargetRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter StrategicReserveTargetRatio must be between zero and one: %s", policy.StrategicReserveTargetRatio)
	}

	if policy.InsuranceTargetRatio.IsNil() {
		return errors.New("treasury parameter InsuranceTargetRatio must be set")
	}
	if !policy.InsuranceTargetRatio.IsInValidRange() {
		return errors.New("treasury parameter InsuranceTargetRatio is not representable")
	}
	if policy.InsuranceTargetRatio.IsNegative() || policy.InsuranceTargetRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter InsuranceTargetRatio must be between zero and one: %s", policy.InsuranceTargetRatio)
	}

	return nil
}

// Equal reports whether two monetary policies contain identical values.
func (policy MonetaryPolicy) Equal(other MonetaryPolicy) bool {
	return policy.StabilityTaxRate.Equal(other.StabilityTaxRate) &&
		policy.ValidatorBlockRewardTarget.Equal(other.ValidatorBlockRewardTarget) &&
		policy.OracleBlockRewardTarget.Equal(other.OracleBlockRewardTarget) &&
		policy.RedemptionBufferTargetRatio.Equal(other.RedemptionBufferTargetRatio) &&
		policy.StrategicReserveTargetRatio.Equal(other.StrategicReserveTargetRatio) &&
		policy.InsuranceTargetRatio.Equal(other.InsuranceTargetRatio)
}

// IsZero reports whether every policy value is zero.
func (policy MonetaryPolicy) IsZero() bool {
	return policy.StabilityTaxRate.IsZero() &&
		policy.ValidatorBlockRewardTarget.IsZero() &&
		policy.OracleBlockRewardTarget.IsZero() &&
		policy.RedemptionBufferTargetRatio.IsZero() &&
		policy.StrategicReserveTargetRatio.IsZero() &&
		policy.InsuranceTargetRatio.IsZero()
}
