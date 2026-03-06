package types

import (
	"fmt"

	"cosmossdk.io/math"

	core "noah/types"
)

// Default parameter values
const (
	DefaultVotePeriod               = core.BlocksPerMinute / 2 // 30 seconds
	DefaultSlashWindow              = core.BlocksPerWeek       // window for a week
	DefaultRewardDistributionWindow = core.BlocksPerYear       // window for a year
)

// Default parameter values
var (
	DefaultVoteThreshold = math.LegacyNewDecWithPrec(50, 2) // 50%
	DefaultRewardBand    = math.LegacyNewDecWithPrec(2, 2)  // 2% (-1, 1)
	DefaultTobinTax      = math.LegacyNewDecWithPrec(25, 4) // 0.25%
	DefaultWhitelist     = DenomList{
		{Name: core.MicroKRWDenom, TobinTax: DefaultTobinTax},
		{Name: core.MicroSDRDenom, TobinTax: DefaultTobinTax},
		{Name: core.MicroUSDDenom, TobinTax: DefaultTobinTax},
		{Name: core.MicroMNTDenom, TobinTax: DefaultTobinTax.MulInt64(8)},
	}
	DefaultSlashFraction     = math.LegacyNewDecWithPrec(1, 4) // 0.01%
	DefaultMinValidPerWindow = math.LegacyNewDecWithPrec(5, 2) // 5%
)

// DefaultParams creates default oracle module parameters
func DefaultParams() Params {
	return Params{
		VotePeriod:               DefaultVotePeriod,
		VoteThreshold:            DefaultVoteThreshold,
		RewardBand:               DefaultRewardBand,
		RewardDistributionWindow: DefaultRewardDistributionWindow,
		Whitelist:                DefaultWhitelist,
		SlashFraction:            DefaultSlashFraction,
		SlashWindow:              DefaultSlashWindow,
		MinValidPerWindow:        DefaultMinValidPerWindow,
	}
}

// Validate performs basic validation on oracle parameters.
func (p Params) Validate() error {
	if p.VotePeriod == 0 {
		return fmt.Errorf("oracle parameter VotePeriod must be > 0, is %d", p.VotePeriod)
	}
	if p.VoteThreshold.LTE(math.LegacyNewDecWithPrec(33, 2)) {
		return fmt.Errorf("oracle parameter VoteThreshold must be greater than 33 percent")
	}

	if p.RewardBand.GT(math.LegacyOneDec()) || p.RewardBand.IsNegative() {
		return fmt.Errorf("oracle parameter RewardBand must be between [0, 1]")
	}

	if p.RewardDistributionWindow < p.VotePeriod {
		return fmt.Errorf("oracle parameter RewardDistributionWindow must be greater than or equal with VotePeriod")
	}

	if p.SlashFraction.GT(math.LegacyOneDec()) || p.SlashFraction.IsNegative() {
		return fmt.Errorf("oracle parameter SlashFraction must be between [0, 1]")
	}

	if p.SlashWindow < p.VotePeriod {
		return fmt.Errorf("oracle parameter SlashWindow must be greater than or equal with VotePeriod")
	}

	if p.MinValidPerWindow.GT(math.LegacyOneDec()) || p.MinValidPerWindow.IsNegative() {
		return fmt.Errorf("oracle parameter MinValidPerWindow must be between [0, 1]")
	}

	for _, denom := range p.Whitelist {
		if denom.TobinTax.GT(math.LegacyOneDec()) || denom.TobinTax.IsNegative() {
			return fmt.Errorf("oracle parameter Whitelist Denom must have TobinTax between [0, 1]")
		}
		if len(denom.Name) == 0 {
			return fmt.Errorf("oracle parameter Whitelist Denom must have name")
		}
	}
	return nil
}
