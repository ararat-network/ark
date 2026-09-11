// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/market/types/params.go.
// Modified for Ark: split conversion policy, bounds, and checked arithmetic.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
)

// Default conversion values
const (
	DefaultPoolRecoveryPeriod = chain.BlocksPerDay // 14,400

	// MaxPoolRecoveryPeriod caps recovery at a chain year. Excessive divisors can truncate small
	// delta recovery to zero, leaving persistent spread pressure.
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

// IsZero recognises the exact disabled policy: no unit, zero depth, recovery, and spread. Unset
// decimals are invalid because this predicate is the disabled mandate's payload validation.
func (policy ConversionPolicy) IsZero() bool {
	if policy.BasePool.Denom != "" || policy.PoolRecoveryPeriod != 0 {
		return false
	}
	if policy.BasePool.Amount.IsNil() || policy.MinStabilitySpread.IsNil() {
		return false
	}

	return policy.BasePool.Amount.IsZero() && policy.MinStabilitySpread.IsZero()
}
