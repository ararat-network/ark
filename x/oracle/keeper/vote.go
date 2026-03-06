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
func (k Keeper) PickReferenceDenom(ctx context.Context, voteTargets map[string]math.LegacyDec, voteMap map[string]types.ExchangeRateBallot) (string, error) {
	largestBallotPower := int64(0)
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

		ballotPower := int64(0)

		// If the ballot is not passed, remove it from the voteTargets array
		// to prevent slashing validators who did valid vote.
		if power, ok := ballot.BallotIsPassing(thresholdVotes); ok {
			ballotPower = power.Int64()
		} else {
			delete(voteTargets, denom)
			delete(voteMap, denom)
			continue
		}

		if ballotPower > largestBallotPower || largestBallotPower == 0 {
			referenceNoah = denom
			largestBallotPower = ballotPower
		} else if largestBallotPower == ballotPower && referenceNoah > denom {
			referenceNoah = denom
		}
	}

	return referenceNoah, nil
}

// BuildValidatorClaimMap builds a map of validator claims for all bonded validators in the active set.
func (k Keeper) BuildValidatorClaimMap(ctx context.Context) (map[string]types.Claim, error) {
	validatorClaimMap := make(map[string]types.Claim)

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
			validatorClaimMap[operator] = types.NewClaim(
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

// CountMisses increments the miss counter for validators who failed to vote on all passing denoms.
func (k Keeper) CountMisses(ctx context.Context, voteTargets map[string]math.LegacyDec, validatorClaimMap map[string]types.Claim) error {
	voteTargetsLen := len(voteTargets)
	for _, claim := range validatorClaimMap {
		// Skip abstain & valid voters
		if int(claim.WinCount) == voteTargetsLen {
			continue
		}

		// Increase miss counter
		missCount, err := k.MissCounter.Get(ctx, claim.Recipient)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting miss counter: %w", err)
		}
		if err := k.MissCounter.Set(ctx, claim.Recipient, missCount+1); err != nil {
			return fmt.Errorf("setting miss counter: %w", err)
		}
	}

	return nil
}
