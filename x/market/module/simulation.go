package market

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/market/simulation"
	"github.com/ararat-network/ark/x/market/types"
)

// GenerateGenesisState creates a randomised GenState of the market module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// ProposalMsgsX returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory())
	reg.Add(weights.Get("msg_update_policy", 100), simulation.MsgUpdatePolicyFactory())
	reg.Add(weights.Get("msg_set_tobin_tax_override", 50), simulation.MsgSetTobinTaxOverrideFactory(am.k))
	reg.Add(weights.Get("msg_remove_tobin_tax_override", 30), simulation.MsgRemoveTobinTaxOverrideFactory(am.k))
	reg.Add(weights.Get("msg_set_conversion_mandate", 50), simulation.MsgSetConversionMandateFactory())
}

// RegisterStoreDecoder registers a decoder for market module's types
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}

// WeightedOperationsX registers weighted market module operations for simulation.
func (am AppModule) WeightedOperationsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_committee_update_policy", 50), simulation.MsgCommitteeUpdatePolicyFactory(am.k))
	reg.Add(weights.Get("msg_committee_set_tobin_tax", 50), simulation.MsgCommitteeSetTobinTaxFactory(am.k))
	reg.Add(weights.Get("msg_swap", 100), simulation.MsgSwapFactory(am.k))
	reg.Add(weights.Get("msg_swap_send", 100), simulation.MsgSwapSendFactory(am.k))
}
