package keeper

import (
	"context"
	"fmt"

	"ark/x/market/types"
)

// InitGenesis initializes the market module genesis
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}
	if err := k.ArkPoolDelta.Set(ctx, data.ArkPoolDelta); err != nil {
		return fmt.Errorf("setting ark pool delta: %w", err)
	}

	// check if the module account exists
	moduleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	if moduleAcc == nil {
		return fmt.Errorf("%s module account has not been set", types.ModuleName)
	}

	return nil
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	arkPoolDelta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting ark pool delta: %w", err)
	}

	return types.NewGenesisState(arkPoolDelta, params), nil
}
