// Package reserve is the strategic Reserve: custody the protocol may deploy,
// and the accounting that decides how much of it counts.
//
// It owns the strategic_reserve account and its send restriction (NOAH and
// registry members; external symbols are refused), the one-committee Reserve
// mandate, the Reserve-to-Buffer and Reserve-to-Insurance transfers
// (governance's outright, the committee's inside the mandate floor), the
// append-only quantity journal of positions (deployment, return attribution,
// impairment, closure), the recognition policy of eligibility entries with
// haircuts, jointly solved caps, and per-entry staleness, and the burn
// authority split between committee and governance. Recognition degrades to
// zero, never to a stale figure, and no valuation is stored.
//
// Treasury sizes the Reserve target and this module reports the capital it
// recognises through one one-way interface; it reads Treasury's required
// capital and shortfalls back only to bound its own burns and transfers. It
// owns no tax, target, waterfall, liability valuation, or claim.
package reserve

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/types"
)

const consensusVersion = 1

var (
	_ module.AppModuleBasic = AppModule{}
	_ module.HasGenesis     = AppModule{}

	_ appmodule.AppModule = AppModule{}
)

// AppModule implements an application module for the reserve module. It has
// no per-block work: everything is transaction-driven.
type AppModule struct {
	k *keeper.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{
		k: k,
	}
}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

// Name returns the reserve module's name.
func (am AppModule) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the module's types on the given LegacyAmino codec.
func (am AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the reserve
// module.
func (am AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// RegisterInterfaces registers the module's interface types
func (am AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	types.RegisterMsgServer(registrar, keeper.NewMsgServerImpl(am.k))
	types.RegisterQueryServer(registrar, keeper.NewQueryServerImpl(am.k))
	return nil
}

// DefaultGenesis returns default genesis state as raw bytes for the reserve
// module.
func (am AppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis performs genesis state validation for the reserve module.
func (am AppModule) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var data types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return data.Validate()
}

// InitGenesis performs genesis initialization for the reserve module. It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	var genesisState types.GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)
	if err := am.k.InitGenesis(ctx, &genesisState); err != nil {
		panic(err)
	}
}

// ExportGenesis returns the exported genesis state as raw bytes for the reserve
// module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs, err := am.k.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	return cdc.MustMarshalJSON(gs)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return consensusVersion }
