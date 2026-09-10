package claims

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/claims/simulation"
	"github.com/ararat-network/ark/x/claims/types"
)

// GenerateGenesisState creates a randomised GenState of the claims module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// ProposalMsgsX registers governance factories for parameters, mandate appointments, claim
// submission, and cancellation. Committee transactions register in WeightedOperationsX.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory())
	reg.Add(weights.Get("msg_set_claims_mandate", 50), simulation.MsgSetClaimsMandateFactory(am.k))
	reg.Add(weights.Get("msg_submit_claim", 50), simulation.MsgSubmitClaimFactory(am.k))
	reg.Add(weights.Get("msg_cancel_claim", 30), simulation.MsgCancelClaimFactory(am.k))
}

// RegisterStoreDecoder registers a decoder for the claims module's types.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

// WeightedOperationsX registers the Claims committee surface. A committee
// message is a signed transaction rather than a governance proposal, so it
// registers here rather than in ProposalMsgsX.
func (am AppModule) WeightedOperationsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_committee_submit_claim", 50), simulation.MsgCommitteeSubmitClaimFactory(am.k))
	reg.Add(weights.Get("msg_committee_cancel_claim", 30), simulation.MsgCommitteeCancelClaimFactory(am.k))
}

func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
