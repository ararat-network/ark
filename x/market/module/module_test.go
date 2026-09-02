package market_test

import (
	"encoding/json"
	"math/rand"
	"slices"
	"strings"
	"testing"

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
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	marketv1 "github.com/ararat-network/ark/api/ark/market/v1"
	marketmodule "github.com/ararat-network/ark/x/market/module"
	"github.com/ararat-network/ark/x/market/testutil"
	"github.com/ararat-network/ark/x/market/types"
)

func TestAppModuleStandalone(t *testing.T) {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_market")
	storeService := runtime.NewKVStoreService(key)
	transientStoreService := runtime.NewTransientStoreService(transientKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, transientKey)

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	treasuryKeeper := testutil.NewMockTreasuryKeeper(ctrl)
	assetKeeper := testutil.NewMockAssetKeeper(ctrl)

	// The constructor asserts the module account exists, so this has to answer
	// before ProvideModule rather than at genesis import.
	moduleAccount := authtypes.NewEmptyModuleAccount(types.ModuleName)
	accountKeeper.EXPECT().
		GetModuleAddress(types.ModuleName).
		Return(moduleAccount.GetAddress()).
		AnyTimes()

	outputs := marketmodule.ProvideModule(marketmodule.ModuleInputs{
		Cdc:                   cdc,
		StoreService:          storeService,
		TransientStoreService: transientStoreService,
		AccountKeeper:         accountKeeper,
		BankKeeper:            bankKeeper,
		OracleKeeper:          oracleKeeper,
		TreasuryKeeper:        treasuryKeeper,
		AssetKeeper:           assetKeeper,
	})
	require.NotNil(t, outputs.MarketKeeper)
	require.IsType(t, marketmodule.AppModule{}, outputs.Module)

	appModule := marketmodule.NewAppModule(outputs.MarketKeeper)
	require.Equal(t, types.ModuleName, appModule.Name())
	require.Equal(t, uint64(1), appModule.ConsensusVersion())

	defaultGenesis := appModule.DefaultGenesis(cdc)
	require.NoError(t, appModule.ValidateGenesis(cdc, nil, defaultGenesis))

	invalidGenesis := types.DefaultGenesisState()
	invalidGenesis.ConversionPolicy.PoolRecoveryPeriod = 0
	require.Error(
		t,
		appModule.ValidateGenesis(cdc, nil, cdc.MustMarshalJSON(invalidGenesis)),
	)
	require.Error(t, appModule.ValidateGenesis(cdc, nil, []byte("{")))

	grpcServer := grpc.NewServer()
	require.NoError(t, appModule.RegisterServices(grpcServer))
	serviceInfo := grpcServer.GetServiceInfo()
	require.Contains(t, serviceInfo, marketv1.Msg_ServiceDesc.ServiceName)
	require.Contains(t, serviceInfo, marketv1.Query_ServiceDesc.ServiceName)

	// The virtual pool prices conversion in the protocol reference unit, and
	// genesis import refuses a pool that disagrees with it. x/oracle imports
	// first on a real chain, so the reference is already in state by here.
	var defaults types.GenesisState
	cdc.MustUnmarshalJSON(defaultGenesis, &defaults)
	accountKeeper.EXPECT().
		GetModuleAccount(gomock.Any(), types.ModuleName).
		Return(moduleAccount).
		AnyTimes()
	oracleKeeper.EXPECT().
		GetReferenceDenom(gomock.Any()).
		Return(defaults.ConversionPolicy.BasePool.Denom, nil).
		AnyTimes()

	appModule.InitGenesis(testCtx.Ctx, cdc, defaultGenesis)
	exportedGenesis := appModule.ExportGenesis(testCtx.Ctx, cdc)
	var exported types.GenesisState
	cdc.MustUnmarshalJSON(exportedGenesis, &exported)
	require.NoError(t, exported.Validate())
	require.JSONEq(t, string(defaultGenesis), string(exportedGenesis))

	simulationState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       cdc,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  map[string]json.RawMessage{},
	}
	appModule.GenerateGenesisState(&simulationState)
	var simulated types.GenesisState
	cdc.MustUnmarshalJSON(simulationState.GenState[types.ModuleName], &simulated)
	require.NoError(t, simulated.Validate())

	storeDecoders := simtypes.StoreDecoderRegistry{}
	appModule.RegisterStoreDecoder(storeDecoders)
	require.Contains(t, storeDecoders, types.StoreKey)
}

func TestAutoCLIOptionsCoverMarketServices(t *testing.T) {
	options := marketmodule.NewAppModule(nil).AutoCLIOptions()
	require.NotNil(t, options)
	require.NotNil(t, options.Query)
	require.NotNil(t, options.Tx)
	require.Equal(t, marketv1.Query_ServiceDesc.ServiceName, options.Query.Service)
	require.Equal(t, marketv1.Msg_ServiceDesc.ServiceName, options.Tx.Service)

	// The expected sets are read off the service descriptors rather than
	// transcribed, so a new RPC fails this test until it is described here.
	require.Equal(
		t,
		serviceMethods(marketv1.Query_ServiceDesc),
		rpcMethods(options.Query.RpcCommandOptions),
	)
	require.Equal(
		t,
		serviceMethods(marketv1.Msg_ServiceDesc),
		rpcMethods(options.Tx.RpcCommandOptions),
	)

	// Market is the one module with three classes of sender, so the Committee
	// prefix alone does not split them: conversion is an ordinary transaction
	// anyone submits, and it must never acquire a GovProposal flag that would
	// route it through governance. Both non-governance sets are pinned by name.
	var committee, open []string
	for _, command := range options.Tx.RpcCommandOptions {
		if command.GovProposal {
			require.False(
				t,
				strings.HasPrefix(command.RpcMethod, "Committee"),
				command.RpcMethod,
			)

			continue
		}
		if strings.HasPrefix(command.RpcMethod, "Committee") {
			committee = append(committee, command.RpcMethod)

			continue
		}
		open = append(open, command.RpcMethod)
	}
	slices.Sort(committee)
	slices.Sort(open)
	require.Equal(t, []string{"CommitteeSetTobinTax", "CommitteeUpdatePolicy"}, committee)
	require.Equal(t, []string{"Settle", "Swap", "SwapSend"}, open)
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
