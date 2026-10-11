package disbursement

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/disbursement/simulation"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// GenerateGenesisState appoints a committee and a registrar from the run's accounts and opens a
// member tranche from the seeded member custody.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// RegisterStoreDecoder registers a Collections decoder for disbursement state.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

// ProposalMsgsX registers the governance surface. Directly signed messages register in
// WeightedOperationsX.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory())
	reg.Add(weights.Get("msg_set_grants_mandate", 50), simulation.MsgSetGrantsMandateFactory())
	reg.Add(weights.Get("msg_open_tranche", 50), simulation.MsgOpenTrancheFactory(am.k))
	reg.Add(weights.Get("msg_return_unallocated", 20), simulation.MsgReturnUnallocatedFactory(am.k))
	reg.Add(weights.Get("msg_grant_ownership", 25), simulation.MsgGrantOwnershipFactory(am.k))
	reg.Add(weights.Get("msg_compensate", 25), simulation.MsgCompensateFactory(am.k))
	reg.Add(weights.Get("msg_cancel_grants", 20), simulation.MsgCancelGrantsFactory(am.k))
	reg.Add(weights.Get("msg_reinstate_members", 20), simulation.MsgReinstateMembersFactory(am.k))
}

// WeightedOperationsX registers the messages the committee, the registrar, and a beneficiary's
// controller sign. Registration leads: it creates the members the committee then acts on.
func (am AppModule) WeightedOperationsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_register_members", 60), simulation.MsgRegisterMembersFactory(am.k))
	reg.Add(weights.Get("msg_committee_registrar", 20), simulation.MsgCommitteeRegistrarFactory(am.k))
	reg.Add(weights.Get("msg_committee_suspend", 20), simulation.MsgCommitteeSuspendFactory(am.k))
	reg.Add(weights.Get("msg_committee_reinstate", 20), simulation.MsgCommitteeReinstateFactory(am.k))
	reg.Add(weights.Get("msg_committee_compensate", 40), simulation.MsgCommitteeCompensateFactory(am.k))
	reg.Add(weights.Get("msg_set_payee", 30), simulation.MsgSetPayeeFactory(am.k))
	reg.Add(weights.Get("msg_set_controller", 20), simulation.MsgSetControllerFactory(am.k))
}

// WeightedOperations is empty because simulation uses ProposalMsgsX for governance and
// WeightedOperationsX for directly signed transactions.
func (AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
