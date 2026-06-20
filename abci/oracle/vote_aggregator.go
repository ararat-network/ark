package oracle

import (
	"fmt"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "noah/abci/oracle/encoding"
	abcioracletypes "noah/abci/oracle/types"
	oracletypes "noah/x/oracle/types"
)

func NewVoteAggregator(
	logger log.Logger,
) *VoteAggregator {
	return &VoteAggregator{
		logger: logger,
	}
}

// VoteAggregator computes oracle exchange rates and validator scores from
// decoded vote-extension reports.
type VoteAggregator struct {
	logger log.Logger

	// latestValidatorRates stores the rates reported in the latest aggregation
	// by validator consensus address.
	latestValidatorRates map[string]map[string]math.LegacyDec
}

// AggregateOracleVotes groups submitted oracle rates by denom, selects a
// reference denom from the denoms that meet the vote threshold, computes
// weighted-median exchange rates, and returns validator scores for reward/miss
// accounting.
func (va *VoteAggregator) AggregateOracleVotes(
	ctx sdk.Context,
	votes []Vote,
	params oracletypes.Params,
	voteTargets map[string]math.LegacyDec,
) (map[string]math.LegacyDec, map[string]abcioracletypes.ValidatorScore, error) {
	// Build validator scores, group submitted rates by denom, and
	// track total extended-commit voting power and per-validator reported rates.
	voteMap := make(map[string]abcioracletypes.DenomVotes)
	scoreMap := make(map[string]abcioracletypes.ValidatorScore)
	validatorRates := make(map[string]map[string]math.LegacyDec)
	totalPower := math.ZeroInt()

	for _, vote := range votes {
		totalPower = totalPower.AddRaw(vote.Validator.Power)
		consAddr := sdk.ConsAddress(vote.Validator.Address)
		consAddrStr := consAddr.String()

		scoreMap[consAddrStr] = abcioracletypes.NewValidatorScore(
			uint64(vote.Validator.Power),
			0,
			0,
			consAddr,
		)
		validatorRates[consAddrStr] = make(map[string]math.LegacyDec, len(vote.OracleVoteExtension.Rates))

		for denom, rawRate := range vote.OracleVoteExtension.Rates {
			rate, err := oracleencoding.DecodeRate(rawRate)
			if err != nil {
				return nil, nil, fmt.Errorf("decode oracle rate for validator %s denom %q: %w", consAddrStr, denom, err)
			}
			dv := abcioracletypes.NewDenomVote(
				rate,
				denom,
				consAddr,
				uint64(vote.Validator.Power),
			)
			if !dv.ExchangeRate.IsPositive() {
				dv.Power = 0
			}

			voteMap[denom] = append(voteMap[denom], dv)
			validatorRates[consAddrStr][denom] = rate

			if _, exists := voteTargets[denom]; exists {
				// Any submitted target denom counts as participation first. Non-positive
				// rates are explicit abstentions/outage signals: they do not add quorum
				// power, but still protect the validator from miss accounting. Positive
				// out-of-band votes on denoms that pass quorum are handled later.
				score := scoreMap[consAddrStr]
				score.WinCount++
				scoreMap[consAddrStr] = score
			}
		}
	}
	va.latestValidatorRates = validatorRates

	thresholdVotes := params.VoteThreshold.MulInt(totalPower).RoundInt()
	referenceDenom := pickReferenceDenom(voteMap, voteTargets, thresholdVotes)
	if referenceDenom == "" {
		return map[string]math.LegacyDec{}, scoreMap, nil
	}

	prices := computePricesAndScores(referenceDenom, voteMap, params.RewardBand, scoreMap)

	return prices, scoreMap, nil
}

// pickReferenceDenom selects the supported denom with the largest voting power
// among denoms that meet quorum. It mutates voteMap by removing
// unsupported or failed-quorum denoms.
func pickReferenceDenom(
	voteMap map[string]abcioracletypes.DenomVotes,
	voteTargets map[string]math.LegacyDec,
	thresholdVotes math.Int,
) string {
	largestVotePower := math.ZeroInt()
	referenceDenom := ""

	for denom := range voteMap {
		if _, exists := voteTargets[denom]; !exists {
			delete(voteMap, denom)
		}
	}

	for denom, votes := range voteMap {
		votesPower := math.NewInt(int64(votes.Power()))

		// Remove denoms that did not meet quorum to prevent pricing those denoms.
		if votesPower.IsZero() || votesPower.LT(thresholdVotes) {
			delete(voteMap, denom)
			continue
		}

		if votesPower.GT(largestVotePower) || largestVotePower.IsZero() {
			referenceDenom = denom
			largestVotePower = votesPower
		} else if largestVotePower.Equal(votesPower) && referenceDenom > denom {
			referenceDenom = denom
		}
	}

	return referenceDenom
}

// computePricesAndScores computes final exchange rates from passing denom
// votes and updates validator reward weights.
func computePricesAndScores(
	referenceDenom string,
	voteMap map[string]abcioracletypes.DenomVotes,
	rewardBand math.LegacyDec,
	scoreMap map[string]abcioracletypes.ValidatorScore,
) map[string]math.LegacyDec {
	referenceVotes := voteMap[referenceDenom]
	referenceRates := referenceVotes.ValidatorMap()
	referenceMedian := referenceVotes.WeightedMedian()

	// Calculate exchange rates using the reference median.
	prices := make(map[string]math.LegacyDec)
	for denom, votes := range voteMap {
		// Convert non-reference denom votes to cross exchange rates.
		if denom != referenceDenom {
			votes = votes.CrossRate(referenceRates)
		}

		// Get the weighted median of the current exchange rates.
		exchangeRate := votes.WeightedMedian()
		standardDeviation := votes.StandardDeviation(exchangeRate)
		rewardSpread := exchangeRate.Mul(rewardBand.QuoInt64(2))

		if standardDeviation.GT(rewardSpread) {
			rewardSpread = standardDeviation
		}

		for _, vote := range votes {
			// Non-positive votes are abstains after original vote parsing or cross-rate
			// conversion. They should not earn weight, but they still count as submitted.
			if !vote.ExchangeRate.IsPositive() {
				continue
			}
			voter := vote.Voter.String()
			score := scoreMap[voter]
			// Reward validators whose vote was within the reward band.
			if vote.ExchangeRate.GTE(exchangeRate.Sub(rewardSpread)) &&
				vote.ExchangeRate.LTE(exchangeRate.Add(rewardSpread)) {
				score.Weight += vote.Power
			} else if score.WinCount > 0 {
				// This denom passed quorum, so an out-of-band positive vote should not
				// protect the validator from miss counts for this target.
				score.WinCount--
			}
			scoreMap[voter] = score
		}

		// Convert the cross rate back to the denom's exchange rate.
		if denom != referenceDenom {
			exchangeRate = referenceMedian.Quo(exchangeRate)
		}
		prices[denom] = exchangeRate
	}

	return prices
}

// GetPriceForValidator gets the rates reported by a validator in the latest
// aggregation.
func (va *VoteAggregator) GetPriceForValidator(validator sdk.ConsAddress) map[string]math.LegacyDec {
	rates, ok := va.latestValidatorRates[validator.String()]
	if !ok {
		return nil
	}

	reportedRates := make(map[string]math.LegacyDec, len(rates))
	for denom, rate := range rates {
		reportedRates[denom] = rate
	}

	return reportedRates
}
