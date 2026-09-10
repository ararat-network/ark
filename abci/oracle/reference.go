package oracle

import (
	"math/big"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/decimal"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

type referenceScore struct {
	targetIndex      int
	qualifiedTargets int
	overlapPower     math.Int
	rawPower         int64
}

func (s referenceScore) betterThan(other referenceScore) bool {
	if s.qualifiedTargets != other.qualifiedTargets {
		return s.qualifiedTargets > other.qualifiedTargets
	}
	if !s.overlapPower.Equal(other.overlapPower) {
		return s.overlapPower.GT(other.overlapPower)
	}
	if s.rawPower != other.rawPower {
		return s.rawPower > other.rawPower
	}

	return s.targetIndex < other.targetIndex
}

type pricedTally struct {
	targetIndex int
	votes       []tallyVote
	median      math.LegacyDec
	price       math.LegacyDec
}

// selectReference returns priced tallies for the passing target with the
// strongest raw quorum overlap. Passing must contain at least one target index.
func selectReference(passing []int, ballots []ballot, thresholdPower int64) []pricedTally {
	scores := scoreReferences(passing, ballots, thresholdPower)
	best := scores[0]
	for _, candidate := range scores[1:] {
		if candidate.betterThan(best) {
			best = candidate
		}
	}

	voteCapacity := 0
	for _, targetIndex := range passing {
		voteCapacity += len(ballots[targetIndex].votes)
	}

	referenceIndex := best.targetIndex
	voteArena := make([]tallyVote, 0, voteCapacity)
	referenceBallot := ballots[referenceIndex]
	voteArena = append(voteArena, referenceBallot.votes...)
	referenceTally := ballot{
		votes: voteArena,
		power: referenceBallot.power,
	}
	referenceMedian := referenceTally.weightedMedian()
	tallies := make([]pricedTally, 0, len(passing))

	for _, targetIndex := range passing {
		if targetIndex == referenceIndex {
			tallies = append(tallies, pricedTally{
				targetIndex: targetIndex,
				votes:       referenceTally.votes,
				median:      referenceMedian,
				price:       referenceMedian,
			})
			continue
		}

		start := len(voteArena)
		var power int64
		voteArena, power = ballots[targetIndex].appendCrossRates(referenceBallot, voteArena)
		crossTally := ballot{
			votes: voteArena[start:],
			power: power,
		}
		if crossTally.power < thresholdPower {
			continue
		}

		median := crossTally.weightedMedian()
		// Cross-rate rounding can produce a price above the direct-report bound.
		// Omit it so stored prices remain safe for consumer arithmetic.
		price, err := decimal.Quo(referenceMedian, median)
		if err != nil || !price.IsPositive() || price.GT(oracletypes.MaxExchangeRate) {
			continue
		}
		tallies = append(tallies, pricedTally{
			targetIndex: targetIndex,
			votes:       crossTally.votes,
			median:      median,
			price:       price,
		})
	}

	return tallies
}

// scoreReferences ranks passing targets by raw quorum overlap. Checked
// cross-rate conversion may discard observations, so raw overlap is an upper
// bound on usable power.
func scoreReferences(passing []int, ballots []ballot, thresholdPower int64) []referenceScore {
	scores := make([]referenceScore, len(passing))
	for i, targetIndex := range passing {
		scores[i] = referenceScore{
			targetIndex:      targetIndex,
			qualifiedTargets: 1,
			overlapPower:     math.ZeroInt(),
			rawPower:         ballots[targetIndex].power,
		}
	}

	// Equal validator-ordered voter/power sequences imply equal pairwise overlap.
	if passingBallotsShareSupport(passing, ballots) {
		sharedPower := ballots[passing[0]].power
		overlapPower := math.NewInt(sharedPower).MulRaw(int64(len(passing) - 1))
		for i := range scores {
			scores[i].qualifiedTargets = len(passing)
			scores[i].overlapPower = overlapPower
		}
		return scores
	}

	// Mutable accumulators avoid allocating one immutable math.Int per
	// qualifying pair while supporting totals larger than int64.
	overlapTotals := make([]big.Int, len(passing))
	var overlapValue big.Int
	for i := range passing {
		for j := i + 1; j < len(passing); j++ {
			overlapPower := ballots[passing[i]].overlapPower(ballots[passing[j]])
			if overlapPower >= thresholdPower {
				scores[i].qualifiedTargets++
				scores[j].qualifiedTargets++

				overlapValue.SetInt64(overlapPower)
				overlapTotals[i].Add(&overlapTotals[i], &overlapValue)
				overlapTotals[j].Add(&overlapTotals[j], &overlapValue)
			}
		}
	}
	for i := range scores {
		if overlapTotals[i].Sign() != 0 {
			// Accumulation is complete; the score owns this immutable value.
			scores[i].overlapPower = math.NewIntFromBigIntMut(&overlapTotals[i])
		}
	}

	return scores
}

func passingBallotsShareSupport(passing []int, ballots []ballot) bool {
	first := ballots[passing[0]]
	for _, targetIndex := range passing[1:] {
		candidate := ballots[targetIndex]
		if candidate.power != first.power || len(candidate.votes) != len(first.votes) {
			return false
		}
		for i, vote := range first.votes {
			other := candidate.votes[i]
			if vote.validator != other.validator || vote.power != other.power {
				return false
			}
		}
	}

	return true
}
