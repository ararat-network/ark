package types

import (
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/math"

	chain "github.com/ararat-network/ark/pkg/chain"
)

// Default parameter values
const (
	DefaultRewardWindow             = chain.BlocksPerWeek // window for a week
	DefaultAttendanceWindow         = chain.BlocksPerWeek // window for a week
	DefaultRewardDistributionWindow = chain.BlocksPerYear // window for a year
	DefaultMaxExchangeRateAge       = time.Minute

	// MaxRewardWindow and MaxAttendanceWindow bound the two settlement
	// cadences at a year. Neither feeds arithmetic that can overflow — both are
	// moduli — but both are the only trigger their settlement has, so a period
	// no chain reaches does not slow them down, it switches them off: rewards
	// would never settle, and attendance would never grade, which is the
	// deadman switch jailing depends on. A year is fifty-odd times the
	// week-long defaults, so the bound refuses only values that were never a
	// schedule.
	MaxRewardWindow     = chain.BlocksPerYear
	MaxAttendanceWindow = chain.BlocksPerYear

	// MaxAllowedExchangeRateAge bounds the staleness window governance may set.
	//
	// This is the load-bearing one. MaxExchangeRateAge is the gate every
	// freshness check measures against — conversion quotes, the liability
	// partition's priced bucket, each recognition entry's own window — so a
	// value large enough to never elapse does not loosen the gate, it removes
	// it: every rate reads fresh forever, conversions quote on arbitrarily old
	// prices, and the partition reports a fully priced aggregate it cannot
	// support. Nothing errors, which is what makes it worth refusing at the
	// write. Seven days is four orders of magnitude above the one-minute
	// default and far beyond any outage a live feed should survive as "fresh".
	MaxAllowedExchangeRateAge = 7 * 24 * time.Hour
)

// Default parameter values
var (
	MinVoteThreshold     = math.LegacyNewDecWithPrec(50, 2)                     // 50%
	DefaultVoteThreshold = math.LegacyMustNewDecFromStr("0.666666666666666667") // > 2/3
	DefaultRewardBand    = math.LegacyNewDecWithPrec(2, 2)                      // 2% (-1, 1)
	// DefaultMinAttendancePerWindow is deliberately lenient, and the week-long
	// DefaultAttendanceWindow above is part of the same choice: together they are
	// the defence against correlated jailing when a target-set change outpaces
	// sidecar rollouts. A dark validator passes by attending only the final
	// ratio-share of the window, so the recovery span is (1 - ratio) × window —
	// most of a week with these defaults. Tightening either parameter shrinks
	// that span and must be weighed against rollout lag, not just individual
	// hygiene.
	DefaultMinAttendancePerWindow = math.LegacyNewDecWithPrec(5, 2) // 5%
	// MinFunctioningBlockThreshold floors FunctioningBlockThreshold at a
	// majority. A block grades attendance only once participating power reaches
	// the threshold, so a dark coalition holding more than the remaining share
	// switches grading off entirely; flooring at a majority forces that
	// coalition to be a majority itself. Going lower buys nothing back: the
	// deadman would start charging validators for blocks a majority of the
	// fleet could not price, which is the correlated-outage mass jailing the
	// design exists to prevent. Governance may only raise this, trading
	// sensitivity for forgiveness; MinAttendancePerWindow = 0 remains the
	// direct way to switch jailing off.
	MinFunctioningBlockThreshold     = math.LegacyNewDecWithPrec(50, 2) // 50%
	DefaultFunctioningBlockThreshold = MinFunctioningBlockThreshold
	// MaxParticipationThreshold caps ParticipationThreshold at half the target
	// set. The parameter is a deadman floor — the share of targets a report
	// must price before it counts as participation at all — not a coverage
	// mandate: coverage misses correlate through shared providers, so above a
	// majority of targets the floor would let a split-fleet provider gap jail
	// the affected half while the healthy half keeps blocks functioning.
	// Coverage pressure belongs to per-target reward weight. Zero restores the
	// single-positive-rate floor.
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
			uint64(MaxRewardWindow),
			p.RewardWindow,
		)
	}
	if p.RewardDistributionWindow < p.RewardWindow {
		return errors.New("oracle parameter RewardDistributionWindow must be greater than or equal with RewardWindow")
	}
	if p.AttendanceWindow == 0 || p.AttendanceWindow > MaxAttendanceWindow {
		return fmt.Errorf(
			"oracle parameter AttendanceWindow must be between one and %d, is %d",
			uint64(MaxAttendanceWindow),
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
