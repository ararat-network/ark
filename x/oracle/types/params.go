package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	chain "noah/pkg/chain"
)

// Default parameter values
const (
	DefaultRewardWindow             = chain.BlocksPerWeek       // window for a week
	DefaultSlashWindow              = chain.BlocksPerWeek       // window for a week
	DefaultRewardDistributionWindow = chain.BlocksPerYear       // window for a year
	DefaultMaxExchangeRateAge       = chain.BlocksPerMinute / 2 // 30 seconds
)

// Default parameter values
var (
	DefaultVoteThreshold = math.LegacyNewDecWithPrec(50, 2) // 50%
	DefaultRewardBand    = math.LegacyNewDecWithPrec(2, 2)  // 2% (-1, 1)
	DefaultTobinTax      = math.LegacyNewDecWithPrec(25, 4) // 0.25%
	DefaultTobinTaxes    = TobinTaxes{
		{Denom: chain.MicroKRWDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroSDRDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroUSDDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroMNTDenom, TobinTax: DefaultTobinTax.MulInt64(8)},
	}
	DefaultSlashFraction     = math.LegacyNewDecWithPrec(1, 4) // 0.01%
	DefaultMinValidPerWindow = math.LegacyNewDecWithPrec(5, 2) // 5%
)

// DefaultParams creates default oracle module parameters
func DefaultParams() Params {
	return Params{
		VoteThreshold:            DefaultVoteThreshold,
		RewardBand:               DefaultRewardBand,
		RewardWindow:             DefaultRewardWindow,
		RewardDistributionWindow: DefaultRewardDistributionWindow,
		TobinTaxes:               DefaultTobinTaxes,
		SlashFraction:            DefaultSlashFraction,
		SlashWindow:              DefaultSlashWindow,
		MinValidPerWindow:        DefaultMinValidPerWindow,
		MaxExchangeRateAge:       DefaultMaxExchangeRateAge,
	}
}

// Validate performs basic validation on oracle parameters.
func (p Params) Validate() error {
	if p.VoteThreshold.LTE(math.LegacyNewDecWithPrec(33, 2)) {
		return errors.New("oracle parameter VoteThreshold must be greater than 33 percent")
	}
	if p.RewardBand.GT(math.LegacyOneDec()) || p.RewardBand.IsNegative() {
		return errors.New("oracle parameter RewardBand must be between [0, 1]")
	}
	if p.RewardWindow == 0 {
		return fmt.Errorf("oracle parameter RewardWindow must be > 0, is %d", p.RewardWindow)
	}
	if p.RewardDistributionWindow < p.RewardWindow {
		return errors.New("oracle parameter RewardDistributionWindow must be greater than or equal with RewardWindow")
	}
	if p.SlashFraction.GT(math.LegacyOneDec()) || p.SlashFraction.IsNegative() {
		return errors.New("oracle parameter SlashFraction must be between [0, 1]")
	}
	if p.SlashWindow == 0 {
		return fmt.Errorf("oracle parameter SlashWindow must be > 0, is %d", p.SlashWindow)
	}
	if p.MinValidPerWindow.GT(math.LegacyOneDec()) || p.MinValidPerWindow.IsNegative() {
		return errors.New("oracle parameter MinValidPerWindow must be between [0, 1]")
	}
	seen := make(map[string]struct{}, len(p.TobinTaxes))
	for _, tobinTax := range p.TobinTaxes {
		if tobinTax.TobinTax.GT(math.LegacyOneDec()) || tobinTax.TobinTax.IsNegative() {
			return errors.New("oracle parameter TobinTaxes must have TobinTax between [0, 1]")
		}

		if len(tobinTax.Denom) < 3 || tobinTax.Denom[0] != 'u' {
			return fmt.Errorf("oracle parameter TobinTaxes denom must be a micro denom beginning with u: %s", tobinTax.Denom)
		}

		if _, ok := seen[tobinTax.Denom]; ok {
			return fmt.Errorf("oracle parameter TobinTaxes contains duplicate denom: %s", tobinTax.Denom)
		}
		seen[tobinTax.Denom] = struct{}{}
	}

	return nil
}
