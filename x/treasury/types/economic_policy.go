package types

import (
	"fmt"
	"math/big"

	"cosmossdk.io/math"
)

// MaxBlockRewardTarget bounds either per-block reward target, and exists to
// make the window accrual safe by inspection rather than by projection.
//
// Reward targets accumulate one block at a time for a whole funding window, so
// without a ceiling their sum is bounded only by the integer type — which put
// the burden on a validation that had to re-prove, at every write of params or
// policy, that the current state plus this policy over the remaining blocks
// would still fit. A ceiling on the field itself replaces that: with both
// targets under 2^128 and the window under MaxRewardFundingWindow, a whole
// window accrues at most 2^161, leaving ninety-five bits of headroom under the
// Int limit. The cap is far past economic reality — 2^128 anoah is on the order
// of 10^20 NOAH in a single block — so it constrains nothing governance would
// ever want, and refuses at the write what would otherwise fail a block.
var MaxBlockRewardTarget = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))

// MaxExposureWeight bounds each indicator weight. Like MaxBlockRewardTarget it
// is a domain cap rather than a projection: the composite is folded and applied
// inside block hooks, where a checked arithmetic error and a panic are the same
// outcome, so the defence has to be refusing the value at the write. A million
// is far past any defensible setting — a liability-ratio weight of one already
// means a fund holding as much as the liability itself doubles its target — and
// exists to keep the arithmetic provably in range, not to express a view.
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

// Validate performs context-free validation of one reversible policy.
//
// Three tables, because the levers fall into three shapes and the shape is what
// the bound means. A share is a fraction of something, so exceeding one is not
// a large value but an incoherent one. A reward target is an absolute per-block
// amount whose ceiling exists so a whole window's accrual stays in range. A
// weight is a multiplier on an indicator, unbounded in principle and capped
// only to keep the composite representable. A new lever joins whichever table
// states its bound, or brings a fourth.
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

	// The exposure weights. Each states how much extra capital a unit of its
	// indicator should demand, so zero is meaningful — it is the launch value,
	// and the way one indicator is switched off without disturbing the others —
	// and the only ceiling is MaxExposureWeight, the domain cap that keeps the
	// composite in range. What a weight is safe inside is the multiplier cap and
	// step limit, which live in Params and stay with governance.
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

// FundTargets sizes every fund target against one liability basis.
//
// Rounding is up at both steps, because a target sizes a requirement rather
// than a payment: MulRoundUp keeps the product from shedding a fraction of a
// base unit, and Ceil turns whatever fraction remains into the whole unit a
// fund has to actually hold. A target rounded down would call a fund full while
// it was a base unit short of its own rule.
//
// The basis is taken rather than derived because one policy implies two
// different sets, and which one is correct depends on the question. What bounds
// a committee act is sized against gross liability, since the committee can
// re-issue the paper its own fund holds; what the next expansion fills is sized
// against net, since no claim arrives from that paper (D67).
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
