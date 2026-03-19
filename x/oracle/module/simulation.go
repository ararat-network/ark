package oracle

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"noah/x/oracle/simulation"
	"noah/x/oracle/types"
)

// GenerateGenesisState creates a randomized GenState of the market module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// ProposalMsgsX returns msgs used for governance proposals for simulations.
func (AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory())
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
	reg.Add(weights.Get("msg_aggregate_exchange_rate_prevote", 100), simulation.MsgAggregateExchangeRatePrevoteFactory(am.k))
	reg.Add(weights.Get("msg_aggregate_exchange_rate_vote", 100), simulation.MsgAggregateExchangeRateVoteFactory(am.k))
	reg.Add(weights.Get("msg_delegate_feed_consent", 50), simulation.MsgDelegateFeedConsentFactory(am.k))
}
