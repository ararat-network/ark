package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

const (
	DefaultReferenceDenom      = chain.XDRBaseDenom
	DefaultRewardFundingWindow = chain.BlocksPerWeek

	// DefaultExposureRefreshPeriodBlocks recomputes the multiplier hourly. The
	// cadence is a judgement about the fastest stress worth tracking: a
	// Terra-speed run unfolds over days, so an hour resolves it many times over,
	// while a shorter period spends state writes resolving noise the decay
	// windows are there to absorb.
	DefaultExposureRefreshPeriodBlocks = chain.BlocksPerHour

	// MaxRewardFundingWindow bounds how many blocks one funding window accrues
	// over. Together with EconomicPolicy's MaxBlockRewardTarget it is what
	// makes the accrual safe by inspection: the two ceilings multiply to a
	// whole-window total ninety-five bits under the Int limit, so no sequence
	// of blocks can overflow the running targets. A window of 2^32 blocks is
	// some seven centuries at this chain's block time, so the bound refuses
	// only values that were never a schedule.
	MaxRewardFundingWindow = 1 << 32

	// MaxExposureRefreshPeriodBlocks bounds the cadence at a year. A period
	// longer than the series it reads is not a schedule, and one no block can
	// reach would freeze the multiplier at its stored value with no way to
	// observe that it had.
	MaxExposureRefreshPeriodBlocks = chain.BlocksPerYear
)

var (
	// MaxExposureMultiplierCap bounds both the multiplier ceiling and the
	// per-update step. It is a domain cap with orders of magnitude of headroom
	// rather than a projection over live state: the composite is folded and
	// applied inside block hooks, where a checked arithmetic error and a panic
	// are the same outcome — the block fails — so the defence has to be
	// refusing the value at the write, where a human is in the loop. A million
	// is far past any defensible setting.
	MaxExposureMultiplierCap = math.LegacyNewDec(1_000_000)

	// DefaultExposureVolatilityDecay retains variance with a half-life of about
	// 13,863 blocks — roughly a day. Volatility is a regime rather than an
	// event, and a day's memory outlasts a single bad hour without carrying a
	// month-old panic into today's target. It and DefaultExposureFlowDecay are
	// per-block EWMA retentions rather than half-lives because the fold applies
	// them directly, and deriving one from the other on chain would need a
	// logarithm LegacyDec does not have.
	DefaultExposureVolatilityDecay = math.LegacyMustNewDecFromStr("0.99995")
	// DefaultExposureFlowDecay retains flow pressure with a half-life of about
	// 602 blocks — roughly an hour. Flow is the fastest of the three signals and
	// the only one an actor can produce deliberately, so it forgets quickly: a
	// spike bought before a large redemption has to be sustained to matter.
	DefaultExposureFlowDecay = math.LegacyMustNewDecFromStr("0.99885")

	// DefaultExposureMultiplierCap ceilings the multiplier at four. With the
	// launch target ratios this keeps the sum of scaled targets below the whole
	// liability, so overflow burn still fires and expansions still contract
	// supply.
	DefaultExposureMultiplierCap = math.LegacyNewDec(4)
	// DefaultExposureMultiplierMaxStep moves the multiplier by at most a quarter
	// per update. At the default cadence a climb from one to the cap takes at
	// least twelve sustained hours, which no single period of any input can
	// force.
	DefaultExposureMultiplierMaxStep = math.LegacyMustNewDecFromStr("0.25")

	// DefaultBaseFeeTargetUtilisation opens the base-fee controller's rails
	// (dynamic fees, phase 2a). It holds price at half-full blocks —
	// EIP-1559's choice: symmetric headroom for demand in both directions.
	DefaultBaseFeeTargetUtilisation = math.LegacyMustNewDecFromStr("0.5")
	// DefaultBaseFeeAdjustmentRate moves the price at most 2.5% per block.
	// Sustained full blocks double it in about 28 blocks and sustained empty
	// ones halve it symmetrically — deliberately slow, per the launch plan of
	// observing before tightening.
	DefaultBaseFeeAdjustmentRate = math.LegacyMustNewDecFromStr("0.025")

	// DefaultMinBaseGasPrice is the resting price of gas in reference base
	// units per gas unit. The reference is atto-scaled (10^18 per XDR), and
	// 10^11 is Terra Classic's posted gas price in the same unit — 0.1018 usdr
	// per gas at six decimals — carried to eighteen: a 200k-gas transfer rests
	// at 0.02 XDR, the fee band Terra ran in production. Governance tunes from
	// here after observation.
	DefaultMinBaseGasPrice = math.LegacyNewDec(100_000_000_000)

	// MaxBaseGasPrice bounds the governance floor and is the saturation
	// ceiling the controller clamps the live price to. A clamp rather than an
	// error because the update runs in EndBlock, where a checked arithmetic
	// failure and a panic are the same outcome — congestion past representable
	// range holds here instead of failing a block. One whole reference unit
	// per gas unit — ten million times the resting price, an hour of
	// sustained full blocks at the default rate — is saturation in every
	// practical sense, and a uint64 of gas at this ceiling still sits some
	// twenty orders of magnitude inside the Dec domain, which is what keeps
	// the requirement arithmetic's overflow backstops unreachable.
	MaxBaseGasPrice = math.LegacyNewDec(1_000_000_000_000_000_000)
	// MaxBaseFeeAdjustmentRate caps the per-block move at a doubling.
	MaxBaseFeeAdjustmentRate = math.LegacyOneDec()
)

// DefaultParams returns the safe launch defaults for Treasury.
//
// The reference tax cap launches at one base unit — the tightest finite
// ceiling — rather than zero, which is the explicit uncapped sentinel: an
// unconfigured chain should clamp the transfer tax to dust, not leave it
// unbounded, and governance opts into either a real ceiling or none.
func DefaultParams() Params {
	return Params{
		ReferenceDenom:              DefaultReferenceDenom,
		ReferenceTaxCap:             math.OneInt(),
		TransferTaxRate:             math.LegacyZeroDec(),
		RewardFundingWindow:         DefaultRewardFundingWindow,
		VolatilityDecay:             DefaultExposureVolatilityDecay,
		FlowDecay:                   DefaultExposureFlowDecay,
		MultiplierCap:               DefaultExposureMultiplierCap,
		MultiplierMaxStep:           DefaultExposureMultiplierMaxStep,
		ExposureRefreshPeriodBlocks: DefaultExposureRefreshPeriodBlocks,
		BaseFeeTargetUtilisation:    DefaultBaseFeeTargetUtilisation,
		BaseFeeAdjustmentRate:       DefaultBaseFeeAdjustmentRate,
		MinBaseGasPrice:             DefaultMinBaseGasPrice,
	}
}

// Validate performs context-free validation of Treasury parameters.
// ReferenceDenom's identity with the protocol reference is validated by the
// keeper, and the indicator weights the exposure machinery below sizes live
// on EconomicPolicy, which validates them.
func (p Params) Validate() error {
	if err := chain.ValidatePricedDenom(p.ReferenceDenom); err != nil {
		return fmt.Errorf("treasury parameter ReferenceDenom is invalid: %w", err)
	}
	if p.ReferenceTaxCap.IsNil() {
		return errors.New("treasury parameter ReferenceTaxCap must be set")
	}
	if p.ReferenceTaxCap.IsNegative() {
		return fmt.Errorf("treasury parameter ReferenceTaxCap must not be negative: %s", p.ReferenceTaxCap)
	}
	if p.TransferTaxRate.IsNil() {
		return errors.New("treasury parameter TransferTaxRate must be set")
	}
	if !p.TransferTaxRate.IsInValidRange() {
		return errors.New("treasury parameter TransferTaxRate is not representable")
	}
	// A share of the transfer: zero disables the tax and one takes the whole
	// input, so both ends are legitimate and anything past one is incoherent.
	if p.TransferTaxRate.IsNegative() || p.TransferTaxRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf(
			"treasury parameter TransferTaxRate must be between zero and one: %s",
			p.TransferTaxRate,
		)
	}
	if p.RewardFundingWindow == 0 || p.RewardFundingWindow > MaxRewardFundingWindow {
		return fmt.Errorf(
			"treasury parameter RewardFundingWindow must be between one and %d: %d",
			uint64(MaxRewardFundingWindow),
			p.RewardFundingWindow,
		)
	}

	// Strictly below one at the top: a retention of one never forgets a sample,
	// so the series would carry its first observation forever and stop
	// describing current conditions. Zero is the opposite extreme and is
	// legitimate — it makes the series the latest sample alone.
	for _, decay := range []struct {
		name  string
		value math.LegacyDec
	}{
		{"ExposureVolatilityDecay", p.VolatilityDecay},
		{"ExposureFlowDecay", p.FlowDecay},
	} {
		if decay.value.IsNil() {
			return fmt.Errorf("treasury parameter %s must be set", decay.name)
		}
		if !decay.value.IsInValidRange() {
			return fmt.Errorf("treasury parameter %s is not representable", decay.name)
		}
		if decay.value.IsNegative() || decay.value.GTE(math.LegacyOneDec()) {
			return fmt.Errorf(
				"treasury parameter %s must be at least zero and below one: %s",
				decay.name,
				decay.value,
			)
		}
	}

	if p.MultiplierCap.IsNil() {
		return errors.New("treasury parameter ExposureMultiplierCap must be set")
	}
	if !p.MultiplierCap.IsInValidRange() {
		return errors.New("treasury parameter ExposureMultiplierCap is not representable")
	}
	// Floored at one, not at zero: the multiplier is a scaling of the current
	// sizing, so a cap below one would demand less capital than the unscaled
	// rule and turn a risk model into a discount.
	if p.MultiplierCap.LT(math.LegacyOneDec()) || p.MultiplierCap.GT(MaxExposureMultiplierCap) {
		return fmt.Errorf(
			"treasury parameter ExposureMultiplierCap must be between one and %s: %s",
			MaxExposureMultiplierCap,
			p.MultiplierCap,
		)
	}

	if p.MultiplierMaxStep.IsNil() {
		return errors.New("treasury parameter ExposureMultiplierMaxStep must be set")
	}
	if !p.MultiplierMaxStep.IsInValidRange() {
		return errors.New("treasury parameter ExposureMultiplierMaxStep is not representable")
	}
	// Strictly positive: a zero step admits no movement at all, which freezes
	// the multiplier at whatever it was last set to and makes every weight
	// below it dead configuration.
	if !p.MultiplierMaxStep.IsPositive() || p.MultiplierMaxStep.GT(MaxExposureMultiplierCap) {
		return fmt.Errorf(
			"treasury parameter ExposureMultiplierMaxStep must be greater than zero and at most %s: %s",
			MaxExposureMultiplierCap,
			p.MultiplierMaxStep,
		)
	}

	if p.ExposureRefreshPeriodBlocks == 0 ||
		p.ExposureRefreshPeriodBlocks > MaxExposureRefreshPeriodBlocks {
		return fmt.Errorf(
			"treasury parameter ExposureRefreshPeriodBlocks must be between one and %d: %d",
			MaxExposureRefreshPeriodBlocks,
			p.ExposureRefreshPeriodBlocks,
		)
	}

	if p.BaseFeeTargetUtilisation.IsNil() {
		return errors.New("treasury parameter BaseFeeTargetUtilisation must be set")
	}
	if !p.BaseFeeTargetUtilisation.IsInValidRange() {
		return errors.New("treasury parameter BaseFeeTargetUtilisation is not representable")
	}
	// Strictly positive: the update divides by the target, and a zero target
	// is a division the write must refuse rather than the block hook.
	if !p.BaseFeeTargetUtilisation.IsPositive() || p.BaseFeeTargetUtilisation.GT(math.LegacyOneDec()) {
		return fmt.Errorf(
			"treasury parameter BaseFeeTargetUtilisation must be above zero and at most one: %s",
			p.BaseFeeTargetUtilisation,
		)
	}

	if p.BaseFeeAdjustmentRate.IsNil() {
		return errors.New("treasury parameter BaseFeeAdjustmentRate must be set")
	}
	if !p.BaseFeeAdjustmentRate.IsInValidRange() {
		return errors.New("treasury parameter BaseFeeAdjustmentRate is not representable")
	}
	// Zero is legitimate: it disables the controller and holds the price at
	// the floor.
	if p.BaseFeeAdjustmentRate.IsNegative() || p.BaseFeeAdjustmentRate.GT(MaxBaseFeeAdjustmentRate) {
		return fmt.Errorf(
			"treasury parameter BaseFeeAdjustmentRate must be at least zero and at most %s: %s",
			MaxBaseFeeAdjustmentRate,
			p.BaseFeeAdjustmentRate,
		)
	}

	if p.MinBaseGasPrice.IsNil() {
		return errors.New("treasury parameter MinBaseGasPrice must be set")
	}
	if !p.MinBaseGasPrice.IsInValidRange() {
		return errors.New("treasury parameter MinBaseGasPrice is not representable")
	}
	if !p.MinBaseGasPrice.IsPositive() || p.MinBaseGasPrice.GT(MaxBaseGasPrice) {
		return fmt.Errorf(
			"treasury parameter MinBaseGasPrice must be above zero and at most %s: %s",
			MaxBaseGasPrice,
			p.MinBaseGasPrice,
		)
	}

	return nil
}
