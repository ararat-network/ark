package oracle

import (
	"fmt"
	"slices"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "ark/abci/oracle/encoding"
	oracletypes "ark/x/oracle/types"
)

// AggregationResult contains all consensus and telemetry output from one
// complete oracle aggregation.
type AggregationResult struct {
	Prices           map[string]math.LegacyDec
	VoteTargets      []string
	ValidatorReports []ValidatorReport

	scores []validatorScore
}

// ValidatorReport contains one validator's decoded report for telemetry.
type ValidatorReport struct {
	Validator   sdk.ConsAddress
	BlockIDFlag cmtproto.BlockIDFlag
	Rates       map[string]math.LegacyDec
}

// validatorScore directs oracle rewards and miss accounting to a validator.
type validatorScore struct {
	recipient    sdk.ConsAddress
	rewardWeight math.Int
	missed       bool
}

// aggregateOracleVotes groups submitted oracle rates by supported denom,
// selects a reference denom from the denoms that meet raw and overlap quorum,
// computes weighted-median exchange rates, and returns validator accounting.
func aggregateOracleVotes(
	votes []Vote,
	params oracletypes.Params,
	voteTargets []string,
) (AggregationResult, error) {
	targetDenoms := slices.Clone(voteTargets)
	slices.Sort(targetDenoms)
	result := AggregationResult{
		Prices:           map[string]math.LegacyDec{},
		VoteTargets:      targetDenoms,
		ValidatorReports: make([]ValidatorReport, len(votes)),
		scores:           make([]validatorScore, len(votes)),
	}

	targetIndexes := make(map[string]int, len(targetDenoms))
	for i, denom := range targetDenoms {
		targetIndexes[denom] = i
	}

	ballots := make([]ballot, len(targetDenoms))
	allRates := make([]math.LegacyDec, len(targetDenoms)*len(votes))
	for targetIndex := range ballots {
		start := targetIndex * len(votes)
		ballots[targetIndex].rates = allRates[start : start+len(votes)]
	}
	var totalPower int64

	for validatorIndex, vote := range votes {
		// Proposal validation matched this voting power against DecidedLastCommit.
		totalPower += vote.Validator.Power

		consAddr := sdk.ConsAddress(vote.Validator.Address)
		result.scores[validatorIndex] = validatorScore{
			recipient:    consAddr,
			rewardWeight: math.ZeroInt(),
		}
		result.ValidatorReports[validatorIndex] = ValidatorReport{
			Validator:   consAddr,
			BlockIDFlag: vote.BlockIDFlag,
			Rates:       make(map[string]math.LegacyDec, len(vote.OracleVoteExtension.Rates)),
		}
		submitted := 0

		for denom, rawRate := range vote.OracleVoteExtension.Rates {
			rate, err := oracleencoding.DecodeRate(rawRate)
			if err != nil {
				return AggregationResult{}, fmt.Errorf(
					"decode oracle rate for validator %s denom %q: %w",
					consAddr.String(),
					denom,
					err,
				)
			}
			result.ValidatorReports[validatorIndex].Rates[denom] = rate

			targetIndex, supported := targetIndexes[denom]
			if !supported {
				continue
			}

			// Submission protects the validator from participation misses even when
			// the rate is an explicit non-positive abstention.
			submitted++
			if !rate.IsPositive() {
				continue
			}

			ballots[targetIndex].add(tallyVote{
				validator: validatorIndex,
				rate:      rate,
				power:     vote.Validator.Power,
			})
		}

		result.scores[validatorIndex].missed = submitted != len(targetDenoms)
	}

	if totalPower <= 0 {
		return result, nil
	}

	thresholdPower := params.VoteThreshold.
		MulInt64(totalPower).
		Ceil().
		TruncateInt64()

	passing := make([]int, 0, len(ballots))
	for targetIndex := range ballots {
		if ballots[targetIndex].power > 0 && ballots[targetIndex].power >= thresholdPower {
			passing = append(passing, targetIndex)
		}
	}

	selection := selectReference(passing, targetDenoms, ballots, thresholdPower)
	if selection.score.index < 0 {
		return result, nil
	}

	result.Prices = computePricesAndScores(
		selection,
		targetDenoms,
		params.RewardBand,
		result.scores,
	)

	return result, nil
}

type referenceDenomScore struct {
	index        int
	denom        string
	priceable    int
	overlapPower math.Int
	rawPower     int64
}

func (s referenceDenomScore) betterThan(other referenceDenomScore) bool {
	if other.index < 0 {
		return true
	}
	if s.priceable != other.priceable {
		return s.priceable > other.priceable
	}
	if !s.overlapPower.Equal(other.overlapPower) {
		return s.overlapPower.GT(other.overlapPower)
	}
	if s.rawPower != other.rawPower {
		return s.rawPower > other.rawPower
	}

	return s.denom < other.denom
}

type pricedTally struct {
	targetIndex int
	votes       []tallyVote
	median      math.LegacyDec
	price       math.LegacyDec
}

type referenceUpperBound struct {
	score     referenceDenomScore
	qualified []bool
}

type referenceSelection struct {
	score   referenceDenomScore
	tallies []pricedTally
}

// referenceWorkspace owns the cross-rate votes and tally descriptors for one
// reference candidate. Two workspaces are swapped during selection so the
// current winner is retained while the other buffer is reused.
type referenceWorkspace struct {
	votes   []tallyVote
	tallies []pricedTally
}

func (w *referenceWorkspace) reset(voteCapacity, tallyCapacity int) {
	if cap(w.votes) < voteCapacity {
		w.votes = make([]tallyVote, 0, voteCapacity)
	} else {
		w.votes = w.votes[:0]
	}
	if cap(w.tallies) < tallyCapacity {
		w.tallies = make([]pricedTally, 0, tallyCapacity)
	} else {
		w.tallies = w.tallies[:0]
	}
}

func (w *referenceWorkspace) crossRate(target, reference ballot) ballot {
	start := len(w.votes)
	var power int64
	w.votes, power = target.appendCrossRates(reference, w.votes)

	return ballot{votes: w.votes[start:], power: power}
}

// selectReference returns the passing denom that can actually price the most
// other passing denoms. Directional usable overlap gives an upper bound for
// each candidate; candidates that could still beat the best result are then
// evaluated through final median conversion. The winning tallies are retained
// for scoring so cross ballots are not built twice.
func selectReference(passing []int, targetDenoms []string, ballots []ballot, thresholdPower int64) referenceSelection {
	if len(passing) == 0 {
		return referenceSelection{score: referenceDenomScore{index: -1}}
	}

	upperBounds := make([]referenceUpperBound, len(passing))
	qualifiedTargets := make([]bool, len(passing)*len(passing))
	for i, targetIndex := range passing {
		start := i * len(passing)
		upperBounds[i] = referenceUpperBound{
			score: referenceDenomScore{
				index:        targetIndex,
				denom:        targetDenoms[targetIndex],
				priceable:    1,
				overlapPower: math.ZeroInt(),
				rawPower:     ballots[targetIndex].power,
			},
			qualified: qualifiedTargets[start : start+len(passing)],
		}
		upperBounds[i].qualified[i] = true
	}

	for i := range passing {
		for j := i + 1; j < len(passing); j++ {
			leftPower, rightPower := ballots[passing[i]].crossRatePowers(ballots[passing[j]])
			if leftPower >= thresholdPower {
				upperBounds[i].score.priceable++
				upperBounds[i].score.overlapPower = upperBounds[i].score.overlapPower.AddRaw(leftPower)
				upperBounds[i].qualified[j] = true
			}
			if rightPower >= thresholdPower {
				upperBounds[j].score.priceable++
				upperBounds[j].score.overlapPower = upperBounds[j].score.overlapPower.AddRaw(rightPower)
				upperBounds[j].qualified[i] = true
			}
		}
	}

	slices.SortFunc(upperBounds, func(left, right referenceUpperBound) int {
		switch {
		case left.score.betterThan(right.score):
			return -1
		case right.score.betterThan(left.score):
			return 1
		default:
			return 0
		}
	})

	workspaceVoteCapacity := 0
	for _, targetIndex := range passing {
		workspaceVoteCapacity += len(ballots[targetIndex].votes)
	}

	best := referenceSelection{score: referenceDenomScore{index: -1}}
	var bestWorkspace, candidateWorkspace referenceWorkspace
	for _, upperBound := range upperBounds {
		// Actual conversion can only reduce priceable count and qualifying
		// overlap from this upper bound. Sorted later candidates cannot recover.
		if !upperBound.score.betterThan(best.score) {
			break
		}

		candidate := evaluateReferenceCandidate(
			upperBound,
			passing,
			ballots,
			thresholdPower,
			&candidateWorkspace,
			workspaceVoteCapacity,
		)
		if candidate.score.betterThan(best.score) {
			best = candidate
			bestWorkspace, candidateWorkspace = candidateWorkspace, bestWorkspace
		}
	}

	return best
}

func evaluateReferenceCandidate(
	upperBound referenceUpperBound,
	passing []int,
	ballots []ballot,
	thresholdPower int64,
	workspace *referenceWorkspace,
	workspaceVoteCapacity int,
) referenceSelection {
	workspace.reset(workspaceVoteCapacity, len(passing))
	upperScore := upperBound.score
	referenceBallot := ballots[upperScore.index]
	referenceMedian := referenceBallot.weightedMedian()
	selection := referenceSelection{
		score: referenceDenomScore{
			index:        upperScore.index,
			denom:        upperScore.denom,
			priceable:    1,
			overlapPower: math.ZeroInt(),
			rawPower:     upperScore.rawPower,
		},
		tallies: workspace.tallies,
	}

	for passingIndex, targetIndex := range passing {
		if !upperBound.qualified[passingIndex] {
			continue
		}
		if targetIndex == upperScore.index {
			selection.tallies = append(selection.tallies, pricedTally{
				targetIndex: targetIndex,
				votes:       referenceBallot.votes,
				median:      referenceMedian,
				price:       referenceMedian,
			})
			continue
		}

		tallyBallot := workspace.crossRate(ballots[targetIndex], referenceBallot)
		if tallyBallot.power < thresholdPower {
			continue
		}

		median := tallyBallot.weightedMedian()
		price, ok := safePositiveQuotient(referenceMedian, median)
		if !ok {
			continue
		}

		selection.score.priceable++
		selection.score.overlapPower = selection.score.overlapPower.AddRaw(tallyBallot.power)
		selection.tallies = append(selection.tallies, pricedTally{
			targetIndex: targetIndex,
			votes:       tallyBallot.votes,
			median:      median,
			price:       price,
		})
	}
	workspace.tallies = selection.tallies

	return selection
}

// withinSpread reports whether rate is within the inclusive distance from the
// median without constructing overflow-prone median +/- spread endpoints.
func withinSpread(rate, median, spread math.LegacyDec) bool {
	if rate.GTE(median) {
		return rate.Sub(median).LTE(spread)
	}

	return median.Sub(rate).LTE(spread)
}

// computePricesAndScores applies fixed-band reward and miss accounting to the
// already evaluated winning reference tallies.
func computePricesAndScores(
	selection referenceSelection,
	targetDenoms []string,
	rewardBand math.LegacyDec,
	scores []validatorScore,
) map[string]math.LegacyDec {
	halfRewardBand := rewardBand.QuoInt64(2)
	prices := make(map[string]math.LegacyDec, len(selection.tallies))

	for _, tally := range selection.tallies {
		spread := tally.median.Mul(halfRewardBand)
		for _, vote := range tally.votes {
			score := &scores[vote.validator]
			if withinSpread(vote.rate, tally.median, spread) {
				score.rewardWeight = score.rewardWeight.AddRaw(vote.power)
			} else {
				score.missed = true
			}
		}

		prices[targetDenoms[tally.targetIndex]] = tally.price
	}

	return prices
}
