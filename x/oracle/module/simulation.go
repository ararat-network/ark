package oracle

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/oracle/simulation"
	"github.com/ararat-network/ark/x/oracle/types"
)

// GenerateGenesisState creates a randomised GenState of the oracle module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// ProposalMsgsX returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory(am.k))
	reg.Add(weights.Get("msg_add_feed", 50), simulation.MsgAddFeedFactory(am.k))
	reg.Add(weights.Get("msg_remove_feed", 30), simulation.MsgRemoveFeedFactory(am.k))
	reg.Add(weights.Get("msg_set_reference_denom", 20), simulation.MsgSetReferenceDenomFactory(am.k))
}

// RegisterStoreDecoder registers a decoder for the oracle module's types
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
