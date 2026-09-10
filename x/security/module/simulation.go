package security

import (
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
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

// ProposalMsgsX registers the Security governance surface for simulation. The
// appointment is the module's only authority message; everything else the
// committee signs, which simulation does not yet exercise.
func (AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_set_security_mandate", 100), simulation.MsgSetSecurityMandateFactory())
}

// WeightedOperations is empty: pending upgrades do not round-trip through SDK export and client
// recovery needs IBC state absent from simulation. App integration tests exercise these powers.
func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
