package types

import (
	"errors"
	"fmt"
)

// DefaultMonetaryMandate returns the canonical disabled committee
// mandate. The term may increase later while the mandate remains disabled.
func DefaultMonetaryMandate() MonetaryMandate {
	return NewDisabledMonetaryMandate(0)
}

// NewDisabledMonetaryMandate returns a canonical disabled mandate at the
// supplied term.
func NewDisabledMonetaryMandate(term uint64) MonetaryMandate {
	policy := DefaultMonetaryPolicy()
	return MonetaryMandate{
		Term:          term,
		MinimumPolicy: policy,
		MaximumPolicy: policy,
	}
}

// Validate validates either a disabled mandate or one complete bounded
// committee appointment.
func (mandate MonetaryMandate) Validate() error {
	if err := mandate.MinimumPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid monetary-policy minimum: %w", err)
	}
	if err := mandate.MaximumPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid monetary-policy maximum: %w", err)
	}

	if mandate.Committee == "" {
		if mandate.ActivationHeight != 0 || mandate.ExpiryHeight != 0 {
			return errors.New("disabled monetary mandate must not have an activation or expiry height")
		}
		if !mandate.MinimumPolicy.Equal(mandate.MaximumPolicy) || !mandate.MinimumPolicy.IsZero() {
			return errors.New("disabled monetary mandate must use identical zero bounds")
		}
		return nil
	}

	if mandate.Term == 0 {
		return errors.New("configured monetary mandate term must be positive")
	}
	if _, err := ParseCanonicalAccountAddress("monetary-policy committee", mandate.Committee); err != nil {
		return err
	}
	if mandate.ActivationHeight >= mandate.ExpiryHeight {
		return errors.New("monetary mandate activation height must precede expiry height")
	}
	if err := mandate.ValidatePolicy(mandate.MinimumPolicy); err != nil {
		return fmt.Errorf("invalid monetary-policy bounds: %w", err)
	}
	return nil
}

// IsActive reports whether the mandate can authorize a committee update at
// the supplied height.
func (mandate MonetaryMandate) IsActive(height int64) bool {
	return mandate.Committee != "" &&
		mandate.ActivationHeight <= uint64(height) &&
		uint64(height) < mandate.ExpiryHeight
}

// ValidatePolicy checks a committee policy against every mandate bound.
func (mandate MonetaryMandate) ValidatePolicy(policy MonetaryPolicy) error {
	if policy.StabilityTaxRate.LT(mandate.MinimumPolicy.StabilityTaxRate) ||
		policy.StabilityTaxRate.GT(mandate.MaximumPolicy.StabilityTaxRate) {
		return fmt.Errorf(
			"stability tax rate %s is outside mandate range [%s, %s]",
			policy.StabilityTaxRate,
			mandate.MinimumPolicy.StabilityTaxRate,
			mandate.MaximumPolicy.StabilityTaxRate,
		)
	}
	if policy.RedemptionBufferTargetRatio.LT(mandate.MinimumPolicy.RedemptionBufferTargetRatio) ||
		policy.RedemptionBufferTargetRatio.GT(mandate.MaximumPolicy.RedemptionBufferTargetRatio) {
		return fmt.Errorf(
			"redemption Buffer target ratio %s is outside mandate range [%s, %s]",
			policy.RedemptionBufferTargetRatio,
			mandate.MinimumPolicy.RedemptionBufferTargetRatio,
			mandate.MaximumPolicy.RedemptionBufferTargetRatio,
		)
	}
	if policy.StrategicReserveTargetRatio.LT(mandate.MinimumPolicy.StrategicReserveTargetRatio) ||
		policy.StrategicReserveTargetRatio.GT(mandate.MaximumPolicy.StrategicReserveTargetRatio) {
		return fmt.Errorf(
			"strategic Reserve target ratio %s is outside mandate range [%s, %s]",
			policy.StrategicReserveTargetRatio,
			mandate.MinimumPolicy.StrategicReserveTargetRatio,
			mandate.MaximumPolicy.StrategicReserveTargetRatio,
		)
	}
	if policy.InsuranceTargetRatio.LT(mandate.MinimumPolicy.InsuranceTargetRatio) ||
		policy.InsuranceTargetRatio.GT(mandate.MaximumPolicy.InsuranceTargetRatio) {
		return fmt.Errorf(
			"insurance target ratio %s is outside mandate range [%s, %s]",
			policy.InsuranceTargetRatio,
			mandate.MinimumPolicy.InsuranceTargetRatio,
			mandate.MaximumPolicy.InsuranceTargetRatio,
		)
	}
	if policy.ValidatorBlockRewardTarget.LT(mandate.MinimumPolicy.ValidatorBlockRewardTarget) ||
		policy.ValidatorBlockRewardTarget.GT(mandate.MaximumPolicy.ValidatorBlockRewardTarget) {
		return fmt.Errorf(
			"validator block reward target %s is outside mandate range [%s, %s]",
			policy.ValidatorBlockRewardTarget,
			mandate.MinimumPolicy.ValidatorBlockRewardTarget,
			mandate.MaximumPolicy.ValidatorBlockRewardTarget,
		)
	}
	if policy.OracleBlockRewardTarget.LT(mandate.MinimumPolicy.OracleBlockRewardTarget) ||
		policy.OracleBlockRewardTarget.GT(mandate.MaximumPolicy.OracleBlockRewardTarget) {
		return fmt.Errorf(
			"oracle block reward target %s is outside mandate range [%s, %s]",
			policy.OracleBlockRewardTarget,
			mandate.MinimumPolicy.OracleBlockRewardTarget,
			mandate.MaximumPolicy.OracleBlockRewardTarget,
		)
	}

	return nil
}
