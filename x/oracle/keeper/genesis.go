package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

// InitGenesis initialize default parameters
// and the keeper's address to pubkey map
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	for _, d := range data.FeederDelegations {
		voter, err := sdk.ValAddressFromBech32(d.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("invalid address: %w", err)
		}

		feeder, err := sdk.AccAddressFromBech32(d.FeederAddress)
		if err != nil {
			return fmt.Errorf("invalid address: %w", err)
		}

		if err := k.FeederDelegation.Set(ctx, voter, feeder); err != nil {
			return fmt.Errorf("setting feeder delegation: %w", err)
		}
	}

	for _, ex := range data.ExchangeRates {
		if err := k.ExchangeRate.Set(ctx, ex.Denom, ex.ExchangeRate); err != nil {
			return fmt.Errorf("setting exchange rate: %w", err)
		}
	}

	for _, mc := range data.MissCounters {
		operator, err := sdk.ValAddressFromBech32(mc.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("invalid address: %w", err)
		}

		if err := k.MissCounter.Set(ctx, operator, mc.MissCounter); err != nil {
			return fmt.Errorf("setting miss counter: %w", err)
		}
	}

	for _, ap := range data.AggregateExchangeRatePrevotes {
		valAddr, err := sdk.ValAddressFromBech32(ap.Voter)
		if err != nil {
			return fmt.Errorf("invalid address: %w", err)
		}

		if err := k.AggregateExchangeRatePrevote.Set(ctx, valAddr, ap); err != nil {
			return fmt.Errorf("setting prevote: %w", err)
		}
	}

	for _, av := range data.AggregateExchangeRateVotes {
		valAddr, err := sdk.ValAddressFromBech32(av.Voter)
		if err != nil {
			return fmt.Errorf("invalid address: %w", err)
		}

		if err := k.AggregateExchangeRateVote.Set(ctx, valAddr, av); err != nil {
			return fmt.Errorf("setting vote: %w", err)
		}
	}

	if len(data.TobinTaxes) > 0 {
		for _, tt := range data.TobinTaxes {
			if err := k.TobinTax.Set(ctx, tt.Denom, tt.TobinTax); err != nil {
				return fmt.Errorf("setting tobin tax: %w", err)
			}
		}
	} else {
		for _, item := range data.Params.Whitelist {
			if err := k.TobinTax.Set(ctx, item.Name, item.TobinTax); err != nil {
				return fmt.Errorf("setting tobin tax: %w", err)
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

// ExportGenesis writes the current store values
// to a genesis file, which can be imported again
// with InitGenesis
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

	exchangeRates := []types.ExchangeRateTuple{}
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, rate math.LegacyDec) (bool, error) {
		exchangeRates = append(exchangeRates, types.ExchangeRateTuple{Denom: denom, ExchangeRate: rate})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating exchange rates: %w", err)
	}

	missCounters := []types.MissCounter{}
	if err := k.MissCounter.Walk(ctx, nil, func(operator sdk.ValAddress, missCounter uint64) (bool, error) {
		missCounters = append(missCounters, types.MissCounter{
			ValidatorAddress: operator.String(),
			MissCounter:      missCounter,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating miss counters: %w", err)
	}

	aggregateExchangeRatePrevotes := []types.AggregateExchangeRatePrevote{}
	if err := k.AggregateExchangeRatePrevote.Walk(ctx, nil, func(_ sdk.ValAddress, aggregatePrevote types.AggregateExchangeRatePrevote) (bool, error) {
		aggregateExchangeRatePrevotes = append(aggregateExchangeRatePrevotes, aggregatePrevote)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating prevotes: %w", err)
	}

	aggregateExchangeRateVotes := []types.AggregateExchangeRateVote{}
	if err := k.AggregateExchangeRateVote.Walk(ctx, nil, func(_ sdk.ValAddress, aggregateVote types.AggregateExchangeRateVote) (bool, error) {
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
		missCounters,
		aggregateExchangeRatePrevotes,
		aggregateExchangeRateVotes,
		tobinTaxes,
	), nil
}
