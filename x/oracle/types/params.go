package types

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"cosmossdk.io/math"

	chain "ark/pkg/chain"
)

// Default parameter values
const (
	DefaultRewardWindow             = chain.BlocksPerWeek // window for a week
	DefaultSlashWindow              = chain.BlocksPerWeek // window for a week
	DefaultRewardDistributionWindow = chain.BlocksPerYear // window for a year
	DefaultMaxExchangeRateAge       = time.Minute
)

// Default parameter values
var (
	DefaultVoteThreshold = math.LegacyNewDecWithPrec(667, 3) // 66.7%
	DefaultRewardBand    = math.LegacyNewDecWithPrec(2, 2)   // 2% (-1, 1)
	DefaultTobinTax      = math.LegacyNewDecWithPrec(25, 4)  // 0.25%
	DefaultTobinTaxes    = []TobinTax{
		{Denom: chain.MicroUSDDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroKRWDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroSDRDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroCNYDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroJPYDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroEURDenom, TobinTax: DefaultTobinTax},
		{Denom: chain.MicroGBPDenom, TobinTax: DefaultTobinTax},
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
		TobinTaxes:               slices.Clone(DefaultTobinTaxes),
		SlashFraction:            DefaultSlashFraction,
		SlashWindow:              DefaultSlashWindow,
		MinValidPerWindow:        DefaultMinValidPerWindow,
		MaxExchangeRateAge:       DefaultMaxExchangeRateAge,
	}
}

// Validate performs basic validation on oracle parameters.
func (p Params) Validate() error {
	if p.VoteThreshold.IsNil() {
		return errors.New("oracle parameter VoteThreshold must be set")
	}
	if p.VoteThreshold.LTE(math.LegacyNewDecWithPrec(33, 2)) {
		return errors.New("oracle parameter VoteThreshold must be greater than 33 percent")
	}
	if p.VoteThreshold.GT(math.LegacyOneDec()) {
		return errors.New("oracle parameter VoteThreshold must not exceed 100 percent")
	}
	if p.RewardBand.IsNil() {
		return errors.New("oracle parameter RewardBand must be set")
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
	if p.SlashFraction.IsNil() {
		return errors.New("oracle parameter SlashFraction must be set")
	}
	if p.SlashFraction.GT(math.LegacyOneDec()) || p.SlashFraction.IsNegative() {
		return errors.New("oracle parameter SlashFraction must be between [0, 1]")
	}
	if p.SlashWindow == 0 {
		return fmt.Errorf("oracle parameter SlashWindow must be > 0, is %d", p.SlashWindow)
	}
	if p.MinValidPerWindow.IsNil() {
		return errors.New("oracle parameter MinValidPerWindow must be set")
	}
	if p.MinValidPerWindow.GT(math.LegacyOneDec()) || p.MinValidPerWindow.IsNegative() {
		return errors.New("oracle parameter MinValidPerWindow must be between [0, 1]")
	}
	if p.MaxExchangeRateAge <= 0 {
		return errors.New("oracle parameter MaxExchangeRateAge must be greater than zero")
	}
	if len(p.TobinTaxes) > MaxVoteTargets {
		return fmt.Errorf(
			"oracle parameter TobinTaxes count %d exceeds maximum vote targets %d",
			len(p.TobinTaxes),
			MaxVoteTargets,
		)
	}
	seen := make(map[string]struct{}, len(p.TobinTaxes))
	for _, tobinTax := range p.TobinTaxes {
		if tobinTax.TobinTax.IsNil() {
			return fmt.Errorf("oracle parameter TobinTaxes must have TobinTax set for denom %s", tobinTax.Denom)
		}
		if tobinTax.TobinTax.GT(math.LegacyOneDec()) || tobinTax.TobinTax.IsNegative() {
			return errors.New("oracle parameter TobinTaxes must have TobinTax between [0, 1]")
		}

		if err := chain.ValidateMicroDenom(tobinTax.Denom); err != nil {
			return fmt.Errorf("oracle parameter TobinTaxes %w", err)
		}
		if tobinTax.Denom == chain.MicroNoahDenom {
			return fmt.Errorf("oracle parameter TobinTaxes must not contain native denom %s", tobinTax.Denom)
		}

		if _, ok := seen[tobinTax.Denom]; ok {
			return fmt.Errorf("oracle parameter TobinTaxes contains duplicate denom: %s", tobinTax.Denom)
		}
		seen[tobinTax.Denom] = struct{}{}
	}

	return nil
}
