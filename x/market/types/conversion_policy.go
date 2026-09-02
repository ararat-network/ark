package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
)

// Default conversion values
const (
	DefaultPoolRecoveryPeriod = chain.BlocksPerDay // 14,400

	// MaxPoolRecoveryPeriod bounds the recovery span at a year. The period is a
	// divisor: each block returns delta/period toward zero, so a span long
	// enough that the quotient truncates to nothing does not slow recovery, it
	// stops it, and the pool keeps an imbalance for good. Since the delta is
	// what makes sustained one-way flow expensive (D3), that turns the spread's
	// memory into a permanent state rather than a decaying one.
	MaxPoolRecoveryPeriod = chain.BlocksPerYear
)

var (
	DefaultBasePool = sdk.NewDecCoin(
		chain.XDRBaseDenom,
		chain.NativeBaseAmount(1_000_000),
	) // 1,000,000 XDR = 1,000,000,000,000,000,000,000,000 axdr
	DefaultMinStabilitySpread = math.LegacyNewDecWithPrec(2, 2) // 2%
)

// DefaultConversionPolicy returns the launch conversion dials.
func DefaultConversionPolicy() ConversionPolicy {
	return ConversionPolicy{
		BasePool:           DefaultBasePool,
		PoolRecoveryPeriod: DefaultPoolRecoveryPeriod,
		MinStabilitySpread: DefaultMinStabilitySpread,
	}
}

// ZeroConversionPolicy returns the canonical zero policy a disabled mandate
// carries as both bounds.
func ZeroConversionPolicy() ConversionPolicy {
	return ConversionPolicy{
		BasePool:           sdk.DecCoin{Amount: math.LegacyZeroDec()},
		MinStabilitySpread: math.LegacyZeroDec(),
	}
}

// Validate checks one complete live conversion policy.
func (policy ConversionPolicy) Validate() error {
	if policy.BasePool.Amount.IsNil() {
		return errors.New("base pool amount must be set")
	}
	if err := policy.BasePool.Validate(); err != nil {
		return fmt.Errorf("invalid base pool: %w", err)
	}
	if !policy.BasePool.IsPositive() {
		return fmt.Errorf("base pool must be positive, is %s", policy.BasePool)
	}
	if _, err := decimal.Mul(policy.BasePool.Amount, policy.BasePool.Amount); err != nil {
		return fmt.Errorf("base pool square must be representable: %w", err)
	}
	if policy.PoolRecoveryPeriod == 0 || policy.PoolRecoveryPeriod > MaxPoolRecoveryPeriod {
		return fmt.Errorf(
			"pool recovery period must be between one and %d, is %d",
			MaxPoolRecoveryPeriod,
			policy.PoolRecoveryPeriod,
		)
	}
	if policy.MinStabilitySpread.IsNil() {
		return errors.New("min stability spread must be set")
	}
	if policy.MinStabilitySpread.IsNegative() || policy.MinStabilitySpread.GT(math.LegacyOneDec()) {
		return fmt.Errorf(
			"min stability spread must be in [0, 1], is %s",
			policy.MinStabilitySpread,
		)
	}

	return nil
}

// Equal reports whether two policies carry the same unit, depth, recovery
// period, and spread floor. Nil decimals are tolerated rather than compared, so
// a policy that has not been through Validate — a freshly decoded message, a
// half-built literal — compares without panicking.
func (policy ConversionPolicy) Equal(other ConversionPolicy) bool {
	if policy.PoolRecoveryPeriod != other.PoolRecoveryPeriod {
		return false
	}
	if policy.BasePool.Denom != other.BasePool.Denom {
		return false
	}
	if !equalOrBothNil(policy.BasePool.Amount, other.BasePool.Amount) {
		return false
	}

	return equalOrBothNil(policy.MinStabilitySpread, other.MinStabilitySpread)
}

// equalOrBothNil compares two decimals that may be unset, treating a nil pair as
// equal rather than panicking on the comparison.
func equalOrBothNil(a, b math.LegacyDec) bool {
	if a.IsNil() || b.IsNil() {
		return a.IsNil() == b.IsNil()
	}

	return a.Equal(b)
}

// IsZero reports whether the policy carries no unit, no depth, no recovery
// period, and no spread floor — the exact payload a disabled mandate holds.
//
// An unset decimal reads as false rather than as zero. Nil is not a value the
// caller wrote, and the sentinel exists to have one spelling, so a policy that
// omits a decimal is not the zero policy — it is not yet a policy. Answering it
// leniently would matter, because this is the whole judgment the disabled
// branch gets: a zero base pool is the one policy ConversionPolicy.Validate
// refuses, so nothing else validates the payload there.
func (policy ConversionPolicy) IsZero() bool {
	if policy.BasePool.Denom != "" || policy.PoolRecoveryPeriod != 0 {
		return false
	}
	if policy.BasePool.Amount.IsNil() || policy.MinStabilitySpread.IsNil() {
		return false
	}

	return policy.BasePool.Amount.IsZero() && policy.MinStabilitySpread.IsZero()
}
