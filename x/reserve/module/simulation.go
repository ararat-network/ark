package reserve

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/reserve/types"
)

// GenerateGenesisState installs the valid default reserve genesis state.
// Randomising it would need coordinated state — eligibility entries reference
// oracle feeds, and recognised capital is read by Treasury — that the sorted
// per-module generation order cannot provide.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(
		types.DefaultGenesisState(),
	)
}

// RegisterStoreDecoder registers a Collections decoder for reserve state.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

// WeightedOperations returns none: every message is committee-gated, and
// random single-key signing cannot hold the committee seat. The recognition
// arithmetic is fuzzed directly in types instead, and the keeper tests drive
// the committee surface against real wiring.
func (AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
