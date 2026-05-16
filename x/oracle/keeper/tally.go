package keeper

import (
	"context"
	"fmt"
	"sort"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

// UpdateExchangeRates computes new exchange rates from validator votes using cross-rate medians and writes them to the store.
func (k Keeper) UpdateExchangeRates(
	ctx context.Context,
	rewardBand,
	voteThreshold math.LegacyDec,
	voteTargets map[string]math.LegacyDec,
	validatorScoreMap map[string]types.ValidatorScore,
) error {
	// Clear all exchange rates
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, _ types.ExchangeRate) (bool, error) {
		if err := k.ExchangeRate.Remove(ctx, denom); err != nil {
			return true, fmt.Errorf("removing exchange rate: %w", err)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating exchange rate: %w", err)
	}

	voteMap, err := k.GroupVotesByDenom(ctx, validatorScoreMap)
	if err != nil {
		return err
	}

	referenceDenom, err := k.PickReferenceDenom(ctx, voteThreshold, voteTargets, voteMap)
	if err != nil {
		return err
	}
	if referenceDenom == "" {
		return nil
	}

	referenceVotes := voteMap[referenceDenom]
	referenceRates := referenceVotes.ValidatorMap()
	referenceMedian := referenceVotes.WeightedMedian()

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for denom, votes := range voteMap {
		// Convert votes to cross exchange rates
		if denom != referenceDenom {
			votes = votes.CrossRate(referenceRates)
		}

		// Get weighted median of cross exchange rates
		exchangeRate := k.ScoreVotes(ctx, rewardBand, votes, validatorScoreMap)

		// Transform into the original form uark/stablecoin
		if denom != referenceDenom {
			exchangeRate = referenceMedian.Quo(exchangeRate)
		}

		// Set the exchange rate, emit ABCI event
		if err := k.SetExchangeRateWithEvent(ctx, types.NewExchangeRate(
			denom,
			exchangeRate,
			sdkCtx.BlockTime(),
			uint64(sdkCtx.BlockHeight()),
		)); err != nil {
			return fmt.Errorf("setting exchange rate: %w", err)
		}
	}

	return nil
}

// GroupVotesByDenom groups all oracle votes for the period by denom. Inactive or jailed validators
// are filtered out, and abstain votes are given zero vote power.
func (k Keeper) GroupVotesByDenom(ctx context.Context, validatorScoreMap map[string]types.ValidatorScore) (map[string]types.DenomVotes, error) {
	votes := make(map[string]types.DenomVotes)
	iteratorHandler := func(voterAddr sdk.ValAddress, vote types.Vote) (bool, error) {
		if score, ok := validatorScoreMap[vote.Voter]; ok {
			for _, er := range vote.ExchangeRates {
				power := score.Power
				if !er.Rate.IsPositive() {
					// Make the power of abstain vote zero
					power = 0
				}

				votes[er.Denom] = append(votes[er.Denom],
					types.NewDenomVote(
						er.Rate,
						er.Denom,
						voterAddr,
						power,
					),
				)
			}
		}

		return false, nil
	}

	if err := k.Vote.Walk(ctx, nil, iteratorHandler); err != nil {
		return nil, fmt.Errorf("iterating: %w", err)
	}

	// sort grouped votes
	for denom, denomVotes := range votes {
		sort.Sort(denomVotes)
		votes[denom] = denomVotes
	}

	return votes, nil
}

// PickReferenceDenom selects the denom with the highest voting power as the reference for cross-rate calculation.
// Ties are broken alphabetically. Denoms that fail to meet quorum are removed from voteTargets and voteMap.
func (k Keeper) PickReferenceDenom(ctx context.Context, voteThreshold math.LegacyDec, voteTargets map[string]math.LegacyDec, voteMap map[string]types.DenomVotes) (string, error) {
	largestVotePower := math.ZeroInt()
	referenceNoah := ""

	totalValidatorPower, err := k.stakingKeeper.TotalValidatorPower(ctx)
	if err != nil {
		return "", err
	}
	totalBondedPower := sdk.TokensToConsensusPower(totalValidatorPower, k.stakingKeeper.PowerReduction(ctx))
	thresholdVotes := voteThreshold.MulInt64(totalBondedPower).RoundInt()

	for denom, votes := range voteMap {
		// skip denoms not in the vote target set
		if _, exists := voteTargets[denom]; !exists {
			delete(voteMap, denom)
			continue
		}

		votesPower := math.NewInt(votes.Power())

		// remove denoms that didn't meet quorum so validators aren't penalised for skipping them
		if votesPower.IsZero() || votesPower.LT(thresholdVotes) {
			delete(voteTargets, denom)
			delete(voteMap, denom)
			continue
		}

		if votesPower.GT(largestVotePower) || largestVotePower.IsZero() {
			referenceNoah = denom
			largestVotePower = votesPower
		} else if largestVotePower.Equal(votesPower) && referenceNoah > denom {
			referenceNoah = denom
		}
	}

	return referenceNoah, nil
}

// ScoreVotes computes the weighted median exchange rate and credits validators who voted within the reward spread.
func (k Keeper) ScoreVotes(
	ctx context.Context,
	rewardBand math.LegacyDec,
	denomVotes types.DenomVotes,
	validatorScoreMap map[string]types.ValidatorScore,
) (weightedMedian math.LegacyDec) {
	weightedMedian = denomVotes.WeightedMedian()
	standardDeviation := denomVotes.StandardDeviation(weightedMedian)
	rewardSpread := weightedMedian.Mul(rewardBand.QuoInt64(2))

	if standardDeviation.GT(rewardSpread) {
		rewardSpread = standardDeviation
	}

	for _, vote := range denomVotes {
		// Filter vote winners & abstains
		if (vote.ExchangeRate.GTE(weightedMedian.Sub(rewardSpread)) &&
			vote.ExchangeRate.LTE(weightedMedian.Add(rewardSpread))) ||
			!vote.ExchangeRate.IsPositive() {

			voter := vote.Voter.String()
			score := validatorScoreMap[voter]
			score.Weight += vote.Power
			score.WinCount++
			validatorScoreMap[voter] = score
		}
	}

	return weightedMedian
}

// ClearVotes clears all tallied prevotes and votes from the store
func (k Keeper) ClearVotes(ctx context.Context, votePeriod uint64) error {
	// Clear all prevotes
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.Prevote.Walk(
		ctx,
		nil,
		func(voterAddr sdk.ValAddress, prevote types.Prevote) (bool, error) {
			if sdkCtx.BlockHeight() > int64(prevote.SubmitBlock+votePeriod) {
				if err := k.Prevote.Remove(ctx, voterAddr); err != nil {
					return false, err
				}
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	// Clear all votes
	if err := k.Vote.Walk(
		ctx,
		nil,
		func(voterAddr sdk.ValAddress, _ types.Vote) (bool, error) {
			if err := k.Vote.Remove(ctx, voterAddr); err != nil {
				return false, err
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	return nil
}
