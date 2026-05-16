package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

// InitGenesis initialises default parameters and the keeper's address to pubkey map
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	for _, fd := range data.FeederDelegations {
		voter, err := sdk.ValAddressFromBech32(fd.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing feeder delegation validator address %q: %w", fd.ValidatorAddress, err)
		}

		feeder, err := sdk.AccAddressFromBech32(fd.FeederAddress)
		if err != nil {
			return fmt.Errorf("parsing feeder delegation feeder address %q: %w", fd.FeederAddress, err)
		}

		if err := k.FeederDelegation.Set(ctx, voter, feeder); err != nil {
			return fmt.Errorf("setting feeder delegation for validator %s: %w", voter, err)
		}
	}

	for _, er := range data.ExchangeRates {
		if err := k.ExchangeRate.Set(ctx, er.Denom, er); err != nil {
			return fmt.Errorf("setting genesis exchange rate for denom %s: %w", er.Denom, err)
		}
	}

	for _, mc := range data.MissCounts {
		operator, err := sdk.ValAddressFromBech32(mc.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing miss count validator address %q: %w", mc.ValidatorAddress, err)
		}

		if err := k.MissCount.Set(ctx, operator, mc.MissCount); err != nil {
			return fmt.Errorf("setting genesis miss counter for validator %s: %w", operator, err)
		}
	}

	for _, ap := range data.Prevotes {
		valAddr, err := sdk.ValAddressFromBech32(ap.Voter)
		if err != nil {
			return fmt.Errorf("parsing prevote voter address %q: %w", ap.Voter, err)
		}

		if err := k.Prevote.Set(ctx, valAddr, ap); err != nil {
			return fmt.Errorf("setting genesis prevote for validator %s: %w", valAddr, err)
		}
	}

	for _, av := range data.Votes {
		valAddr, err := sdk.ValAddressFromBech32(av.Voter)
		if err != nil {
			return fmt.Errorf("parsing vote voter address %q: %w", av.Voter, err)
		}

		if err := k.Vote.Set(ctx, valAddr, av); err != nil {
			return fmt.Errorf("setting genesis vote for validator %s: %w", valAddr, err)
		}
	}

	if len(data.TobinTaxes) > 0 {
		for _, tt := range data.TobinTaxes {
			if err := k.TobinTax.Set(ctx, tt.Denom, tt.TobinTax); err != nil {
				return fmt.Errorf("setting genesis tobin tax for denom %s: %w", tt.Denom, err)
			}
		}
	} else {
		for _, item := range data.Params.TobinTaxes {
			if err := k.TobinTax.Set(ctx, item.Denom, item.TobinTax); err != nil {
				return fmt.Errorf("setting params tobin tax for denom %s: %w", item.Denom, err)
			}
		}
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}

	// check if the module account exists
	moduleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	if moduleAcc == nil {
		return fmt.Errorf("%s module account has not been set", types.ModuleName)
	}

	return nil
}

// ExportGenesis writes the current store values to a genesis file, which can be imported again with InitGenesis
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}

	feederDelegations := []types.FeederDelegation{}
	if err := k.FeederDelegation.Walk(ctx, nil, func(valAddr sdk.ValAddress, feederAddr sdk.AccAddress) (bool, error) {
		feederDelegations = append(feederDelegations, types.FeederDelegation{
			FeederAddress:    feederAddr.String(),
			ValidatorAddress: valAddr.String(),
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating feeder delegations: %w", err)
	}

	exchangeRates := []types.ExchangeRate{}
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, exchangeRate types.ExchangeRate) (bool, error) {
		exchangeRates = append(exchangeRates, types.ExchangeRate{
			Denom:          denom,
			Rate:           exchangeRate.Rate,
			BlockTimestamp: exchangeRate.BlockTimestamp,
			BlockHeight:    exchangeRate.BlockHeight,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating exchange rates: %w", err)
	}

	missCounts := []types.MissCount{}
	if err := k.MissCount.Walk(ctx, nil, func(operator sdk.ValAddress, missCounter uint64) (bool, error) {
		missCounts = append(missCounts, types.MissCount{
			ValidatorAddress: operator.String(),
			MissCount:        missCounter,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating miss counters: %w", err)
	}

	aggregateExchangeRatePrevotes := []types.Prevote{}
	if err := k.Prevote.Walk(ctx, nil, func(_ sdk.ValAddress, aggregatePrevote types.Prevote) (bool, error) {
		aggregateExchangeRatePrevotes = append(aggregateExchangeRatePrevotes, aggregatePrevote)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating prevotes: %w", err)
	}

	aggregateExchangeRateVotes := []types.Vote{}
	if err := k.Vote.Walk(ctx, nil, func(_ sdk.ValAddress, aggregateVote types.Vote) (bool, error) {
		aggregateExchangeRateVotes = append(aggregateExchangeRateVotes, aggregateVote)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating votes: %w", err)
	}

	tobinTaxes := []types.TobinTax{}
	if err := k.TobinTax.Walk(ctx, nil, func(denom string, tobinTax math.LegacyDec) (bool, error) {
		tobinTaxes = append(tobinTaxes, types.TobinTax{Denom: denom, TobinTax: tobinTax})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating tobin taxes: %w", err)
	}

	return types.NewGenesisState(
		params,
		exchangeRates,
		feederDelegations,
		missCounts,
		aggregateExchangeRatePrevotes,
		aggregateExchangeRateVotes,
		tobinTaxes,
	), nil
}
