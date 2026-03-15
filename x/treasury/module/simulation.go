package treasury

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"noah/x/treasury/simulation"
	"noah/x/treasury/types"
)

// GenerateGenesisState creates a randomized GenState of the treasury module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// ProposalMsgsX returns msgs used for governance proposals for simulations.
func (AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory())
}

// RegisterStoreDecoder registers a decoder for treasury module's types
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
