package oracle

import (
	"slices"

	"cosmossdk.io/math"
)

// tallyVote is one positive validator report in an aggregation-local ballot.
type tallyVote struct {
	validator int
	rate      math.LegacyDec
	power     int64
}

// A source ballot caches positive reports both in tally order and by dense
// validator index. A nil rate means that validator did not submit a positive
// report. Derived cross ballots use only votes and power.
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

// weightedMedian returns the lower weighted median. At an exact even-power tie,
// the lower rate wins; for odd power, cumulative power must reach the ceiling of
// half the ballot power.
func (b ballot) weightedMedian() math.LegacyDec {
	if b.power <= 0 || len(b.votes) == 0 {
		return math.LegacyZeroDec()
	}

	slices.SortFunc(b.votes, func(a, other tallyVote) int {
		switch {
		case a.rate.LT(other.rate):
			return -1
		case a.rate.GT(other.rate):
			return 1
		default:
			return a.validator - other.validator
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

// crossRatePowers returns the usable overlap power for each reference
// direction. A validator contributes only when that direction's derived
// reference/target rate is inside the conservative safe range.
func (b ballot) crossRatePowers(other ballot) (bReferencePower, otherReferencePower int64) {
	if len(b.votes) > len(other.votes) {
		otherPower, bPower := other.crossRatePowers(b)
		return bPower, otherPower
	}

	for _, vote := range b.votes {
		if vote.validator >= len(other.rates) {
			continue
		}
		otherRate := other.rates[vote.validator]
		if otherRate.IsNil() {
			continue
		}

		if positiveQuotientSafelyRepresentable(vote.rate, otherRate) {
			bReferencePower += vote.power
		}
		if positiveQuotientSafelyRepresentable(otherRate, vote.rate) {
			otherReferencePower += vote.power
		}
	}

	return bReferencePower, otherReferencePower
}

// appendCrossRates appends safe reference/target reports from validators with
// a positive rate in both ballots into dst and returns the extended slice plus
// their voting power.
func (b ballot) appendCrossRates(reference ballot, dst []tallyVote) ([]tallyVote, int64) {
	var power int64
	for _, vote := range b.votes {
		if vote.validator >= len(reference.rates) {
			continue
		}
		referenceRate := reference.rates[vote.validator]
		if referenceRate.IsNil() {
			continue
		}

		rate, ok := safePositiveQuotient(referenceRate, vote.rate)
		if !ok {
			continue
		}

		vote.rate = rate
		dst = append(dst, vote)
		power += vote.power
	}

	return dst, power
}

const (
	// These conservative magnitude bounds guarantee a positive, in-range
	// LegacyDec quotient given its roughly 60 precision bits and 256 magnitude
	// bits. Ratios near the numeric limits are deliberately treated as unusable
	// oracle observations rather than supported exactly.
	minSafeQuotientBitDelta = -58
	maxSafeQuotientBitDelta = 254
)

// positiveQuotientSafelyRepresentable reports whether the quotient is well
// inside LegacyDec's positive range. The conservative boundary intentionally
// rejects technically representable extreme ratios that have no useful oracle
// meaning, while keeping reference selection allocation-free.
func positiveQuotientSafelyRepresentable(
	numerator,
	denominator math.LegacyDec,
) bool {
	if numerator.IsNil() || denominator.IsNil() ||
		!numerator.IsPositive() || !denominator.IsPositive() {
		return false
	}

	bitDelta := numerator.BigIntMut().BitLen() - denominator.BigIntMut().BitLen()

	return bitDelta >= minSafeQuotientBitDelta && bitDelta <= maxSafeQuotientBitDelta
}

// safePositiveQuotient returns numerator/denominator when it is inside the
// conservative oracle range. Invalid or extreme derived rates are unusable
// observations rather than block-fatal panics.
func safePositiveQuotient(
	numerator,
	denominator math.LegacyDec,
) (math.LegacyDec, bool) {
	if !positiveQuotientSafelyRepresentable(numerator, denominator) {
		return math.LegacyZeroDec(), false
	}

	return numerator.Quo(denominator), true
}
