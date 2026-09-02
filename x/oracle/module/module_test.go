package oracle_test

import (
	"encoding/json"
	"math/rand"
	"slices"
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
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	modulev1 "github.com/ararat-network/ark/api/ark/oracle/module/v1"
	oraclev1 "github.com/ararat-network/ark/api/ark/oracle/v1"
	oraclemodule "github.com/ararat-network/ark/x/oracle/module"
	"github.com/ararat-network/ark/x/oracle/testutil"
	"github.com/ararat-network/ark/x/oracle/types"
)

func TestAppModuleStandalone(t *testing.T) {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(
		t,
		key,
		storetypes.NewTransientStoreKey("transient_test"),
	)
	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	distributionKeeper := testutil.NewMockDistributionKeeper(ctrl)
	stakingKeeper := testutil.NewMockStakingKeeper(ctrl)

	// The constructor asserts both its own account and the distribution
	// account the reward pool pays into, so these answer before ProvideModule.
	moduleAccount := authtypes.NewEmptyModuleAccount(types.ModuleName)
	accountKeeper.EXPECT().
		GetModuleAddress(gomock.Any()).
		DoAndReturn(authtypes.NewModuleAddress).
		AnyTimes()

	outputs := oraclemodule.ProvideModule(oraclemodule.ModuleInputs{
		Config:             &modulev1.Module{},
		Cdc:                cdc,
		StoreService:       storeService,
		AccountKeeper:      accountKeeper,
		BankKeeper:         bankKeeper,
		DistributionKeeper: distributionKeeper,
		StakingKeeper:      stakingKeeper,
	})
	require.NotNil(t, outputs.OracleKeeper)
	require.IsType(t, oraclemodule.AppModule{}, outputs.Module)

	// An unset DistributionName falls back to x/distribution's own name rather
	// than to the empty string, which would fail the constructor's assertion.
	named := oraclemodule.ProvideModule(oraclemodule.ModuleInputs{
		Config:             &modulev1.Module{DistributionName: distrtypes.ModuleName},
		Cdc:                cdc,
		StoreService:       storeService,
		AccountKeeper:      accountKeeper,
		BankKeeper:         bankKeeper,
		DistributionKeeper: distributionKeeper,
		StakingKeeper:      stakingKeeper,
	})
	require.NotNil(t, named.OracleKeeper)

	appModule := oraclemodule.NewAppModule(outputs.OracleKeeper)
	require.Equal(t, types.ModuleName, appModule.Name())
	require.Equal(t, uint64(1), appModule.ConsensusVersion())

	defaultGenesis := appModule.DefaultGenesis(cdc)
	require.NoError(t, appModule.ValidateGenesis(cdc, nil, defaultGenesis))

	invalidGenesis := types.DefaultGenesisState()
	invalidGenesis.Accounting.RewardWindow = 0
	require.Error(
		t,
		appModule.ValidateGenesis(cdc, nil, cdc.MustMarshalJSON(invalidGenesis)),
	)
	require.Error(t, appModule.ValidateGenesis(cdc, nil, []byte("{")))

	grpcServer := grpc.NewServer()
	require.NoError(t, appModule.RegisterServices(grpcServer))
	serviceInfo := grpcServer.GetServiceInfo()
	require.Contains(t, serviceInfo, oraclev1.Msg_ServiceDesc.ServiceName)
	require.Contains(t, serviceInfo, oraclev1.Query_ServiceDesc.ServiceName)

	accountKeeper.EXPECT().
		GetModuleAccount(gomock.Any(), types.ModuleName).
		Return(moduleAccount).
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

func TestAutoCLIOptionsCoverOracleServices(t *testing.T) {
	options := oraclemodule.NewAppModule(nil).AutoCLIOptions()
	require.NotNil(t, options)
	require.NotNil(t, options.Query)
	require.NotNil(t, options.Tx)
	require.Equal(t, oraclev1.Query_ServiceDesc.ServiceName, options.Query.Service)
	require.Equal(t, oraclev1.Msg_ServiceDesc.ServiceName, options.Tx.Service)

	// The expected sets are read off the service descriptors rather than
	// transcribed, so a new RPC fails this test until it is described here.
	require.Equal(
		t,
		serviceMethods(oraclev1.Query_ServiceDesc),
		rpcMethods(options.Query.RpcCommandOptions),
	)
	require.Equal(
		t,
		serviceMethods(oraclev1.Msg_ServiceDesc),
		rpcMethods(options.Tx.RpcCommandOptions),
	)

	// Oracle has no committee: the feed registry and the reference unit move
	// only by governance, and validator reports reach consensus as vote
	// extensions rather than as messages. Every command is a proposal, and one
	// arriving without the flag would be a directly submittable registry edit.
	for _, command := range options.Tx.RpcCommandOptions {
		require.True(t, command.GovProposal, command.RpcMethod)
	}
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
