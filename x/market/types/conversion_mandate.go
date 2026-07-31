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

// DelegatesTobinPower reports whether the appointment carries any Tobin-raise
// authority. A nil cap is read as zero so a mandate appointed without the
// field — a capacity-only committee — validates and simply has no Tobin power.
func (conversionMandate ConversionMandate) DelegatesTobinPower() bool {
	return !conversionMandate.MaxTobinTax.IsNil() && !conversionMandate.MaxTobinTax.IsZero()
}

// Validate validates either a disabled mandate or one complete bounded
// committee appointment.
//
// The bound checks sit inside the enabled branch rather than ahead of it,
// unlike Treasury's equivalent: a market pool must be positive, so the zero
// bounds a disabled mandate carries are the one pair ConversionPolicy.Validate
// refuses. Envelope.Validate is reached through the field because this method
// shadows the promoted one.
func (conversionMandate ConversionMandate) Validate() error {
	if err := conversionMandate.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", ConversionMandateLabel, err)
	}

	if conversionMandate.IsDisabled() {
		if !conversionMandate.MinimumPolicy.IsZero() || !conversionMandate.MaximumPolicy.IsZero() {
			return errors.New("disabled conversion mandate must use identical zero bounds")
		}
		if conversionMandate.DelegatesTobinPower() {
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
	// A zero cap is a valid enabled mandate: it delegates the corridor and no
	// Tobin power. A set cap must itself be a chargeable Tobin rate.
	if conversionMandate.DelegatesTobinPower() {
		if err := ValidateTobinTax(conversionMandate.MaxTobinTax); err != nil {
			return fmt.Errorf("invalid conversion mandate Tobin cap: %w", err)
		}
	}

	return nil
}

// IsActive reports whether the appointment can actually authorize a capacity
// update at height: inside its window and still denominated in the live pool's
// unit.
//
// Being inside the window is not enough on its own. A reference re-point
// rebases the pool and deliberately leaves the corridor in the unit governance
// appointed it in, so an otherwise-live appointment can be stranded — and the
// moment a committee musters is the worst possible time to learn that. Anything
// reporting a mandate's standing has to answer the question a caller is really
// asking, which is whether the fast path is there, not whether the window is
// open.
//
// This method shadows the promoted Envelope.IsActive the way Validate shadows
// Envelope.Validate, and the different signature is the point: a capacity
// mandate cannot answer for itself without the live pool, so asking without it
// does not compile. Window-only semantics remain reachable through the
// Envelope field, which is also how this method reads them.
func (conversionMandate ConversionMandate) IsActive(livePolicy ConversionPolicy, height uint64) bool {
	if !conversionMandate.Envelope.IsActive(height) {
		return false
	}

	return conversionMandate.MinimumPolicy.BasePool.Denom == livePolicy.BasePool.Denom
}

// ValidatePolicy checks one committee candidate against every mandate bound.
// Bounds are inclusive: governance naming a depth as the corridor edge means
// the committee may set exactly that depth.
//
// The denomination check is what strands a mandate across a reference change.
// Bounds keep the unit governance approved them in, so after a re-pointing a
// candidate in the new unit falls outside the corridor and one in the old unit
// is refused as a re-denomination, until governance re-appoints the committee
// with bounds it has actually reviewed.
func (conversionMandate ConversionMandate) ValidatePolicy(policy ConversionPolicy) error {
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

	return nil
}

// ValidateTobinRaise checks one committee Tobin candidate against the
// mandate's cap and the denomination's current effective rate. The committee
// only raises: the floor is wherever the rate already stands, so the delegated
// power widens the oracle-staleness buffer and never narrows it. Lowering or
// removing an override is governance's restore path. Both boundaries are
// inclusive, matching the corridor: pinning the current rate and raising
// exactly to the cap are both authorized.
//
// Unlike the corridor, the cap is a dimensionless rate, so a reference
// re-point that strands the depth bounds leaves this power usable: containment
// must not die with a unit change it has nothing to do with.
func (conversionMandate ConversionMandate) ValidateTobinRaise(effective, candidate math.LegacyDec) error {
	if !conversionMandate.DelegatesTobinPower() {
		return errors.New("conversion mandate delegates no Tobin power")
	}
	if err := ValidateTobinTax(candidate); err != nil {
		return err
	}
	if candidate.LT(effective) {
		return fmt.Errorf(
			"tobin tax %s is below the current effective rate %s: the committee only raises",
			candidate,
			effective,
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
