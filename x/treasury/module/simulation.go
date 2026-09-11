package treasury

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/treasury/simulation"
	"github.com/ararat-network/ark/x/treasury/types"
)

// GenerateGenesisState creates a randomised GenState of the treasury module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// ProposalMsgsX registers the governance surface. Committee messages need
// exact governed state and register in WeightedOperationsX instead.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory())
	reg.Add(weights.Get("msg_set_economic_mandate", 50), simulation.MsgSetEconomicMandateFactory())
	reg.Add(weights.Get("msg_update_policy", 100), simulation.MsgUpdatePolicyFactory())
	reg.Add(weights.Get("msg_return_subsidy", 20), simulation.MsgReturnSubsidyFactory(am.k))
}

// RegisterStoreDecoder registers a decoder for treasury module's types
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

// WeightedOperationsX registers the Treasury committee surface. A committee
// message is a signed transaction rather than a governance proposal, so it
// registers here rather than in ProposalMsgsX.
func (am AppModule) WeightedOperationsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_committee_update_policy", 50), simulation.MsgCommitteeUpdatePolicyFactory(am.k))
}

func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
