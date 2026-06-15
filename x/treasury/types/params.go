package types

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
)

// Default parameter values
const (
	DefaultWindowShort     = uint64(4)  // a month
	DefaultWindowLong      = uint64(52) // a year
	DefaultWindowProbation = uint64(12) // 3 month
)

var (
	DefaultTaxPolicy = PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),                                       // 0.05%
		RateMax:       math.LegacyNewDecWithPrec(1, 2),                                       // 1%
		Cap:           sdk.NewCoin(core.MicroSDRDenom, math.OneInt().MulRaw(core.MicroUnit)), // 1 SDR Tax cap
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 5),                                      // 0.025%
	}
	DefaultRewardPolicy = PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 2),       // 5%
		RateMax:       math.LegacyNewDecWithPrec(50, 2),      // 50%
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 3),      // 2.5%
		Cap:           sdk.NewCoin("unused", math.ZeroInt()), // UNUSED
	}
	DefaultSeigniorageBurdenTarget = math.LegacyNewDecWithPrec(67, 2)  // 67%
	DefaultBurnWeight              = math.LegacyNewDecWithPrec(1, 1)   // 10%
	DefaultMiningIncrement         = math.LegacyNewDecWithPrec(107, 2) // 1.07 mining increment; exponential growth
)

// DefaultParams creates default treasury module parameters
func DefaultParams() Params {
	return Params{
		TaxPolicy:               DefaultTaxPolicy,
		RewardPolicy:            DefaultRewardPolicy,
		SeigniorageBurdenTarget: DefaultSeigniorageBurdenTarget,
		BurnWeight:              DefaultBurnWeight,
		MiningIncrement:         DefaultMiningIncrement,
		WindowShort:             DefaultWindowShort,
		WindowLong:              DefaultWindowLong,
		WindowProbation:         DefaultWindowProbation,
	}
}

// Validate performs basic validation on treasury parameters.
func (p Params) Validate() error {
	if err := validateLegacyDecSet("TaxPolicy.RateMax", p.TaxPolicy.RateMax); err != nil {
		return err
	}
	if err := validateLegacyDecSet("TaxPolicy.RateMin", p.TaxPolicy.RateMin); err != nil {
		return err
	}
	if err := validateLegacyDecSet("TaxPolicy.ChangeRateMax", p.TaxPolicy.ChangeRateMax); err != nil {
		return err
	}
	if err := validateLegacyDecSet("RewardPolicy.RateMax", p.RewardPolicy.RateMax); err != nil {
		return err
	}
	if err := validateLegacyDecSet("RewardPolicy.RateMin", p.RewardPolicy.RateMin); err != nil {
		return err
	}
	if err := validateLegacyDecSet("RewardPolicy.ChangeRateMax", p.RewardPolicy.ChangeRateMax); err != nil {
		return err
	}
	if err := validateLegacyDecSet("SeigniorageBurdenTarget", p.SeigniorageBurdenTarget); err != nil {
		return err
	}
	if err := validateLegacyDecSet("BurnWeight", p.BurnWeight); err != nil {
		return err
	}
	if err := validateLegacyDecSet("MiningIncrement", p.MiningIncrement); err != nil {
		return err
	}

	if p.TaxPolicy.RateMax.LT(p.TaxPolicy.RateMin) {
		return fmt.Errorf("treasury TaxPolicy.RateMax %s must be greater than TaxPolicy.RateMin %s",
			p.TaxPolicy.RateMax, p.TaxPolicy.RateMin)
	}

	if p.TaxPolicy.RateMin.IsNegative() {
		return fmt.Errorf("treasury parameter TaxPolicy.RateMin must be zero or positive: %s", p.TaxPolicy.RateMin)
	}

	if !p.TaxPolicy.Cap.IsValid() {
		return fmt.Errorf("treasury parameter TaxPolicy.Cap is invalid")
	}

	if p.TaxPolicy.ChangeRateMax.IsNegative() {
		return fmt.Errorf("treasury parameter TaxPolicy.ChangeRateMax must be zero or positive: %s", p.TaxPolicy.ChangeRateMax)
	}

	if p.RewardPolicy.RateMax.LT(p.RewardPolicy.RateMin) {
		return fmt.Errorf("treasury RewardPolicy.RateMax %s must be greater than RewardPolicy.RateMin %s",
			p.RewardPolicy.RateMax, p.RewardPolicy.RateMin)
	}

	if p.RewardPolicy.RateMin.IsNegative() {
		return fmt.Errorf("treasury parameter RewardPolicy.RateMin must be zero or positive: %s", p.RewardPolicy.RateMin)
	}

	if p.RewardPolicy.ChangeRateMax.IsNegative() {
		return fmt.Errorf("treasury parameter RewardPolicy.ChangeRateMax must be zero or positive: %s", p.RewardPolicy.ChangeRateMax)
	}

	if p.SeigniorageBurdenTarget.IsNegative() {
		return fmt.Errorf("treasury parameter SeigniorageBurdenTarget must be zero or positive: %s", p.SeigniorageBurdenTarget)
	}

	if p.BurnWeight.Add(p.RewardPolicy.RateMax).GT(math.LegacyOneDec()) || p.BurnWeight.IsNegative() {
		return fmt.Errorf("treasury parameter BurnWeight must be between zero and 1 - RewardPolicy.RateMax: %s", p.BurnWeight)
	}

	if p.MiningIncrement.IsNegative() {
		return fmt.Errorf("treasury parameter MiningIncrement must be zero or positive: %s", p.MiningIncrement)
	}

	if p.WindowLong == 0 {
		return fmt.Errorf("treasury parameter WindowLong must be positive")
	}

	if p.WindowShort == 0 {
		return fmt.Errorf("treasury parameter WindowShort must be positive")
	}

	if p.WindowLong <= p.WindowShort {
		return fmt.Errorf("treasury parameter WindowLong must be greater than WindowShort: (%d, %d)", p.WindowLong, p.WindowShort)
	}

	return nil
}

func validateLegacyDecSet(name string, dec math.LegacyDec) error {
	if dec.IsNil() {
		return fmt.Errorf("treasury parameter %s must be set", name)
	}

	return nil
}
