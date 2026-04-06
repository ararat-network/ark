package types

import (
	"fmt"

	"cosmossdk.io/math"

	core "noah/types"
)

// Default parameter values
const (
	DefaultPoolRecoveryPeriod = core.BlocksPerDay // 14,400
)

// Default parameter values
var (
	DefaultBasePool           = math.LegacyNewDec(1_000_000 * core.MicroUnit) // 1,000,000sdr = 1,000,000,000,000usdr
	DefaultMinStabilitySpread = math.LegacyNewDecWithPrec(2, 2)               // 2%
)

// DefaultParams creates default market module parameters
func DefaultParams() Params {
	return Params{
		BasePool:           DefaultBasePool,
		PoolRecoveryPeriod: DefaultPoolRecoveryPeriod,
		MinStabilitySpread: DefaultMinStabilitySpread,
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if p.BasePool.IsNegative() {
		return fmt.Errorf("base pool must be positive or zero, is %s", p.BasePool)
	}
	if p.PoolRecoveryPeriod == 0 {
		return fmt.Errorf("pool recovery period must be positive, is %d", p.PoolRecoveryPeriod)
	}
	if p.MinStabilitySpread.IsNegative() || p.MinStabilitySpread.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min stability spread must be in [0, 1], is %s", p.MinStabilitySpread)
	}
	return nil
}
