package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
)

// Default parameter values
const (
	DefaultWindowShort     = uint64(4)  // a month
	DefaultWindowLong      = uint64(52) // a year
	DefaultWindowProbation = uint64(12) // 3 month
)

var (
	DefaultTaxPolicy = PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),                                         // 0.05%
		RateMax:       math.LegacyNewDecWithPrec(1, 2),                                         // 1%
		Cap:           sdk.NewCoin(chain.MicroSDRDenom, math.OneInt().MulRaw(chain.MicroUnit)), // 1 SDR Tax cap
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 5),                                        // 0.025%
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
	if p.TaxPolicy.RateMax.IsNil() {
		return errors.New("treasury parameter TaxPolicy.RateMax must be set")
	}
	if p.TaxPolicy.RateMin.IsNil() {
		return errors.New("treasury parameter TaxPolicy.RateMin must be set")
	}
	if p.TaxPolicy.ChangeRateMax.IsNil() {
		return errors.New("treasury parameter TaxPolicy.ChangeRateMax must be set")
	}
	if p.RewardPolicy.RateMax.IsNil() {
		return errors.New("treasury parameter RewardPolicy.RateMax must be set")
	}
	if p.RewardPolicy.RateMin.IsNil() {
		return errors.New("treasury parameter RewardPolicy.RateMin must be set")
	}
	if p.RewardPolicy.ChangeRateMax.IsNil() {
		return errors.New("treasury parameter RewardPolicy.ChangeRateMax must be set")
	}
	if p.SeigniorageBurdenTarget.IsNil() {
		return errors.New("treasury parameter SeigniorageBurdenTarget must be set")
	}
	if p.BurnWeight.IsNil() {
		return errors.New("treasury parameter BurnWeight must be set")
	}
	if p.MiningIncrement.IsNil() {
		return errors.New("treasury parameter MiningIncrement must be set")
	}

	if p.TaxPolicy.RateMax.LT(p.TaxPolicy.RateMin) {
		return fmt.Errorf("treasury TaxPolicy.RateMax %s must be greater than TaxPolicy.RateMin %s",
			p.TaxPolicy.RateMax, p.TaxPolicy.RateMin)
	}

	if p.TaxPolicy.RateMin.IsNegative() {
		return fmt.Errorf("treasury parameter TaxPolicy.RateMin must be zero or positive: %s", p.TaxPolicy.RateMin)
	}

	if !p.TaxPolicy.Cap.IsValid() {
		return errors.New("treasury parameter TaxPolicy.Cap is invalid")
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

	if p.BurnWeight.IsNegative() {
		return fmt.Errorf("treasury parameter BurnWeight must be between zero and 1 - RewardPolicy.RateMax: %s", p.BurnWeight)
	}

	if p.RewardPolicy.RateMax.GT(math.LegacyOneDec()) {
		return fmt.Errorf("treasury parameter RewardPolicy.RateMax must not exceed one: %s", p.RewardPolicy.RateMax)
	}

	if p.BurnWeight.GT(math.LegacyOneDec().Sub(p.RewardPolicy.RateMax)) {
		return fmt.Errorf("treasury parameter BurnWeight must be between zero and 1 - RewardPolicy.RateMax: %s", p.BurnWeight)
	}

	if p.MiningIncrement.IsNegative() {
		return fmt.Errorf("treasury parameter MiningIncrement must be zero or positive: %s", p.MiningIncrement)
	}

	if p.WindowLong == 0 {
		return errors.New("treasury parameter WindowLong must be positive")
	}

	if p.WindowShort == 0 {
		return errors.New("treasury parameter WindowShort must be positive")
	}

	if p.WindowLong <= p.WindowShort {
		return fmt.Errorf("treasury parameter WindowLong must be greater than WindowShort: (%d, %d)", p.WindowLong, p.WindowShort)
	}

	return nil
}
