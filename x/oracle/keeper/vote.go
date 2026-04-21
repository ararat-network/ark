package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

// PickReferenceDenom choose reference stablecoin denom with the highest voter turnout. If the voting power of
// the two denominations is the same, select in alphabetical order.
func (k Keeper) PickReferenceDenom(ctx context.Context, voteTargets map[string]math.LegacyDec, voteMap map[string]types.DenomVotes) (string, error) {
	largestBallotPower := math.ZeroInt()
	referenceNoah := ""

	totalBondedPower := sdk.TokensToConsensusPower(k.stakingKeeper.TotalBondedTokens(ctx), k.stakingKeeper.PowerReduction(ctx))
	params, err := k.Params.Get(ctx)
	if err != nil {
		return "", fmt.Errorf("getting params: %w", err)
	}
	thresholdVotes := params.VoteThreshold.MulInt64(totalBondedPower).RoundInt()

	for denom, ballot := range voteMap {
		// If denom is not in the voteTargets, or the ballot for it has failed, then skip
		// and remove it from voteMap for iteration efficiency
		if _, exists := voteTargets[denom]; !exists {
			delete(voteMap, denom)
			continue
		}

		ballotPower := math.NewInt(ballot.Power())

		// If the ballot is not passed, remove it from the voteTargets array
		// to prevent slashing validators who did valid vote.
		if ballotPower.IsZero() || ballotPower.LT(thresholdVotes) {
			delete(voteTargets, denom)
			delete(voteMap, denom)
			continue
		}

		if ballotPower.GT(largestBallotPower) || largestBallotPower.IsZero() {
			referenceNoah = denom
			largestBallotPower = ballotPower
		} else if largestBallotPower.Equal(ballotPower) && referenceNoah > denom {
			referenceNoah = denom
		}
	}

	return referenceNoah, nil
}

// BuildVoteScoreMap builds a map of validator claims for all bonded validators in the active set.
func (k Keeper) BuildVoteScoreMap(ctx context.Context) (map[string]types.ValidatorScore, error) {
	validatorClaimMap := make(map[string]types.ValidatorScore)

	maxValidators := k.stakingKeeper.MaxValidators(ctx)
	iterator := k.stakingKeeper.ValidatorsPowerStoreIterator(ctx)
	defer iterator.Close()

	powerReduction := k.stakingKeeper.PowerReduction(ctx)

	i := 0
	for ; iterator.Valid() && i < int(maxValidators); iterator.Next() {
		validator := k.stakingKeeper.Validator(ctx, iterator.Value())

		if validator.IsBonded() {
			operator := validator.GetOperator()
			addrBytes, err := k.stakingKeeper.ValidatorAddressCodec().StringToBytes(operator)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %w", err)
			}
			validatorClaimMap[operator] = types.NewValidatorScore(
				validator.GetConsensusPower(powerReduction),
				0,
				0,
				sdk.ValAddress(addrBytes),
			)
			i++
		}
	}

	return validatorClaimMap, nil
}

// CountMisses increments the miss count for validators who failed to vote on all passing denoms.
func (k Keeper) CountMisses(ctx context.Context, voteTargets map[string]math.LegacyDec, validatorClaimMap map[string]types.ValidatorScore) error {
	voteTargetsLen := len(voteTargets)
	for _, claim := range validatorClaimMap {
		// Skip abstain & valid voters
		if int(claim.WinCount) == voteTargetsLen {
			continue
		}

		// Increase miss count
		missCount, err := k.MissCount.Get(ctx, claim.Recipient)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting miss counter: %w", err)
		}
		if err := k.MissCount.Set(ctx, claim.Recipient, missCount+1); err != nil {
			return fmt.Errorf("setting miss counter: %w", err)
		}
	}

	return nil
}

// TallyVotes calculates the median and returns it. Sets the set of voters to be rewarded, i.e. voted within
// a reasonable spread from the weighted median to the store
func TallyVotes(ctx context.Context, rewardBand math.LegacyDec, denomVotes types.DenomVotes, validatorScoreMap map[string]types.ValidatorScore) (weightedMedian math.LegacyDec) {
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
			claim := validatorScoreMap[voter]
			claim.Weight += vote.Power
			claim.WinCount++
			validatorScoreMap[voter] = claim
		}
	}

	return weightedMedian
}
