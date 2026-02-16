package keeper

import (
	"context"
	"fmt"
	marketv1 "noah/api/noah/market/v1"
	"noah/x/market/types"

	"cosmossdk.io/math"
)

// InitGenesis new mint genesis
func (keeper Keeper) InitGenesis(ctx context.Context, data *marketv1.GenesisState) {
	if err := keeper.Params.Set(ctx, data.Params); err != nil {
		panic(fmt.Errorf("Getting Params: %w", err))
	}
	noahPoolDelta, err := math.LegacyNewDecFromStr(data.NoahPoolDelta)
	if err != nil {
		panic(fmt.Errorf("Converting String: %w", err))
	}
	if err := keeper.NoahPoolDelta.Set(ctx, noahPoolDelta); err != nil {
		panic(fmt.Errorf("Getting NoahPoolDelta: %w", err))
	}

	keeper.AccountKeeper.GetModuleAccount(ctx, types.ModuleName)
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func (keeper Keeper) ExportGenesis(ctx context.Context) *marketv1.GenesisState {
	params, err := keeper.Params.Get(ctx)
	if err != nil {
		panic(fmt.Errorf("Getting Params: %w", err))
	}
	noahPoolDelta, err := keeper.NoahPoolDelta.Get(ctx)
	if err != nil {
		panic(fmt.Errorf("Getting NoahPoolDelta: %w", err))
	}

	return types.NewGenesisState(noahPoolDelta, params)
}
