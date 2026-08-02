package asset

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"ark/x/asset/types"
)

// GenerateGenesisState installs the valid default asset genesis state.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(
		types.DefaultGenesisState(),
	)
}

// RegisterStoreDecoder registers a Collections decoder for asset state.
func (am AppModule) RegisterStoreDecoder(
	registry simtypes.StoreDecoderRegistry,
) {
	registry[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(
		am.k.Schema,
	)
}

// WeightedOperations returns no uncoordinated lifecycle operations.
func (AppModule) WeightedOperations(
	_ module.SimulationState,
) []simtypes.WeightedOperation {
	return nil
}
