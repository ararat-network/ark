package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
)

// Default capacity values
const (
	DefaultPoolRecoveryPeriod = chain.BlocksPerDay // 14,400
)

var DefaultBasePool = sdk.NewDecCoin(
	chain.SDRBaseDenom,
	chain.NativeBaseAmount(1_000_000),
) // 1,000,000 SDR = 1,000,000,000,000,000,000,000,000 asdr

// DefaultConversionPolicy returns the launch conversion-capacity pair.
func DefaultConversionPolicy() ConversionPolicy {
	return ConversionPolicy{
		BasePool:           DefaultBasePool,
		PoolRecoveryPeriod: DefaultPoolRecoveryPeriod,
	}
}

// ZeroConversionPolicy returns the canonical zero pair a disabled mandate carries
// as both bounds.
//
// It is deliberately not DefaultConversionPolicy: Treasury can use its launch
// policy as the disabled sentinel because every Treasury lever legitimately
// starts at zero, whereas a market pool must always be positive. Zero is
// therefore unmistakably "no delegation" here, and it is the one capacity value
// Validate rejects.
func ZeroConversionPolicy() ConversionPolicy {
	return ConversionPolicy{BasePool: sdk.DecCoin{Amount: math.LegacyZeroDec()}}
}

// Validate checks one complete live capacity pair.
//
// Depth must be positive and its square representable for the same reasons the
// params check enforced before capacity moved out: a zero depth cannot be
// scaled from when depth changes, and the constant product is depth squared.
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
	if policy.PoolRecoveryPeriod == 0 {
		return fmt.Errorf("pool recovery period must be positive, is %d", policy.PoolRecoveryPeriod)
	}

	return nil
}

// Equal reports whether two capacity pairs carry the same unit, depth, and
// recovery period. A nil depth is tolerated so the disabled sentinel and a
// genesis-supplied empty policy compare without panicking.
func (policy ConversionPolicy) Equal(other ConversionPolicy) bool {
	if policy.PoolRecoveryPeriod != other.PoolRecoveryPeriod {
		return false
	}
	if policy.BasePool.Denom != other.BasePool.Denom {
		return false
	}
	if policy.BasePool.Amount.IsNil() || other.BasePool.Amount.IsNil() {
		return policy.BasePool.Amount.IsNil() == other.BasePool.Amount.IsNil()
	}

	return policy.BasePool.Amount.Equal(other.BasePool.Amount)
}

// IsZero reports whether the pair carries no unit, no depth, and no recovery
// period. An unset depth counts as zero, so a mandate whose bounds arrived as
// empty JSON objects is still recognised as the disabled sentinel rather than
// panicking on a nil decimal.
func (policy ConversionPolicy) IsZero() bool {
	if policy.BasePool.Denom != "" || policy.PoolRecoveryPeriod != 0 {
		return false
	}

	return policy.BasePool.Amount.IsNil() || policy.BasePool.Amount.IsZero()
}
