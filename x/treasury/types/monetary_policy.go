package types

import (
	"errors"
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

// DefaultMonetaryPolicy returns the disabled launch policy values.
func DefaultMonetaryPolicy() MonetaryPolicy {
	return MonetaryPolicy{
		StabilityTaxRate:            math.LegacyZeroDec(),
		ValidatorBlockRewardTarget:  math.ZeroInt(),
		OracleBlockRewardTarget:     math.ZeroInt(),
		RedemptionBufferTargetRatio: math.LegacyZeroDec(),
		StrategicReserveTargetRatio: math.LegacyZeroDec(),
		InsuranceTargetRatio:        math.LegacyZeroDec(),
	}
}

// Validate performs context-free validation of one reversible policy.
func (policy MonetaryPolicy) Validate() error {
	if policy.StabilityTaxRate.IsNil() {
		return errors.New("treasury parameter StabilityTaxRate must be set")
	}
	if !policy.StabilityTaxRate.IsInValidRange() {
		return errors.New("treasury parameter StabilityTaxRate is not representable")
	}
	if policy.StabilityTaxRate.IsNegative() || policy.StabilityTaxRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter StabilityTaxRate must be between zero and one: %s", policy.StabilityTaxRate)
	}

	if policy.ValidatorBlockRewardTarget.IsNil() {
		return errors.New("treasury parameter ValidatorBlockRewardTarget must be set")
	}
	if policy.ValidatorBlockRewardTarget.IsNegative() {
		return fmt.Errorf("treasury parameter ValidatorBlockRewardTarget must be zero or positive: %s", policy.ValidatorBlockRewardTarget)
	}
	if policy.ValidatorBlockRewardTarget.GT(MaxBlockRewardTarget) {
		return fmt.Errorf(
			"treasury parameter ValidatorBlockRewardTarget must not exceed %s: %s",
			MaxBlockRewardTarget,
			policy.ValidatorBlockRewardTarget,
		)
	}
	if policy.OracleBlockRewardTarget.IsNil() {
		return errors.New("treasury parameter OracleBlockRewardTarget must be set")
	}
	if policy.OracleBlockRewardTarget.IsNegative() {
		return fmt.Errorf("treasury parameter OracleBlockRewardTarget must be zero or positive: %s", policy.OracleBlockRewardTarget)
	}
	if policy.OracleBlockRewardTarget.GT(MaxBlockRewardTarget) {
		return fmt.Errorf(
			"treasury parameter OracleBlockRewardTarget must not exceed %s: %s",
			MaxBlockRewardTarget,
			policy.OracleBlockRewardTarget,
		)
	}
	if policy.RedemptionBufferTargetRatio.IsNil() {
		return errors.New("treasury parameter RedemptionBufferTargetRatio must be set")
	}
	if !policy.RedemptionBufferTargetRatio.IsInValidRange() {
		return errors.New("treasury parameter RedemptionBufferTargetRatio is not representable")
	}
	if policy.RedemptionBufferTargetRatio.IsNegative() || policy.RedemptionBufferTargetRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter RedemptionBufferTargetRatio must be between zero and one: %s", policy.RedemptionBufferTargetRatio)
	}

	if policy.StrategicReserveTargetRatio.IsNil() {
		return errors.New("treasury parameter StrategicReserveTargetRatio must be set")
	}
	if !policy.StrategicReserveTargetRatio.IsInValidRange() {
		return errors.New("treasury parameter StrategicReserveTargetRatio is not representable")
	}
	if policy.StrategicReserveTargetRatio.IsNegative() || policy.StrategicReserveTargetRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter StrategicReserveTargetRatio must be between zero and one: %s", policy.StrategicReserveTargetRatio)
	}

	if policy.InsuranceTargetRatio.IsNil() {
		return errors.New("treasury parameter InsuranceTargetRatio must be set")
	}
	if !policy.InsuranceTargetRatio.IsInValidRange() {
		return errors.New("treasury parameter InsuranceTargetRatio is not representable")
	}
	if policy.InsuranceTargetRatio.IsNegative() || policy.InsuranceTargetRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter InsuranceTargetRatio must be between zero and one: %s", policy.InsuranceTargetRatio)
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
func (policy MonetaryPolicy) FundTargets(liabilityNoah math.LegacyDec) FundTargetSet {
	return FundTargetSet{
		Buffer:    policy.RedemptionBufferTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt(),
		Reserve:   policy.StrategicReserveTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt(),
		Insurance: policy.InsuranceTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt(),
	}
}

// Equal reports whether two monetary policies contain identical values.
func (policy MonetaryPolicy) Equal(other MonetaryPolicy) bool {
	return policy.StabilityTaxRate.Equal(other.StabilityTaxRate) &&
		policy.ValidatorBlockRewardTarget.Equal(other.ValidatorBlockRewardTarget) &&
		policy.OracleBlockRewardTarget.Equal(other.OracleBlockRewardTarget) &&
		policy.RedemptionBufferTargetRatio.Equal(other.RedemptionBufferTargetRatio) &&
		policy.StrategicReserveTargetRatio.Equal(other.StrategicReserveTargetRatio) &&
		policy.InsuranceTargetRatio.Equal(other.InsuranceTargetRatio)
}

// IsZero reports whether every policy value is zero.
func (policy MonetaryPolicy) IsZero() bool {
	return policy.StabilityTaxRate.IsZero() &&
		policy.ValidatorBlockRewardTarget.IsZero() &&
		policy.OracleBlockRewardTarget.IsZero() &&
		policy.RedemptionBufferTargetRatio.IsZero() &&
		policy.StrategicReserveTargetRatio.IsZero() &&
		policy.InsuranceTargetRatio.IsZero()
}
