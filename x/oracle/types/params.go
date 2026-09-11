// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/oracle/types/params.go.
// Modified for Ark: parameter ownership, defaults, and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package types

import (
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

// Default parameter values
const (
	DefaultRewardWindow             = chain.BlocksPerWeek // window for a week
	DefaultAttendanceWindow         = chain.BlocksPerWeek // window for a week
	DefaultRewardDistributionWindow = chain.BlocksPerYear // window for a year
	DefaultMaxExchangeRateAge       = time.Minute

	// MaxRewardWindow and MaxAttendanceWindow cap settlement cadence at a chain year, keeping valid
	// governance values finite and well above the week-long defaults.
	MaxRewardWindow     = chain.BlocksPerYear
	MaxAttendanceWindow = chain.BlocksPerYear

	// MaxAllowedExchangeRateAge caps the chain-default freshness window at seven days. It bounds
	// ordinary rate reads; consumers using GetRateSetWithin validate their own windows separately.
	MaxAllowedExchangeRateAge = 7 * 24 * time.Hour
)

// MaxOutgoingReferenceRate caps an explicit governance rebase rate at one trillion NOAH per
// outgoing unit. The domain bound constrains consumer rescale arithmetic without depending on
// current state.
var MaxOutgoingReferenceRate = math.LegacyNewDec(1_000_000_000_000)

// Default parameter values
var (
	MinVoteThreshold     = math.LegacyNewDecWithPrec(50, 2)                     // 50%
	DefaultVoteThreshold = math.LegacyMustNewDecFromStr("0.666666666666666667") // > 2/3
	DefaultRewardBand    = math.LegacyNewDecWithPrec(2, 2)                      // 2% (-1, 1)
	// DefaultMinAttendancePerWindow and the week-long window allow recovery after coverage
	// interruptions. A validator can satisfy the ratio by attending the final ratio-share of
	// eligible blocks; tightening either parameter reduces that margin.
	DefaultMinAttendancePerWindow = math.LegacyNewDecWithPrec(5, 2) // 5%
	// MinFunctioningBlockThreshold requires at least half of commit power to participate before
	// grading attendance. Higher values forgive more degraded blocks; zero MinAttendancePerWindow
	// separately disables jailing.
	MinFunctioningBlockThreshold     = math.LegacyNewDecWithPrec(50, 2) // 50%
	DefaultFunctioningBlockThreshold = MinFunctioningBlockThreshold
	// MaxParticipationThreshold caps the attendance coverage floor at half the target set.
	// Per-target rewards encourage fuller coverage; zero still requires one positive rate.
	MaxParticipationThreshold = math.LegacyNewDecWithPrec(50, 2) // 50%
	// DefaultParticipationThreshold asks a participating report to price a
	// fifth of the target set: low enough that no live sidecar with a partial
	// provider gap is at risk, high enough that a hardcoded token rate no
	// longer counts as running an oracle.
	DefaultParticipationThreshold = math.LegacyNewDecWithPrec(20, 2) // 20%
)

// DefaultParams creates default oracle module parameters
func DefaultParams() Params {
	return Params{
		VoteThreshold:             DefaultVoteThreshold,
		RewardBand:                DefaultRewardBand,
		RewardWindow:              DefaultRewardWindow,
		RewardDistributionWindow:  DefaultRewardDistributionWindow,
		AttendanceWindow:          DefaultAttendanceWindow,
		MinAttendancePerWindow:    DefaultMinAttendancePerWindow,
		MaxExchangeRateAge:        DefaultMaxExchangeRateAge,
		FunctioningBlockThreshold: DefaultFunctioningBlockThreshold,
		ParticipationThreshold:    DefaultParticipationThreshold,
	}
}

// Validate performs basic validation on oracle parameters.
func (p Params) Validate() error {
	if p.VoteThreshold.IsNil() {
		return errors.New("oracle parameter VoteThreshold must be set")
	}
	if p.VoteThreshold.LT(MinVoteThreshold) || p.VoteThreshold.GT(math.LegacyOneDec()) {
		return fmt.Errorf(
			"oracle parameter VoteThreshold must be between %s and one, is %s",
			MinVoteThreshold,
			p.VoteThreshold,
		)
	}
	if p.RewardBand.IsNil() {
		return errors.New("oracle parameter RewardBand must be set")
	}
	if p.RewardBand.GT(math.LegacyOneDec()) || p.RewardBand.IsNegative() {
		return errors.New("oracle parameter RewardBand must be between [0, 1]")
	}
	if p.RewardWindow == 0 || p.RewardWindow > MaxRewardWindow {
		return fmt.Errorf(
			"oracle parameter RewardWindow must be between one and %d, is %d",
			MaxRewardWindow,
			p.RewardWindow,
		)
	}
	if p.RewardDistributionWindow < p.RewardWindow {
		return errors.New("oracle parameter RewardDistributionWindow must be greater than or equal with RewardWindow")
	}
	if p.AttendanceWindow == 0 || p.AttendanceWindow > MaxAttendanceWindow {
		return fmt.Errorf(
			"oracle parameter AttendanceWindow must be between one and %d, is %d",
			MaxAttendanceWindow,
			p.AttendanceWindow,
		)
	}
	if p.MinAttendancePerWindow.IsNil() {
		return errors.New("oracle parameter MinAttendancePerWindow must be set")
	}
	if p.MinAttendancePerWindow.GT(math.LegacyOneDec()) || p.MinAttendancePerWindow.IsNegative() {
		return errors.New("oracle parameter MinAttendancePerWindow must be between [0, 1]")
	}
	if p.FunctioningBlockThreshold.IsNil() {
		return errors.New("oracle parameter FunctioningBlockThreshold must be set")
	}
	if p.FunctioningBlockThreshold.LT(MinFunctioningBlockThreshold) ||
		p.FunctioningBlockThreshold.GT(math.LegacyOneDec()) {
		return fmt.Errorf(
			"oracle parameter FunctioningBlockThreshold must be between %s and one, is %s",
			MinFunctioningBlockThreshold,
			p.FunctioningBlockThreshold,
		)
	}
	if p.ParticipationThreshold.IsNil() {
		return errors.New("oracle parameter ParticipationThreshold must be set")
	}
	if p.ParticipationThreshold.IsNegative() || p.ParticipationThreshold.GT(MaxParticipationThreshold) {
		return fmt.Errorf(
			"oracle parameter ParticipationThreshold must be between zero and %s, is %s",
			MaxParticipationThreshold,
			p.ParticipationThreshold,
		)
	}
	if p.MaxExchangeRateAge <= 0 || p.MaxExchangeRateAge > MaxAllowedExchangeRateAge {
		return fmt.Errorf(
			"oracle parameter MaxExchangeRateAge must be greater than zero and at most %s, is %s",
			MaxAllowedExchangeRateAge,
			p.MaxExchangeRateAge,
		)
	}

	return nil
}
