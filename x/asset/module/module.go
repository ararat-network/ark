package asset

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

	"github.com/ararat-network/ark/x/asset/keeper"
	"github.com/ararat-network/ark/x/asset/types"
)

const consensusVersion = 1

var (
	_ module.AppModuleBasic      = AppModule{}
	_ module.AppModuleSimulation = AppModule{}
	_ module.HasGenesis          = AppModule{}

	_ appmodule.AppModule = AppModule{}
)

// AppModule implements an application module for the asset module.
type AppModule struct {
	k *keeper.Keeper
}

// NewAppModule creates a new Asset AppModule.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{k: k}
}

// IsAppModule implements the appmodule.AppModule interface.
func (AppModule) IsAppModule() {}

// Name returns the asset module's name.
func (AppModule) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the asset module's message types.
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterGRPCGatewayRoutes registers the asset module's gRPC Gateway routes.
func (AppModule) RegisterGRPCGatewayRoutes(
	clientCtx client.Context,
	mux *runtime.ServeMux,
) {
	if err := types.RegisterQueryHandlerClient(
		context.Background(),
		mux,
		types.NewQueryClient(clientCtx),
	); err != nil {
		panic(err)
	}
}

// RegisterInterfaces registers the asset module's interface types.
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterServices registers the asset module's message and query services.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	types.RegisterMsgServer(registrar, keeper.NewMsgServerImpl(am.k))
	types.RegisterQueryServer(registrar, keeper.NewQueryServerImpl(am.k))

	return nil
}

// DefaultGenesis returns the asset module's default genesis state.
func (AppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis validates the asset module's genesis state.
func (AppModule) ValidateGenesis(
	cdc codec.JSONCodec,
	_ client.TxEncodingConfig,
	bz json.RawMessage,
) error {
	var data types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf(
			"failed to unmarshal %s genesis state: %w",
			types.ModuleName,
			err,
		)
	}

	return data.Validate()
}

// InitGenesis initialises the asset module without validator updates.
func (am AppModule) InitGenesis(
	ctx sdk.Context,
	cdc codec.JSONCodec,
	data json.RawMessage,
) {
	var genesisState types.GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)
	if err := am.k.InitGenesis(ctx, &genesisState); err != nil {
		panic(err)
	}
}

// ExportGenesis returns the asset module's exported genesis state.
func (am AppModule) ExportGenesis(
	ctx sdk.Context,
	cdc codec.JSONCodec,
) json.RawMessage {
	genesisState, err := am.k.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}

	return cdc.MustMarshalJSON(genesisState)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return consensusVersion }
