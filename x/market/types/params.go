package types

import (
	"fmt"

	"gopkg.in/yaml.v2"
	marketv1 "noah/api/noah/market/v1"
	"noah/types"

	"cosmossdk.io/math"
)

// Default parameter values
var (
	DefaultBasePool           = math.LegacyNewDec(1000000 * types.MicroUnit) // 1000,000sdr = 1000,000,000,000usdr
	DefaultPoolRecoveryPeriod = types.BlocksPerDay                           // 14,400
	DefaultMinStabilitySpread = math.LegacyNewDecWithPrec(2, 2)              // 2%
)

// DefaultParams creates default market module parameters
func DefaultParams() *marketv1.Params {
	return &marketv1.Params{
		BasePool:           DefaultBasePool.String(),
		PoolRecoveryPeriod: DefaultPoolRecoveryPeriod,
		MinStabilitySpread: DefaultMinStabilitySpread.String(),
	}
}

func ParamsString(p *marketv1.Params) string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

// Validate a set of params
func ValidateParams(p *marketv1.Params) error {
	basePool, err := math.LegacyNewDecFromStr(p.BasePool)
	if err != nil {
		return fmt.Errorf("cannot convert string: %w", err)
	}
	minStabilitySpread, err := math.LegacyNewDecFromStr(p.MinStabilitySpread)
	if err != nil {
		return fmt.Errorf("cannot convert string: %w", err)
	}
	if basePool.IsNegative() {
		return fmt.Errorf("mint base pool should be positive or zero, is %s", p.BasePool)
	}
	if p.PoolRecoveryPeriod == 0 {
		return fmt.Errorf("pool recovery period should be positive, is %d", p.PoolRecoveryPeriod)
	}
	if minStabilitySpread.IsNegative() || minStabilitySpread.GT(math.LegacyOneDec()) {
		return fmt.Errorf("market minimum stability spead should be a value between [0,1], is %s", p.MinStabilitySpread)
	}

	return nil
}

func validateBasePool(i any) error {
	v, ok := i.(math.LegacyDec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("mint base pool must be positive or zero: %s", v)
	}

	return nil
}

func validatePoolRecoveryPeriod(i any) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v <= 0 {
		return fmt.Errorf("pool recovery period must be positive: %d", v)
	}

	return nil
}

func validateMinStabilitySpread(i any) error {
	v, ok := i.(math.LegacyDec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("min spread must be positive or zero: %s", v)
	}

	if v.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min spread is too large: %s", v)
	}

	return nil
}
