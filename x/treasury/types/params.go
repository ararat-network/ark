package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

const (
	DefaultReferenceTaxCapDenom      = chain.SDRBaseDenom
	DefaultRewardFundingWindow       = chain.BlocksPerWeek
	DefaultTaxCapRefreshPeriodBlocks = chain.BlocksPerWeek

	// DefaultExposureRefreshPeriodBlocks recomputes the multiplier hourly. The
	// cadence is a judgement about the fastest stress worth tracking: a
	// Terra-speed run unfolds over days, so an hour resolves it many times over,
	// while a shorter period spends state writes resolving noise the decay
	// windows are there to absorb.
	DefaultExposureRefreshPeriodBlocks = chain.BlocksPerHour

	// MaxRewardFundingWindow bounds how many blocks one funding window accrues
	// over. Together with MonetaryPolicy's MaxBlockRewardTarget it is what
	// makes the accrual safe by inspection: the two ceilings multiply to a
	// whole-window total ninety-five bits under the Int limit, so no sequence
	// of blocks can overflow the running targets. A window of 2^32 blocks is
	// some seven centuries at this chain's block time, so the bound refuses
	// only values that were never a schedule.
	MaxRewardFundingWindow = 1 << 32

	// MaxTaxCapRefreshPeriodBlocks bounds the cap-drift cadence at a year.
	MaxTaxCapRefreshPeriodBlocks = chain.BlocksPerYear

	// MaxExposureRefreshPeriodBlocks bounds the cadence at a year. A period
	// longer than the series it reads is not a schedule, and one no block can
	// reach would freeze the multiplier at its stored value with no way to
	// observe that it had.
	MaxExposureRefreshPeriodBlocks = chain.BlocksPerYear
)

// MaxExposureMultiplierCap bounds both the multiplier ceiling and the per-update
// step. It is a domain cap with orders of magnitude of headroom rather than a
// projection over live state: the composite is folded and applied inside block
// hooks, where a checked arithmetic error and a panic are the same outcome — the
// block fails — so the defence has to be refusing the value at the write, where
// a human is in the loop. A million is far past any defensible setting.
var MaxExposureMultiplierCap = math.LegacyNewDec(1_000_000)

// Per-block EWMA retentions. Both are expressed as a retention rather than a
// half-life because the fold applies them directly, and deriving one from the
// other on chain would need a logarithm LegacyDec does not have.
var (
	// DefaultExposureVolatilityDecay retains variance with a half-life of about
	// 13,863 blocks — roughly a day. Volatility is a regime rather than an
	// event, and a day's memory outlasts a single bad hour without carrying a
	// month-old panic into today's target.
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
)

// DefaultParams returns the safe launch defaults for Treasury.
//
// The reference tax cap launches at one base unit — the tightest finite
// ceiling — rather than zero, which is the explicit uncapped sentinel: an
// unconfigured chain should clamp the stability tax to dust, not leave it
// unbounded, and governance opts into either a real ceiling or none.
func DefaultParams() Params {
	return Params{
		ReferenceTaxCap:             sdk.NewCoin(DefaultReferenceTaxCapDenom, math.OneInt()),
		RewardFundingWindow:         DefaultRewardFundingWindow,
		TaxCapRefreshPeriodBlocks:   DefaultTaxCapRefreshPeriodBlocks,
		VolatilityDecay:             DefaultExposureVolatilityDecay,
		FlowDecay:                   DefaultExposureFlowDecay,
		MultiplierCap:               DefaultExposureMultiplierCap,
		MultiplierMaxStep:           DefaultExposureMultiplierMaxStep,
		ExposureRefreshPeriodBlocks: DefaultExposureRefreshPeriodBlocks,
	}
}

// Validate performs context-free validation of Treasury parameters.
// ReferenceTaxCap.Denom's identity with the protocol reference is validated by
// the keeper, and the indicator weights the exposure machinery below sizes live
// on MonetaryPolicy, which validates them.
func (p Params) Validate() error {
	if err := p.ReferenceTaxCap.Validate(); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap is invalid: %w", err)
	}
	if err := chain.ValidatePricedDenom(p.ReferenceTaxCap.Denom); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap denom is invalid: %w", err)
	}
	if p.RewardFundingWindow == 0 || p.RewardFundingWindow > MaxRewardFundingWindow {
		return fmt.Errorf(
			"treasury parameter RewardFundingWindow must be between one and %d: %d",
			uint64(MaxRewardFundingWindow),
			p.RewardFundingWindow,
		)
	}
	if p.TaxCapRefreshPeriodBlocks == 0 ||
		p.TaxCapRefreshPeriodBlocks > MaxTaxCapRefreshPeriodBlocks {
		return fmt.Errorf(
			"treasury parameter TaxCapRefreshPeriodBlocks must be between one and %d: %d",
			MaxTaxCapRefreshPeriodBlocks,
			p.TaxCapRefreshPeriodBlocks,
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

	return nil
}
