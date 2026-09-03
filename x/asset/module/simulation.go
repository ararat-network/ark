package asset

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/asset/simulation"
	"github.com/ararat-network/ark/x/asset/types"
)

// GenerateGenesisState installs the launch asset registry with a randomised
// emergency appointment.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomisedGenState(simState)
}

// RegisterStoreDecoder registers a Collections decoder for asset state.
func (am AppModule) RegisterStoreDecoder(
	registry simtypes.StoreDecoderRegistry,
) {
	registry[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(
		am.k.Schema,
	)
}

// ProposalMsgsX registers the Asset governance surface for simulation. Every
// Asset message is authority-signed, so the lifecycle is reachable only as
// governance proposals; the factories read live state and skip a transition
// whose starting status no asset currently occupies.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), simulation.MsgUpdateParamsFactory())
	reg.Add(weights.Get("msg_halt_issuance", 50), simulation.MsgHaltIssuanceFactory(am.k))
	reg.Add(weights.Get("msg_resume_issuance", 50), simulation.MsgResumeIssuanceFactory(am.k))
	reg.Add(weights.Get("msg_suspend_asset", 30), simulation.MsgSuspendAssetFactory(am.k))
	reg.Add(weights.Get("msg_write_off_asset", 20), simulation.MsgWriteOffAssetFactory(am.k))
	reg.Add(weights.Get("msg_recover_asset", 30), simulation.MsgRecoverAssetFactory(am.k))
	reg.Add(weights.Get("msg_cancel_settlement", 20), simulation.MsgCancelSettlementFactory(am.k))
	reg.Add(weights.Get("msg_open_settlement", 20), simulation.MsgOpenSettlementFactory(am.k))
	reg.Add(weights.Get("msg_finalise_retirement", 10), simulation.MsgFinaliseRetirementFactory(am.k))
	reg.Add(weights.Get("msg_set_emergency_mandate", 20), simulation.MsgSetEmergencyMandateFactory())
}

// WeightedOperationsX registers the Asset committee surface. A committee
// message is a signed transaction rather than a governance proposal, so it
// registers here rather than in ProposalMsgsX.
func (am AppModule) WeightedOperationsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_emergency_suspend_asset", 20), simulation.MsgEmergencySuspendAssetFactory(am.k))
}

// WeightedOperations returns no uncoordinated lifecycle operations.
func (AppModule) WeightedOperations(
	_ module.SimulationState,
) []simtypes.WeightedOperation {
	return nil
}
