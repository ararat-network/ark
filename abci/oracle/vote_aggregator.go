package oracle

import (
	"fmt"
	"maps"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "noah/abci/oracle/encoding"
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

// aggregateOracleVotes groups submitted oracle rates by denom, selects a
// reference denom from the denoms that meet the vote threshold, computes
// weighted-median exchange rates, and returns validator scores for reward/miss
// accounting.
func (va *VoteAggregator) aggregateOracleVotes(
	_ sdk.Context,
	votes []Vote,
	params oracletypes.Params,
	voteTargets map[string]math.LegacyDec,
) (map[string]math.LegacyDec, map[string]validatorScore, error) {
	// Build validator scores, group submitted rates by denom, and
	// track total extended-commit voting power and per-validator reported rates.
	voteMap := make(map[string]denomVotes)
	scoreMap := make(map[string]validatorScore)
	validatorRates := make(map[string]map[string]math.LegacyDec)
	totalPower := math.ZeroInt()

	for _, vote := range votes {
		totalPower = totalPower.AddRaw(vote.Validator.Power)
		consAddr := sdk.ConsAddress(vote.Validator.Address)
		consAddrStr := consAddr.String()

		scoreMap[consAddrStr] = newValidatorScore(
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
			dv := newDenomVote(
				rate,
				denom,
				consAddr,
				uint64(vote.Validator.Power),
			)
			if !dv.ExchangeRate.IsPositive() {
				dv.power = 0
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

	prices := computePricesAndScores(referenceDenom, voteMap, params.RewardBand, thresholdVotes, scoreMap)

	return prices, scoreMap, nil
}

type referenceDenomScore struct {
	denom        string
	priceable    int
	overlapPower math.Int
	rawPower     math.Int
}

func (s referenceDenomScore) betterThan(other referenceDenomScore) bool {
	if other.denom == "" {
		return true
	}
	if s.priceable != other.priceable {
		return s.priceable > other.priceable
	}
	if !s.overlapPower.Equal(other.overlapPower) {
		return s.overlapPower.GT(other.overlapPower)
	}
	if !s.rawPower.Equal(other.rawPower) {
		return s.rawPower.GT(other.rawPower)
	}

	return s.denom < other.denom
}

// pickReferenceDenom selects the supported denom that can price the most other
// passing denoms through validator overlap. It mutates voteMap by removing
// unsupported or failed-quorum denoms.
func pickReferenceDenom(
	voteMap map[string]denomVotes,
	voteTargets map[string]math.LegacyDec,
	thresholdVotes math.Int,
) string {
	rawPowers := make(map[string]math.Int, len(voteMap))
	referenceRates := make(map[string]map[string]math.LegacyDec, len(voteMap))

	for denom := range voteMap {
		if _, exists := voteTargets[denom]; !exists {
			delete(voteMap, denom)
		}
	}

	for denom, votes := range voteMap {
		votesPower := math.NewInt(int64(votes.power()))

		// Remove denoms that did not meet quorum to prevent pricing those denoms.
		if votesPower.IsZero() || votesPower.LT(thresholdVotes) {
			delete(voteMap, denom)
			continue
		}

		rawPowers[denom] = votesPower
		referenceRates[denom] = votes.validatorMap()
	}

	best := referenceDenomScore{}
	for denom := range voteMap {
		score := referenceDenomScore{
			denom:        denom,
			priceable:    1,
			overlapPower: math.ZeroInt(),
			rawPower:     rawPowers[denom],
		}

		for otherDenom, otherVotes := range voteMap {
			if otherDenom == denom {
				continue
			}

			overlapPower := math.NewInt(int64(otherVotes.overlapPower(referenceRates[denom])))
			if overlapPower.GTE(thresholdVotes) {
				score.priceable++
				score.overlapPower = score.overlapPower.Add(overlapPower)
			}
		}

		if score.betterThan(best) {
			best = score
		}
	}

	return best.denom
}

// computePricesAndScores computes final exchange rates from passing denom
// votes and updates validator reward weights.
func computePricesAndScores(
	referenceDenom string,
	voteMap map[string]denomVotes,
	rewardBand math.LegacyDec,
	thresholdVotes math.Int,
	scoreMap map[string]validatorScore,
) map[string]math.LegacyDec {
	referenceVotes := voteMap[referenceDenom]
	referenceRates := referenceVotes.validatorMap()
	referenceMedian := referenceVotes.weightedMedian()

	// Calculate exchange rates using the reference median.
	prices := make(map[string]math.LegacyDec)
	for denom, votes := range voteMap {
		// Convert non-reference denom votes to cross exchange rates.
		if denom != referenceDenom {
			votes = votes.crossRate(referenceRates)
			if math.NewInt(int64(votes.power())).LT(thresholdVotes) {
				continue
			}
		}

		// Get the weighted median of the current exchange rates.
		exchangeRate := votes.weightedMedian()
		standardDeviation := votes.standardDeviation(exchangeRate)
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
				score.Weight += vote.power
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
	maps.Copy(reportedRates, rates)

	return reportedRates
}
