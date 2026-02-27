package keeper

import (
	"context"
	"fmt"

	"noah/x/market/types"
)

// InitGenesis initializes the market module genesis
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) {
	if err := k.Params.Set(ctx, data.Params); err != nil {
		panic(fmt.Errorf("setting params: %w", err))
	}
	if err := k.NoahPoolDelta.Set(ctx, data.NoahPoolDelta); err != nil {
		panic(fmt.Errorf("setting noah pool delta: %w", err))
	}

	// Lazily create the module account in the auth store if it doesn't exist yet.
	k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(fmt.Errorf("getting params: %w", err))
	}
	noahPoolDelta, err := k.NoahPoolDelta.Get(ctx)
	if err != nil {
		panic(fmt.Errorf("getting noah pool delta: %w", err))
	}

	return types.NewGenesisState(noahPoolDelta, params)
}
