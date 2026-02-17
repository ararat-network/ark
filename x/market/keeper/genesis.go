package keeper

import (
	"context"
	"fmt"
	marketv1 "noah/api/noah/market/v1"
	"noah/x/market/types"

	"cosmossdk.io/math"
)

// InitGenesis new mint genesis
func (k Keeper) InitGenesis(ctx context.Context, data *marketv1.GenesisState) {
	if err := k.Params.Set(ctx, data.Params); err != nil {
		panic(fmt.Errorf("Getting Params: %w", err))
	}
	noahPoolDelta, err := math.LegacyNewDecFromStr(data.NoahPoolDelta)
	if err != nil {
		panic(fmt.Errorf("Converting String: %w", err))
	}
	if err := k.NoahPoolDelta.Set(ctx, noahPoolDelta); err != nil {
		panic(fmt.Errorf("Getting NoahPoolDelta: %w", err))
	}

	k.AccountKeeper.GetModuleAccount(ctx, types.ModuleName)
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func (k Keeper) ExportGenesis(ctx context.Context) *marketv1.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(fmt.Errorf("Getting Params: %w", err))
	}
	noahPoolDelta, err := k.NoahPoolDelta.Get(ctx)
	if err != nil {
		panic(fmt.Errorf("Getting NoahPoolDelta: %w", err))
	}

	return types.NewGenesisState(noahPoolDelta, params)
}
