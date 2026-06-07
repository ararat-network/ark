package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

// InitGenesis initialises default parameters and the keeper's address to pubkey map
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	for _, er := range data.ExchangeRates {
		if err := k.ExchangeRate.Set(ctx, er.Denom, er); err != nil {
			return fmt.Errorf("setting genesis exchange rate for denom %s: %w", er.Denom, err)
		}
	}

	for _, sw := range data.ScoreWeights {
		operator, err := sdk.ValAddressFromBech32(sw.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing score weight validator address %q: %w", sw.ValidatorAddress, err)
		}

		if err := k.ScoreWeight.Set(ctx, operator, sw.ScoreWeight); err != nil {
			return fmt.Errorf("setting genesis score weight for validator %s: %w", operator, err)
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

	scoreWeights := []types.ScoreWeight{}
	if err := k.ScoreWeight.Walk(ctx, nil, func(operator sdk.ValAddress, score uint64) (bool, error) {
		scoreWeights = append(scoreWeights, types.ScoreWeight{
			ValidatorAddress: operator.String(),
			ScoreWeight:      score,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating score weights: %w", err)
	}

	missCounts := []types.MissCount{}
	if err := k.MissCount.Walk(ctx, nil, func(operator sdk.ValAddress, missCount uint64) (bool, error) {
		missCounts = append(missCounts, types.MissCount{
			ValidatorAddress: operator.String(),
			MissCount:        missCount,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating miss counts: %w", err)
	}

	return types.NewGenesisState(
		params,
		exchangeRates,
		scoreWeights,
		missCounts,
	), nil
}
