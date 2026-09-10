package types

import (
	"fmt"
	"math/big"

	"cosmossdk.io/math"
)

// MaxBlockRewardTarget caps each per-block target at 2^128. Combined with MaxRewardFundingWindow,
// accrual remains below 2^161 with 95 bits of Int headroom. Validate at writes so block-time sums
// cannot receive unbounded policy inputs.
var MaxBlockRewardTarget = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))

// MaxExposureWeight caps each indicator weight at one million before block-hook composition. It is
// an arithmetic domain bound; governance's multiplier cap and step limit constrain delegated
// effects.
var MaxExposureWeight = math.LegacyNewDec(1_000_000)

// DefaultEconomicPolicy returns the disabled launch policy values.
func DefaultEconomicPolicy() EconomicPolicy {
	return EconomicPolicy{
		ValidatorBlockRewardTarget:  math.ZeroInt(),
		OracleBlockRewardTarget:     math.ZeroInt(),
		RedemptionBufferTargetRatio: math.LegacyZeroDec(),
		StrategicReserveTargetRatio: math.LegacyZeroDec(),
		InsuranceTargetRatio:        math.LegacyZeroDec(),
		LiabilityRatioWeight:        math.LegacyZeroDec(),
		VolatilityWeight:            math.LegacyZeroDec(),
		FlowWeight:                  math.LegacyZeroDec(),
	}
}

// Validate checks policy fields by domain: shares in [0, 1], bounded absolute reward targets, and
// bounded non-negative indicator weights. Validation depends on the candidate alone.
func (policy EconomicPolicy) Validate() error {
	for _, share := range []struct {
		name  string
		value math.LegacyDec
	}{
		{"RedemptionBufferTargetRatio", policy.RedemptionBufferTargetRatio},
		{"StrategicReserveTargetRatio", policy.StrategicReserveTargetRatio},
		{"InsuranceTargetRatio", policy.InsuranceTargetRatio},
	} {
		if share.value.IsNil() {
			return fmt.Errorf("treasury parameter %s must be set", share.name)
		}
		if !share.value.IsInValidRange() {
			return fmt.Errorf("treasury parameter %s is not representable", share.name)
		}
		if share.value.IsNegative() || share.value.GT(math.LegacyOneDec()) {
			return fmt.Errorf(
				"treasury parameter %s must be between zero and one: %s",
				share.name,
				share.value,
			)
		}
	}

	// Representability goes unchecked here alone: Int has no invalid range, and
	// the ceiling does that work instead.
	for _, target := range []struct {
		name  string
		value math.Int
	}{
		{"ValidatorBlockRewardTarget", policy.ValidatorBlockRewardTarget},
		{"OracleBlockRewardTarget", policy.OracleBlockRewardTarget},
	} {
		if target.value.IsNil() {
			return fmt.Errorf("treasury parameter %s must be set", target.name)
		}
		if target.value.IsNegative() || target.value.GT(MaxBlockRewardTarget) {
			return fmt.Errorf(
				"treasury parameter %s must be between zero and %s: %s",
				target.name,
				MaxBlockRewardTarget,
				target.value,
			)
		}
	}

	// Zero exposure weights disable individual indicators. MaxExposureWeight bounds arithmetic;
	// Params retains governance control of the multiplier cap and step limit.
	for _, weight := range []struct {
		name  string
		value math.LegacyDec
	}{
		{"LiabilityRatioWeight", policy.LiabilityRatioWeight},
		{"VolatilityWeight", policy.VolatilityWeight},
		{"FlowWeight", policy.FlowWeight},
	} {
		if weight.value.IsNil() {
			return fmt.Errorf("treasury parameter %s must be set", weight.name)
		}
		if !weight.value.IsInValidRange() {
			return fmt.Errorf("treasury parameter %s is not representable", weight.name)
		}
		if weight.value.IsNegative() || weight.value.GT(MaxExposureWeight) {
			return fmt.Errorf(
				"treasury parameter %s must be between zero and %s: %s",
				weight.name,
				MaxExposureWeight,
				weight.value,
			)
		}
	}

	return nil
}

// FundTargetSet is the three fund targets one liability basis implies.
type FundTargetSet struct {
	Buffer    math.Int
	Reserve   math.Int
	Insurance math.Int
}

// FundTargets uses MulRoundUp then Ceil so fractional requirements never report an underfunded fund
// as full. Callers supply nominal liability for committee bounds or net liability for expansion
// gaps.
func (policy EconomicPolicy) FundTargets(liabilityNoah math.LegacyDec) FundTargetSet {
	return FundTargetSet{
		Buffer:    policy.RedemptionBufferTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt(),
		Reserve:   policy.StrategicReserveTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt(),
		Insurance: policy.InsuranceTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt(),
	}
}

// Equal reports whether two economic policies contain identical values.
func (policy EconomicPolicy) Equal(other EconomicPolicy) bool {
	return policy.ValidatorBlockRewardTarget.Equal(other.ValidatorBlockRewardTarget) &&
		policy.OracleBlockRewardTarget.Equal(other.OracleBlockRewardTarget) &&
		policy.RedemptionBufferTargetRatio.Equal(other.RedemptionBufferTargetRatio) &&
		policy.StrategicReserveTargetRatio.Equal(other.StrategicReserveTargetRatio) &&
		policy.InsuranceTargetRatio.Equal(other.InsuranceTargetRatio) &&
		policy.LiabilityRatioWeight.Equal(other.LiabilityRatioWeight) &&
		policy.VolatilityWeight.Equal(other.VolatilityWeight) &&
		policy.FlowWeight.Equal(other.FlowWeight)
}

// IsZero reports whether every policy value is zero.
func (policy EconomicPolicy) IsZero() bool {
	return policy.ValidatorBlockRewardTarget.IsZero() &&
		policy.OracleBlockRewardTarget.IsZero() &&
		policy.RedemptionBufferTargetRatio.IsZero() &&
		policy.StrategicReserveTargetRatio.IsZero() &&
		policy.InsuranceTargetRatio.IsZero() &&
		policy.LiabilityRatioWeight.IsZero() &&
		policy.VolatilityWeight.IsZero() &&
		policy.FlowWeight.IsZero()
}
