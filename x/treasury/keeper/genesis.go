package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	core "noah/types"
	"noah/x/treasury/types"
)

// InitGenesis initializes default parameters
// and the keeper's address to pubkey map
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}

	if err := k.TaxRate.Set(ctx, data.TaxRate); err != nil {
		return fmt.Errorf("setting tax rate: %w", err)
	}
	if err := k.RewardWeight.Set(ctx, data.RewardWeight); err != nil {
		return fmt.Errorf("setting reward weight: %w", err)
	}
	epochTaxProceeds := types.EpochTaxProceeds{TaxProceeds: data.EpochTaxProceeds}
	if err := k.EpochTaxProceeds.Set(ctx, epochTaxProceeds); err != nil {
		return fmt.Errorf("setting epoch tax proceeds: %w", err)
	}

	// If EpochInitialIssuance is empty, we use current supply as epoch initial issuance
	if data.EpochInitialIssuance.IsZero() {
		if err := k.RecordEpochInitialIssuance(ctx); err != nil {
			return fmt.Errorf("recording epoch initial issuance: %w", err)
		}
	} else {
		epochInitialIssuance := types.EpochInitialIssuance{Issuance: data.EpochInitialIssuance}
		if err := k.EpochInitialIssuance.Set(ctx, epochInitialIssuance); err != nil {
			return fmt.Errorf("setting epoch initial issuance: %w", err)
		}
	}

	// store tax caps
	for _, cap := range data.TaxCaps {
		if err := k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
			return fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
		}
	}

	for _, epochState := range data.EpochStates {
		if err := k.EpochStates.Set(ctx, epochState.Epoch, epochState); err != nil {
			return fmt.Errorf("setting epoch state %d: %w", epochState.Epoch, err)
		}
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
	taxRate, err := k.TaxRate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting tax rate: %w", err)
	}
	rewardWeight, err := k.RewardWeight.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting reward weight: %w", err)
	}
	taxProceeds, err := k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting tax proceeds: %w", err)
	}
	epochInitialIssuance, err := k.EpochInitialIssuance.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting epoch initial issuance: %w", err)
	}

	var taxCaps []types.TaxCap
	if err := k.TaxCaps.Walk(ctx, nil, func(denom string, taxCap math.Int) (bool, error) {
		taxCaps = append(taxCaps, types.TaxCap{
			Denom:  denom,
			TaxCap: taxCap,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating tax caps: %w", err)
	}

	var epochStates []types.EpochState
	curEpoch := k.GetEpoch(ctx)
	for e := uint64(0); e < curEpoch ||
		(e == curEpoch && core.IsPeriodLastBlock(ctx, core.BlocksPerWeek)); e++ {
		epochState, err := k.EpochStates.Get(ctx, e)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("getting epoch state %d: %w", e, err)
		}
		epochStates = append(epochStates, epochState)
	}

	return types.NewGenesisState(
		params,
		taxRate,
		rewardWeight,
		taxCaps,
		taxProceeds.TaxProceeds,
		epochInitialIssuance.Issuance,
		epochStates,
	), nil
}
