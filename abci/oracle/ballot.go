package oracle

import (
	"slices"

	"cosmossdk.io/math"

	"ark/pkg/decimal"
)

// tallyVote is one positive validator report in an aggregation-local ballot.
type tallyVote struct {
	validator int
	rate      math.LegacyDec
	power     int64
}

// A ballot caches positive reports both in tally order and by dense validator
// index. A nil rate means that validator did not submit a positive report.
// Every ballot in a tally sizes rates to the full validator count, so
// cross-ballot methods index another ballot's rates without bounds checks.
type ballot struct {
	votes []tallyVote
	rates []math.LegacyDec
	power int64
}

func (b *ballot) add(vote tallyVote) {
	b.votes = append(b.votes, vote)
	b.rates[vote.validator] = vote.rate
	b.power += vote.power
}

// weightedMedian sorts the ballot in place and returns the lower weighted median.
func (b *ballot) weightedMedian() math.LegacyDec {
	if b.power <= 0 || len(b.votes) == 0 {
		return math.LegacyZeroDec()
	}

	slices.SortFunc(b.votes, func(left, right tallyVote) int {
		switch {
		case left.rate.LT(right.rate):
			return -1
		case left.rate.GT(right.rate):
			return 1
		default:
			return left.validator - right.validator
		}
	})

	pivotPower := b.power/2 + b.power%2
	var cumulativePower int64
	for _, vote := range b.votes {
		cumulativePower += vote.power
		if cumulativePower >= pivotPower {
			return vote.rate
		}
	}
	return math.LegacyZeroDec()
}

// overlapPower returns the voting power shared by both ballots.
func (b ballot) overlapPower(other ballot) int64 {
	if len(b.votes) > len(other.votes) {
		b, other = other, b
	}

	var power int64
	for _, vote := range b.votes {
		if !other.rates[vote.validator].IsNil() {
			power += vote.power
		}
	}

	return power
}

// appendCrossRates appends reference/target ratios from validators with a
// positive rate in both ballots into dst and returns the extended slice plus
// their voting power.
func (b ballot) appendCrossRates(reference ballot, dst []tallyVote) ([]tallyVote, int64) {
	var power int64
	for _, vote := range b.votes {
		referenceRate := reference.rates[vote.validator]
		if referenceRate.IsNil() {
			continue
		}

		crossRate, err := decimal.Quo(referenceRate, vote.rate)
		if err != nil || !crossRate.IsPositive() {
			continue
		}
		vote.rate = crossRate
		dst = append(dst, vote)
		power += vote.power
	}

	return dst, power
}
