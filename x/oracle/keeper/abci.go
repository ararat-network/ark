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
		validatorClaimMap, err := k.BuildValidatorClaimMap(ctx)
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
		if err := k.ClearBallots(ctx, params.VotePeriod); err != nil {
			return err
		}

		// Update vote targets and tobin tax
		if err := k.ApplyWhitelist(ctx, params.Whitelist, voteTargets); err != nil {
			return err
		}
	}

	// Do slash who did miss voting over threshold and
	// reset miss counters of all validators at the last block of slash window
	if core.IsPeriodLastBlock(ctx, params.SlashWindow) {
		if err := k.SlashAndResetMissCounters(ctx); err != nil {
			return err
		}
	}

	return nil
}

// TallyExchangeRates clears existing exchange rates, organizes ballots, picks a reference denom,
// and computes new exchange rates via cross-rate medians.
func (k Keeper) TallyExchangeRates(
	ctx context.Context,
	params types.Params,
	voteTargets map[string]math.LegacyDec,
	validatorClaimMap map[string]types.Claim,
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

	voteMap, err := k.OrganizeBallotByDenom(ctx, validatorClaimMap)
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
	voteMapRT := ballotRT.ToMap()
	exchangeRateRT := ballotRT.WeightedMedianWithAssertion()

	for denom, ballot := range voteMap {
		// Convert ballot to cross exchange rates
		if denom != referenceDenom {
			ballot = ballot.ToCrossRateWithSort(voteMapRT)
		}

		// Get weighted median of cross exchange rates
		exchangeRate := ballot.Tally(ctx, params.RewardBand, validatorClaimMap)

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
