package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"ark/pkg/mandate"
)

// ConversionMandateLabel names the shared appointment envelope in
// conversion-capacity errors.
const ConversionMandateLabel = "conversion mandate"

// DefaultConversionMandate returns the canonical disabled committee mandate. The
// term may increase later while the mandate remains disabled.
func DefaultConversionMandate() ConversionMandate {
	return NewDisabledConversionMandate(0)
}

// NewDisabledConversionMandate returns a canonical disabled mandate at the
// supplied term.
func NewDisabledConversionMandate(term uint64) ConversionMandate {
	policy := ZeroConversionPolicy()

	return ConversionMandate{
		Envelope:      mandate.Disabled(term),
		MinimumPolicy: policy,
		MaximumPolicy: policy,
		MaxTobinTax:   math.LegacyZeroDec(),
	}
}

// Validate validates either a disabled mandate or one complete bounded
// committee appointment.
//
// The bound checks sit inside the enabled branch rather than ahead of it,
// unlike Treasury's equivalent: a market pool must be positive, so the zero
// bounds a disabled mandate carries are the one pair ConversionPolicy.Validate
// refuses. The disabled branch therefore carries the whole judgment of the
// sentinel, which is why an unset decimal fails it: every sibling mandate
// refuses one through the payload validation it runs before branching, and here
// that refusal lives in ConversionPolicy.IsZero and in the Tobin cap's own
// check. Envelope.Validate is reached through the field because this method
// shadows the promoted one.
func (conversionMandate ConversionMandate) Validate() error {
	if err := conversionMandate.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", ConversionMandateLabel, err)
	}

	if conversionMandate.IsDisabled() {
		if !conversionMandate.MinimumPolicy.IsZero() || !conversionMandate.MaximumPolicy.IsZero() {
			return errors.New("disabled conversion mandate must use identical zero bounds")
		}
		if conversionMandate.MaxTobinTax.IsNil() || !conversionMandate.MaxTobinTax.IsZero() {
			return errors.New("disabled conversion mandate must carry a zero Tobin cap")
		}

		return nil
	}

	if err := conversionMandate.MinimumPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid conversion minimum: %w", err)
	}
	if err := conversionMandate.MaximumPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid conversion maximum: %w", err)
	}
	// One corridor cannot span two units. Bounds in different denominations
	// would make the range meaningless and could never both match a candidate.
	if conversionMandate.MinimumPolicy.BasePool.Denom != conversionMandate.MaximumPolicy.BasePool.Denom {
		return fmt.Errorf(
			"conversion bounds must share one denomination, are %s and %s",
			conversionMandate.MinimumPolicy.BasePool.Denom,
			conversionMandate.MaximumPolicy.BasePool.Denom,
		)
	}
	if conversionMandate.MinimumPolicy.BasePool.Amount.GT(conversionMandate.MaximumPolicy.BasePool.Amount) {
		return fmt.Errorf(
			"minimum base pool %s exceeds maximum %s",
			conversionMandate.MinimumPolicy.BasePool.Amount,
			conversionMandate.MaximumPolicy.BasePool.Amount,
		)
	}
	if conversionMandate.MinimumPolicy.PoolRecoveryPeriod > conversionMandate.MaximumPolicy.PoolRecoveryPeriod {
		return fmt.Errorf(
			"minimum pool recovery period %d exceeds maximum %d",
			conversionMandate.MinimumPolicy.PoolRecoveryPeriod,
			conversionMandate.MaximumPolicy.PoolRecoveryPeriod,
		)
	}
	if conversionMandate.MinimumPolicy.MinStabilitySpread.GT(conversionMandate.MaximumPolicy.MinStabilitySpread) {
		return fmt.Errorf(
			"minimum stability spread %s exceeds maximum %s",
			conversionMandate.MinimumPolicy.MinStabilitySpread,
			conversionMandate.MaximumPolicy.MinStabilitySpread,
		)
	}
	if err := ValidateTobinTax(conversionMandate.MaxTobinTax); err != nil {
		return fmt.Errorf("invalid conversion mandate Tobin cap: %w", err)
	}

	return nil
}

// IsActive reports whether the appointment can actually authorize a capacity
// update at height: inside its window and still denominated in the live pool's
// unit.
func (conversionMandate ConversionMandate) IsActive(livePolicy ConversionPolicy, height uint64) bool {
	if !conversionMandate.Envelope.IsActive(height) {
		return false
	}

	return conversionMandate.MinimumPolicy.BasePool.Denom == livePolicy.BasePool.Denom
}

// ValidatePolicy checks one committee candidate against every mandate bound.
// Bounds are inclusive: governance naming a depth as the corridor edge means
// the committee may set exactly that depth.
func (conversionMandate ConversionMandate) ValidatePolicy(policy ConversionPolicy) error {
	if policy.BasePool.Amount.IsNil() {
		return errors.New("base pool amount must be set")
	}
	if policy.MinStabilitySpread.IsNil() {
		return errors.New("min stability spread must be set")
	}
	if policy.BasePool.Denom != conversionMandate.MinimumPolicy.BasePool.Denom {
		return fmt.Errorf(
			"base pool denomination %s is outside the mandate, which bounds %s",
			policy.BasePool.Denom,
			conversionMandate.MinimumPolicy.BasePool.Denom,
		)
	}
	if policy.BasePool.Amount.LT(conversionMandate.MinimumPolicy.BasePool.Amount) ||
		policy.BasePool.Amount.GT(conversionMandate.MaximumPolicy.BasePool.Amount) {
		return fmt.Errorf(
			"base pool %s is outside mandate range [%s, %s]",
			policy.BasePool.Amount,
			conversionMandate.MinimumPolicy.BasePool.Amount,
			conversionMandate.MaximumPolicy.BasePool.Amount,
		)
	}
	if policy.PoolRecoveryPeriod < conversionMandate.MinimumPolicy.PoolRecoveryPeriod ||
		policy.PoolRecoveryPeriod > conversionMandate.MaximumPolicy.PoolRecoveryPeriod {
		return fmt.Errorf(
			"pool recovery period %d is outside mandate range [%d, %d]",
			policy.PoolRecoveryPeriod,
			conversionMandate.MinimumPolicy.PoolRecoveryPeriod,
			conversionMandate.MaximumPolicy.PoolRecoveryPeriod,
		)
	}
	if policy.MinStabilitySpread.LT(conversionMandate.MinimumPolicy.MinStabilitySpread) ||
		policy.MinStabilitySpread.GT(conversionMandate.MaximumPolicy.MinStabilitySpread) {
		return fmt.Errorf(
			"min stability spread %s is outside mandate range [%s, %s]",
			policy.MinStabilitySpread,
			conversionMandate.MinimumPolicy.MinStabilitySpread,
			conversionMandate.MaximumPolicy.MinStabilitySpread,
		)
	}

	return nil
}

// ValidateTobinCandidate checks one committee Tobin candidate against the band
// the mandate delegates: at least the chain-wide default and at most the
// mandate's cap. Both boundaries are inclusive, matching the corridor.
func (conversionMandate ConversionMandate) ValidateTobinCandidate(defaultTobinTax, candidate math.LegacyDec) error {
	if conversionMandate.MaxTobinTax.IsZero() {
		return errors.New("conversion mandate delegates no Tobin power")
	}
	if err := ValidateTobinTax(candidate); err != nil {
		return err
	}
	if candidate.LT(defaultTobinTax) {
		return fmt.Errorf(
			"tobin tax %s is below the default rate %s: only governance sets a denomination lower",
			candidate,
			defaultTobinTax,
		)
	}
	if candidate.GT(conversionMandate.MaxTobinTax) {
		return fmt.Errorf(
			"tobin tax %s exceeds the mandate cap %s",
			candidate,
			conversionMandate.MaxTobinTax,
		)
	}

	return nil
}
