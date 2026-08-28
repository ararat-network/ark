package security

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/security/simulation"
	"github.com/ararat-network/ark/x/security/types"
)

// GenerateGenesisState creates a randomised GenState of the security module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// RegisterStoreDecoder registers a decoder for the security module's types.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

// WeightedOperations returns none: every message needs either governance
// authority or live upgrade state that random single-key signing cannot set
// up. The app integration tests drive them against real wiring instead.
func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
