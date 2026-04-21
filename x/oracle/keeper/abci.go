package keeper

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/telemetry"

	core "noah/types"
	"noah/x/oracle/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	if core.IsPeriodLastBlock(ctx, params.VotePeriod) {
		validatorClaimMap, err := k.BuildVoteScoreMap(ctx)
		if err != nil {
			return err
		}

		// Denom-TobinTax map
		voteTargets := make(map[string]math.LegacyDec)
		if err := k.TobinTax.Walk(ctx, nil, func(denom string, tobinTax math.LegacyDec) (bool, error) {
			voteTargets[denom] = tobinTax
			return false, nil
		}); err != nil {
			return fmt.Errorf("iterating tobin tax: %w", err)
		}
		tobinTaxesChanged := !sameTobinTaxes(voteTargets, params.TobinTaxes)

		if err := k.TallyExchangeRates(ctx, params, voteTargets, validatorClaimMap); err != nil {
			return err
		}

		if err := k.CountMisses(ctx, voteTargets, validatorClaimMap); err != nil {
			return err
		}

		// Distribute rewards to ballot winners
		if err := k.RewardBallotWinners(
			ctx,
			int64(params.VotePeriod),
			int64(params.RewardDistributionWindow),
			validatorClaimMap,
		); err != nil {
			return err
		}

		// Clear the ballot
		if err := k.ClearVotes(ctx, params.VotePeriod); err != nil {
			return err
		}

		// Sync TobinTaxes if there were param updates
		if tobinTaxesChanged {
			if err := k.SetTobinTaxes(ctx, params.TobinTaxes); err != nil {
				return err
			}
		}
	}

	// Do slash who did miss voting over threshold and
	// reset miss counters of all validators at the last block of slash window
	if core.IsPeriodLastBlock(ctx, params.SlashWindow) {
		if err := k.SlashAndResetMissCounts(ctx); err != nil {
			return err
		}
	}

	return nil
}

func sameTobinTaxes(stored map[string]math.LegacyDec, params types.TobinTaxes) bool {
	if len(stored) != len(params) {
		return false
	}

	for _, item := range params {
		tobinTax, ok := stored[item.Denom]
		if !ok || !tobinTax.Equal(item.TobinTax) {
			return false
		}
	}

	return true
}

// TallyExchangeRates clears existing exchange rates, organises ballots, picks a reference denom,
// and computes new exchange rates via cross-rate medians.
func (k Keeper) TallyExchangeRates(
	ctx context.Context,
	params types.Params,
	voteTargets map[string]math.LegacyDec,
	validatorClaimMap map[string]types.ValidatorScore,
) error {
	// Clear all exchange rates
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, _ math.LegacyDec) (bool, error) {
		if err := k.ExchangeRate.Remove(ctx, denom); err != nil {
			return true, fmt.Errorf("removing exchange rate: %w", err)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating exchange rate: %w", err)
	}

	voteMap, err := k.GroupVotesByDenom(ctx, validatorClaimMap)
	if err != nil {
		return err
	}

	referenceDenom, err := k.PickReferenceDenom(ctx, voteTargets, voteMap)
	if err != nil {
		return err
	}
	if referenceDenom == "" {
		return nil
	}

	ballotRT := voteMap[referenceDenom]
	voteMapRT := ballotRT.ValidatorMap()
	exchangeRateRT := ballotRT.WeightedMedian()

	for denom, ballot := range voteMap {
		// Convert ballot to cross exchange rates
		if denom != referenceDenom {
			ballot = ballot.CrossRate(voteMapRT)
		}

		// Get weighted median of cross exchange rates
		exchangeRate := TallyVotes(ctx, params.RewardBand, ballot, validatorClaimMap)

		// Transform into the original form uark/stablecoin
		if denom != referenceDenom {
			exchangeRate = exchangeRateRT.Quo(exchangeRate)
		}

		// Set the exchange rate, emit ABCI event
		if err := k.SetExchangeRateWithEvent(ctx, denom, exchangeRate); err != nil {
			return fmt.Errorf("setting exchange rate: %w", err)
		}
	}

	return nil
}
