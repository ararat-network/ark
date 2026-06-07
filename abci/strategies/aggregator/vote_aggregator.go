package aggregator

import (
	"fmt"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/abci/strategies/aggregator/types"
	"noah/abci/strategies/codec"
	noahabci "noah/abci/types"
	vetypes "noah/abci/ve/types"
	oracletypes "noah/x/oracle/types"
)

// Vote is the decoded oracle payload associated with one validator entry from
// the extended commit info injected into the proposal.
type Vote struct {
	// Validator is the CometBFT validator metadata from ExtendedVoteInfo.
	// Address is the consensus address and Power is the consensus voting power
	// for this commit.
	Validator cometabci.Validator

	// OracleVoteExtension is the decoded oracle vote-extension payload. It may
	// be empty when the validator submitted no rates or its extension was
	// absent/pruned before proposal injection.
	OracleVoteExtension vetypes.OracleVoteExtension
}

// GetOracleVotes decodes the injected extended commit info from the proposal
// and returns one Vote per validator entry. It preserves entries with empty
// vote extensions so their voting power can still count toward quorum
// denominators.
func GetOracleVotes(
	proposal [][]byte,
	veCodec codec.VoteExtensionCodec,
	extCommitCodec codec.ExtendedCommitCodec,
) ([]Vote, error) {
	if len(proposal) < noahabci.NumInjectedTxs {
		return nil, noahabci.MissingCommitInfoError{}
	}

	extendedCommitInfo, err := extCommitCodec.Decode(proposal[noahabci.OracleInfoIndex])
	if err != nil {
		return nil, noahabci.CodecError{
			Err: fmt.Errorf("error decoding extended-commit-info: %w", err),
		}
	}

	votes := make([]Vote, len(extendedCommitInfo.Votes))
	for i, voteInfo := range extendedCommitInfo.Votes {
		voteExtension, err := veCodec.Decode(voteInfo.VoteExtension)
		if err != nil {
			return nil, noahabci.CodecError{
				Err: fmt.Errorf("error decoding vote-extension: %w", err),
			}
		}

		votes[i] = Vote{
			Validator:           voteInfo.Validator,
			OracleVoteExtension: voteExtension,
		}
	}

	return votes, nil
}

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
//
// voteTargets is mutated into the accountable target set: denoms that are not
// submitted or fail quorum are removed so callers do not penalise validators
// for those denoms.
func (va *VoteAggregator) AggregateOracleVotes(
	ctx sdk.Context,
	votes []Vote,
	params oracletypes.Params,
	voteTargets map[string]math.LegacyDec,
) (map[string]math.LegacyDec, map[string]types.ValidatorScore, error) {
	voteMap, scoreMap, totalPower, validatorRates := buildVoteMap(votes)
	va.latestValidatorRates = validatorRates

	thresholdVotes := params.VoteThreshold.MulInt(totalPower).RoundInt()
	referenceDenom := pickReferenceDenom(voteMap, voteTargets, thresholdVotes)
	if referenceDenom == "" {
		return map[string]math.LegacyDec{}, scoreMap, nil
	}

	prices := computePricesAndScores(referenceDenom, voteMap, params.RewardBand, scoreMap)

	return prices, scoreMap, nil
}

// buildVoteMap builds validator scores, groups submitted rates by denom, and
// tracks total extended-commit voting power and per-validator reported rates.
func buildVoteMap(votes []Vote) (map[string]types.DenomVotes, map[string]types.ValidatorScore, math.Int, map[string]map[string]math.LegacyDec) {
	voteMap := make(map[string]types.DenomVotes)
	scoreMap := make(map[string]types.ValidatorScore)
	validatorRates := make(map[string]map[string]math.LegacyDec)
	totalPower := math.ZeroInt()

	for _, vote := range votes {
		totalPower = totalPower.AddRaw(vote.Validator.Power)
		valAddr := sdk.ValAddress(vote.Validator.Address)
		valAddrStr := valAddr.String()

		scoreMap[valAddrStr] = types.NewValidatorScore(
			uint64(vote.Validator.Power),
			0,
			0,
			valAddr,
		)
		validatorRates[valAddrStr] = make(map[string]math.LegacyDec, len(vote.OracleVoteExtension.Rates))

		for _, rate := range vote.OracleVoteExtension.Rates {
			dv := types.NewDenomVote(
				rate.Rate,
				rate.Denom,
				valAddr,
				uint64(vote.Validator.Power),
			)
			if !dv.ExchangeRate.IsPositive() {
				dv.Power = 0
			}

			voteMap[rate.Denom] = append(voteMap[rate.Denom], dv)
			validatorRates[valAddrStr][rate.Denom] = rate.Rate
		}
	}

	return voteMap, scoreMap, totalPower, validatorRates
}

// pickReferenceDenom selects the supported denom with the largest voting power
// among denoms that meet quorum. It mutates voteTargets and voteMap by removing
// unsupported or failed-quorum denoms.
func pickReferenceDenom(voteMap map[string]types.DenomVotes, voteTargets map[string]math.LegacyDec, thresholdVotes math.Int) string {
	largestVotePower := math.ZeroInt()
	referenceDenom := ""

	for denom := range voteMap {
		if _, exists := voteTargets[denom]; !exists {
			delete(voteMap, denom)
		}
	}

	for denom := range voteTargets {
		votes, exists := voteMap[denom]
		if !exists {
			delete(voteTargets, denom)
			continue
		}

		votesPower := math.NewInt(int64(votes.Power()))

		// remove denoms that didn't meet quorum so validators aren't penalised for skipping them
		if votesPower.IsZero() || votesPower.LT(thresholdVotes) {
			delete(voteTargets, denom)
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
// votes and updates validator scores for reward/miss accounting.
func computePricesAndScores(
	referenceDenom string,
	voteMap map[string]types.DenomVotes,
	rewardBand math.LegacyDec,
	scoreMap map[string]types.ValidatorScore,
) map[string]math.LegacyDec {
	referenceVotes := voteMap[referenceDenom]
	referenceRates := referenceVotes.ValidatorMap()
	referenceMedian := referenceVotes.WeightedMedian()

	// Calculate exchange rate using the referenceMedian
	prices := make(map[string]math.LegacyDec)
	for denom, votes := range voteMap {
		// Convert votes to cross exchange rates
		if denom != referenceDenom {
			votes = votes.CrossRate(referenceRates)
		}

		// Get weighted median of cross exchange rates
		exchangeRate := votes.WeightedMedian()
		standardDeviation := votes.StandardDeviation(exchangeRate)
		rewardSpread := exchangeRate.Mul(rewardBand.QuoInt64(2))

		if standardDeviation.GT(rewardSpread) {
			rewardSpread = standardDeviation
		}

		for _, vote := range votes {
			// Credit validators whose vote was within the reward band, plus explicit abstains.
			if (vote.ExchangeRate.GTE(exchangeRate.Sub(rewardSpread)) &&
				vote.ExchangeRate.LTE(exchangeRate.Add(rewardSpread))) ||
				!vote.ExchangeRate.IsPositive() {

				voter := vote.Voter.String()
				score := scoreMap[voter]
				score.Weight += vote.Power
				score.WinCount++
				scoreMap[voter] = score
			}
		}

		// Transform into the original form uark/stablecoin
		if denom != referenceDenom {
			exchangeRate = referenceMedian.Quo(exchangeRate)
		}
		prices[denom] = exchangeRate
	}

	return prices
}

// GetPriceForValidator gets the prices reported by a given validator. This method depends
// on the prices from the latest set of aggregated votes.
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
