package oracle

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/decimal"
	oracletypes "ark/x/oracle/types"
)

// aggregationResult contains the internal output of one complete oracle aggregation.
type aggregationResult struct {
	prices map[string]math.LegacyDec
	scores []validatorScore
}

// validatorScore directs oracle rewards and miss accounting to a validator.
type validatorScore struct {
	recipient       sdk.ConsAddress
	votingPower     int64
	rewardedTargets int64
	rewardWeight    math.Int
	missed          bool
}

// aggregateOracleVotes groups submitted oracle rates by supported denom,
// selects a reference denom from the denoms that meet raw and overlap quorum,
// computes weighted-median exchange rates, and returns validator accounting.
// targetDenoms must be in canonical lexical order.
func aggregateOracleVotes(votes []Vote, params oracletypes.Params, targetDenoms []string) aggregationResult {
	result := aggregationResult{
		prices: map[string]math.LegacyDec{},
		scores: make([]validatorScore, len(votes)),
	}

	// Count first so each ballot can allocate exactly enough space for its
	// positive reports without geometric slice growth.
	positiveRateCounts := make([]int, len(targetDenoms))
	var totalPower int64
	for validatorIndex, vote := range votes {
		// Proposal validation matched this voting power against DecidedLastCommit.
		totalPower += vote.Validator.Power

		recipient := sdk.ConsAddress(vote.Validator.Address)
		result.scores[validatorIndex] = validatorScore{
			recipient:    recipient,
			votingPower:  vote.Validator.Power,
			rewardWeight: math.ZeroInt(),
			missed:       len(vote.Rates) != len(targetDenoms),
		}
		for _, submittedRate := range vote.Rates {
			targetIndex := submittedRate.TargetIndex
			if !submittedRate.Value.IsPositive() {
				result.scores[validatorIndex].missed = true
				continue
			}
			positiveRateCounts[targetIndex]++
		}
	}

	if totalPower <= 0 {
		return result
	}

	ballots := make([]ballot, len(targetDenoms))
	for targetIndex, voteCount := range positiveRateCounts {
		ballots[targetIndex] = ballot{
			votes: make([]tallyVote, 0, voteCount),
			rates: make([]math.LegacyDec, len(votes)),
		}
	}
	for validatorIndex, vote := range votes {
		for _, submittedRate := range vote.Rates {
			rate := submittedRate.Value

			// Non-positive submissions were classified as misses in the counting
			// pass and do not contribute to ballots.
			if !rate.IsPositive() {
				continue
			}

			ballots[submittedRate.TargetIndex].add(tallyVote{
				validator: validatorIndex,
				rate:      rate,
				power:     vote.Validator.Power,
			})
		}
	}

	thresholdPower := params.VoteThreshold.
		MulInt64(totalPower).
		Ceil().
		TruncateInt64()

	passingTargets := make([]int, 0, len(ballots))
	for targetIndex := range ballots {
		if ballots[targetIndex].power >= thresholdPower {
			passingTargets = append(passingTargets, targetIndex)
		}
	}
	if len(passingTargets) == 0 {
		return result
	}

	pricedTallies := selectReference(passingTargets, ballots, thresholdPower)

	result.prices = computePricesAndScores(
		pricedTallies,
		targetDenoms,
		params.RewardBand,
		result.scores,
	)

	return result
}

// computePricesAndScores applies fixed-band reward and miss accounting to the
// already evaluated selected-reference tallies.
func computePricesAndScores(
	tallies []pricedTally,
	targetDenoms []string,
	rewardBand math.LegacyDec,
	scores []validatorScore,
) map[string]math.LegacyDec {
	halfRewardBand := rewardBand.QuoInt64(2)
	prices := make(map[string]math.LegacyDec, len(tallies))

	for _, tally := range tallies {
		rewardSpread, err := decimal.Mul(tally.median, halfRewardBand)
		if err != nil {
			continue
		}
		lowerBound, err := decimal.Sub(tally.median, rewardSpread)
		if err != nil {
			continue
		}
		upperBound, err := decimal.Add(tally.median, rewardSpread)
		if err != nil {
			continue
		}
		for _, vote := range tally.votes {
			if vote.rate.GTE(lowerBound) && vote.rate.LTE(upperBound) {
				scores[vote.validator].rewardedTargets++
			} else {
				scores[vote.validator].missed = true
			}
		}

		prices[targetDenoms[tally.targetIndex]] = tally.price
	}
	// Every tally vote for a validator carries the same proposal-validated
	// voting power, so repeated reward additions are exactly one multiplication.
	for i := range scores {
		scores[i].rewardWeight = math.NewInt(scores[i].votingPower).MulRaw(scores[i].rewardedTargets)
	}

	return prices
}
