package reserve

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/reserve/simulation"
	"github.com/ararat-network/ark/x/reserve/types"
)

// GenerateGenesisState installs the valid default reserve state with a
// randomised committee appointment. Everything else stays as shipped:
// eligibility entries reference oracle feeds, and recognised capital is read by
// Treasury, which the sorted per-module generation order cannot provide.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// RegisterStoreDecoder registers a Collections decoder for reserve state.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

// ProposalMsgsX registers the Reserve governance surface for simulation.
//
// The position messages are here rather than absent: committee deployment opens
// the positions they act on, so they reach their subjects instead of skipping
// every draw.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_set_reserve_mandate", 100), simulation.MsgSetReserveMandateFactory())
	reg.Add(weights.Get("msg_set_recognition_policy", 50), simulation.MsgSetRecognitionPolicyFactory(am.k))
	reg.Add(weights.Get("msg_fund_buffer", 50), simulation.MsgFundBufferFactory(am.k))
	reg.Add(weights.Get("msg_fund_insurance", 50), simulation.MsgFundInsuranceFactory(am.k))
	reg.Add(weights.Get("msg_burn_reserve_assets", 20), simulation.MsgBurnReserveAssetsFactory(am.k))
	reg.Add(weights.Get("msg_mark_impaired", 20), simulation.MsgMarkImpairedFactory(am.k))
	reg.Add(weights.Get("msg_clear_impairment", 20), simulation.MsgClearImpairmentFactory(am.k))
	reg.Add(weights.Get("msg_close_position", 15), simulation.MsgClosePositionFactory(am.k))
	reg.Add(weights.Get("msg_correct_position", 15), simulation.MsgCorrectPositionFactory(am.k))
	reg.Add(weights.Get("msg_reverse_return", 15), simulation.MsgReverseReturnFactory(am.k))
}

// WeightedOperationsX registers the Reserve committee surface. A committee
// message is a signed transaction rather than a governance proposal, so it
// registers here rather than in ProposalMsgsX. Deployment leads: it opens the
// positions every other message here acts on.
func (am AppModule) WeightedOperationsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_committee_deploy", 60), simulation.MsgCommitteeDeployFactory(am.k))
	reg.Add(weights.Get("msg_committee_record_update", 30), simulation.MsgCommitteeRecordUpdateFactory(am.k))
	reg.Add(weights.Get("msg_committee_mark_impaired", 20), simulation.MsgCommitteeMarkImpairedFactory(am.k))
	reg.Add(weights.Get("msg_committee_clear_impairment", 20), simulation.MsgCommitteeClearImpairmentFactory(am.k))
	reg.Add(weights.Get("msg_committee_close_position", 20), simulation.MsgCommitteeClosePositionFactory(am.k))
	reg.Add(weights.Get("msg_committee_burn_surplus", 10), simulation.MsgCommitteeBurnSurplusFactory(am.k))
	reg.Add(weights.Get("msg_committee_attribute_return", 30), simulation.MsgCommitteeAttributeReturnFactory(am.k))
	reg.Add(weights.Get("msg_committee_correct_position", 20), simulation.MsgCommitteeCorrectPositionFactory(am.k))
	reg.Add(weights.Get("msg_committee_reverse_return", 15), simulation.MsgCommitteeReverseReturnFactory(am.k))
}

// WeightedOperations returns none: no Reserve message is user-signable. The
// authority half is registered above as governance proposals; the committee
// half needs a seat random single-key signing cannot hold, so the keeper tests
// drive it against real wiring and the recognition arithmetic is fuzzed
// directly in types.
func (AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
