package oracle

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/decimal"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// aggregationResult contains the internal output of one complete oracle aggregation.
type aggregationResult struct {
	// prices is nil until at least one target reaches price quorum.
	prices map[string]math.LegacyDec
	scores []validatorScore
	// functioningBlock is true when participating power reached the attendance
	// threshold, making the block eligible for every commit validator.
	functioningBlock bool
}

// validatorScore directs oracle rewards and attendance accounting to a validator.
type validatorScore struct {
	recipient       sdk.ConsAddress
	votingPower     int64
	rewardedTargets int64
	rewardWeight    math.Int
	// participated is true when the vote carried positive rates for at least
	// the participation-threshold share of the target set; abstentions,
	// omissions, and invalid reports never count toward that floor.
	participated bool
}

// aggregateOracleVotes groups submitted oracle rates by supported denom,
// selects a reference denom from the denoms that meet raw and overlap quorum,
// computes weighted-median exchange rates, and returns validator accounting:
// per-validator reward weight and participation, plus the fleet-wide
// functioningBlock flag that gates attendance eligibility.
// targetDenoms must be in canonical lexical order.
func aggregateOracleVotes(votes []Vote, params oracletypes.Params, targetDenoms []string) aggregationResult {
	result := aggregationResult{
		scores: make([]validatorScore, len(votes)),
	}

	var totalPower int64
	for validatorIndex, vote := range votes {
		// Proposal validation matched this voting power against DecidedLastCommit.
		totalPower += vote.Validator.Power

		result.scores[validatorIndex] = validatorScore{
			recipient:    sdk.ConsAddress(vote.Validator.Address),
			votingPower:  vote.Validator.Power,
			rewardWeight: math.ZeroInt(),
		}
	}
	if len(targetDenoms) == 0 {
		// No targets means no vote can ever carry a valid rate, so no validator
		// is ever marked participated: the block is never functioning, and
		// nobody accrues attendance eligibility this block.
		return result
	}

	// requiredPositiveRates is the participation floor: how many distinct
	// targets a report must price before it counts as participation. Rates are
	// unique per target by decode validation, so counting rates counts targets.
	// The floor of one keeps a zero threshold exactly the single-positive-rate
	// rule, and the threshold cap of one half by param validation keeps the
	// floor a deadman switch rather than a coverage mandate.
	requiredPositiveRates := params.ParticipationThreshold.
		MulInt64(int64(len(targetDenoms))).
		Ceil().
		TruncateInt64()
	if requiredPositiveRates < 1 {
		requiredPositiveRates = 1
	}

	// This pass computes three outputs in one iteration over votes: it counts
	// positive reports per target so each ballot can allocate exactly enough
	// space without geometric slice growth, it marks each validator whose
	// positive-rate count reaches the participation floor as participated, and
	// it sums participating power across the fleet for the functioning-block
	// check below. Below-floor reports must stay out of participating power:
	// that coupling is what lets a fleet-wide coverage collapse switch grading
	// off instead of jailing the affected validators.
	positiveRateCounts := make([]int, len(targetDenoms))
	var participatingPower int64
	for validatorIndex, vote := range votes {
		var positiveRates int64
		for _, submittedRate := range vote.Rates {
			if !submittedRate.Value.IsPositive() {
				continue
			}
			positiveRates++
			positiveRateCounts[submittedRate.TargetIndex]++
		}
		if positiveRates >= requiredPositiveRates {
			result.scores[validatorIndex].participated = true
			participatingPower += vote.Validator.Power
		}
	}
	// A powerless commit never prices targets and is never functioning. This
	// return sits after the participation pass so zero-power reporters still
	// carry their participated flags.
	if totalPower <= 0 {
		return result
	}
	// Attendance is only graded on functioning blocks, so correlated outages
	// judge no one. The threshold is floored at a majority by param validation:
	// no block is ever graded that a majority of commit power could not
	// participate in.
	result.functioningBlock = participatingPower >= params.FunctioningBlockThreshold.
		MulInt64(totalPower).
		Ceil().
		TruncateInt64()

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

			// Abstentions do not contribute to ballots.
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

// computePricesAndScores applies fixed-band reward accounting to the already
// evaluated selected-reference tallies.
func computePricesAndScores(
	tallies []pricedTally,
	targetDenoms []string,
	rewardBand math.LegacyDec,
	scores []validatorScore,
) map[string]math.LegacyDec {
	halfRewardBand := rewardBand.QuoInt64(2)
	prices := make(map[string]math.LegacyDec, len(tallies))

	for _, tally := range tallies {
		// Every tally price was already validated by reference selection, so it
		// publishes unconditionally: unrepresentable reward-band bounds skip only
		// this target's reward accounting, never its price.
		prices[targetDenoms[tally.targetIndex]] = tally.price

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
			}
		}
	}
	// Every tally vote for a validator carries the same proposal-validated
	// voting power, so repeated reward additions are exactly one multiplication.
	for i := range scores {
		scores[i].rewardWeight = math.NewInt(scores[i].votingPower).MulRaw(scores[i].rewardedTargets)
	}

	return prices
}
