package asset_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	assetv1 "github.com/ararat-network/ark/api/ark/asset/v1"
	assetmodule "github.com/ararat-network/ark/x/asset/module"
	"github.com/ararat-network/ark/x/asset/testutil"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestAppModuleStandalone(t *testing.T) {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	assettypes.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(assettypes.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(
		t,
		key,
		storetypes.NewTransientStoreKey("transient_test"),
	)
	ctrl := gomock.NewController(t)
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)

	outputs := assetmodule.ProvideModule(assetmodule.ModuleInputs{
		Cdc:          cdc,
		StoreService: storeService,
		BankKeeper:   bankKeeper,
		OracleKeeper: oracleKeeper,
	})
	require.NotNil(t, outputs.AssetKeeper)
	require.IsType(t, assetmodule.AppModule{}, outputs.Module)

	appModule := assetmodule.NewAppModule(outputs.AssetKeeper)
	require.Equal(t, assettypes.ModuleName, appModule.Name())
	require.Equal(t, uint64(1), appModule.ConsensusVersion())

	defaultGenesis := appModule.DefaultGenesis(cdc)
	require.NoError(t, appModule.ValidateGenesis(cdc, nil, defaultGenesis))

	invalidGenesis := assettypes.DefaultGenesisState()
	invalidGenesis.Assets[0].Version = 0
	require.Error(
		t,
		appModule.ValidateGenesis(
			cdc,
			nil,
			cdc.MustMarshalJSON(invalidGenesis),
		),
	)
	require.Error(
		t,
		appModule.ValidateGenesis(cdc, nil, []byte("{")),
	)

	grpcServer := grpc.NewServer()
	require.NoError(t, appModule.RegisterServices(grpcServer))
	serviceInfo := grpcServer.GetServiceInfo()
	require.Contains(t, serviceInfo, assetv1.Msg_ServiceDesc.ServiceName)
	require.Contains(t, serviceInfo, assetv1.Query_ServiceDesc.ServiceName)

	// Genesis import checks that every live asset's feed is usable, which is
	// x/oracle state reached through the expected keeper.
	oracleKeeper.EXPECT().
		FeedPhase(gomock.Any(), gomock.Any()).
		Return(oracletypes.FeedPhaseActive, nil).
		AnyTimes()

	var registeredMetadata []banktypes.Metadata
	bankKeeper.EXPECT().
		SetDenomMetaData(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, metadata banktypes.Metadata) {
			registeredMetadata = append(registeredMetadata, metadata)
		}).
		Times(len(assettypes.DefaultGenesisState().Assets))
	appModule.InitGenesis(
		testCtx.Ctx,
		cdc,
		defaultGenesis,
	)
	for i, asset := range assettypes.DefaultGenesisState().Assets {
		require.True(
			t,
			proto.Equal(&asset.Metadata, &registeredMetadata[i]),
		)
	}
	exportedGenesis := appModule.ExportGenesis(testCtx.Ctx, cdc)
	var exported assettypes.GenesisState
	cdc.MustUnmarshalJSON(exportedGenesis, &exported)
	require.NoError(t, exported.Validate())
	require.JSONEq(t, string(defaultGenesis), string(exportedGenesis))

	simulationState := module.SimulationState{
		Cdc:      cdc,
		GenState: map[string]json.RawMessage{},
	}
	appModule.GenerateGenesisState(&simulationState)
	var simulated assettypes.GenesisState
	cdc.MustUnmarshalJSON(
		simulationState.GenState[assettypes.ModuleName],
		&simulated,
	)
	require.NoError(t, simulated.Validate())

	storeDecoders := simtypes.StoreDecoderRegistry{}
	appModule.RegisterStoreDecoder(storeDecoders)
	require.Contains(t, storeDecoders, assettypes.StoreKey)
	require.Empty(t, appModule.WeightedOperations(simulationState))
}

func TestAutoCLIOptionsCoverAssetServices(t *testing.T) {
	options := assetmodule.NewAppModule(nil).AutoCLIOptions()
	require.NotNil(t, options)
	require.NotNil(t, options.Query)
	require.NotNil(t, options.Tx)
	require.Equal(
		t,
		assetv1.Query_ServiceDesc.ServiceName,
		options.Query.Service,
	)
	require.Equal(
		t,
		assetv1.Msg_ServiceDesc.ServiceName,
		options.Tx.Service,
	)

	// The expected sets are read off the service descriptors rather than
	// transcribed, so a new RPC fails this test until it is described here.
	// That is what makes the assertion below a real pin on the committee's
	// powers: a second committee-signed message cannot ship unnoticed.
	// Suspension is the only lifecycle mutation reaching consensus outside
	// x/gov's EndBlocker, so it is the only one that can land while a block is
	// still executing.
	require.Equal(
		t,
		serviceMethods(assetv1.Query_ServiceDesc),
		rpcMethods(options.Query.RpcCommandOptions),
	)
	require.Equal(
		t,
		serviceMethods(assetv1.Msg_ServiceDesc),
		rpcMethods(options.Tx.RpcCommandOptions),
	)
	// Committee messages are signed by the mandate committee, never submitted
	// as governance proposals. Suspension is the committee's only power.
	committeeCommands := map[string]bool{
		"EmergencySuspendAsset": true,
	}
	for _, command := range options.Tx.RpcCommandOptions {
		require.Equal(
			t,
			!committeeCommands[command.RpcMethod],
			command.GovProposal,
			command.RpcMethod,
		)
		require.NotEmpty(t, command.PositionalArgs, command.RpcMethod)
	}
	require.True(t, options.Tx.EnhanceCustomCommand)
}

// serviceMethods returns every RPC a generated service descriptor declares,
// sorted. Comparing against this rather than a transcribed list is what makes
// an undescribed RPC a test failure instead of a silent omission.
func serviceMethods(desc grpc.ServiceDesc) []string {
	methods := make([]string, len(desc.Methods))
	for i, method := range desc.Methods {
		methods[i] = method.MethodName
	}
	slices.Sort(methods)

	return methods
}

func rpcMethods(commands []*autocliv1.RpcCommandOptions) []string {
	methods := make([]string, len(commands))
	for i, command := range commands {
		methods[i] = command.RpcMethod
	}
	slices.Sort(methods)

	return methods
}
