package disbursement

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/x/disbursement/types"
)

// GenerateGenesisState seeds no unfunded obligations.
func (AppModule) GenerateGenesisState(s *module.SimulationState) {
	s.GenState[types.ModuleName] = s.Cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// RegisterStoreDecoder exposes the collections schema to simulation diagnostics.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(am.k.Schema)
}

// ProposalMsgsX exercises bounded operational policy changes through governance.
func (am AppModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_grant_update_params", 100), simsx.SimMsgFactoryFn[*types.MsgUpdateParams](func(ctx context.Context, data *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		p, err := am.k.Params.Get(ctx)
		if err != nil {
			reporter.Skip("disbursement params unavailable")
			return nil, nil
		}
		p.MaxMembers = data.Rand().Uint64InRange(1, types.MaxIssuanceMembers)
		return nil, &types.MsgUpdateParams{Authority: data.ModuleAccountAddress(reporter, "gov"), Params: p}
	}))
}

// WeightedOperations leaves funded payment flows to module and application tests.
func (AppModule) WeightedOperations(module.SimulationState) []simtypes.WeightedOperation { return nil }
