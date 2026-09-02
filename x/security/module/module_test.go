package security_test

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

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	securityv1 "github.com/ararat-network/ark/api/ark/security/v1"
	securitymodule "github.com/ararat-network/ark/x/security/module"
	"github.com/ararat-network/ark/x/security/testutil"
	"github.com/ararat-network/ark/x/security/types"
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
	upgradeKeeper := testutil.NewMockUpgradeKeeper(ctrl)

	outputs := securitymodule.ProvideModule(securitymodule.ModuleInputs{
		Cdc:              cdc,
		StoreService:     storeService,
		MsgServiceRouter: baseapp.NewMsgServiceRouter(),
		AccountKeeper:    accountKeeper,
		UpgradeKeeper:    upgradeKeeper,
	})
	require.NotNil(t, outputs.SecurityKeeper)
	require.IsType(t, securitymodule.AppModule{}, outputs.Module)

	appModule := securitymodule.NewAppModule(outputs.SecurityKeeper)
	require.Equal(t, types.ModuleName, appModule.Name())
	require.Equal(t, uint64(1), appModule.ConsensusVersion())

	defaultGenesis := appModule.DefaultGenesis(cdc)
	require.NoError(t, appModule.ValidateGenesis(cdc, nil, defaultGenesis))

	// An empty plan carrying a term is the stale record the module distrusts:
	// nothing names it, so nothing can ever clear it.
	invalidGenesis := types.DefaultGenesisState()
	invalidGenesis.CommitteePlan.Term = 1
	require.Error(
		t,
		appModule.ValidateGenesis(cdc, nil, cdc.MustMarshalJSON(invalidGenesis)),
	)
	require.Error(t, appModule.ValidateGenesis(cdc, nil, []byte("{")))

	grpcServer := grpc.NewServer()
	require.NoError(t, appModule.RegisterServices(grpcServer))
	serviceInfo := grpcServer.GetServiceInfo()
	require.Contains(t, serviceInfo, securityv1.Msg_ServiceDesc.ServiceName)
	require.Contains(t, serviceInfo, securityv1.Query_ServiceDesc.ServiceName)

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
	require.Empty(t, appModule.WeightedOperations(simulationState))
}

func TestAutoCLIOptionsCoverSecurityServices(t *testing.T) {
	options := securitymodule.NewAppModule(nil).AutoCLIOptions()
	require.NotNil(t, options)
	require.NotNil(t, options.Query)
	require.NotNil(t, options.Tx)
	require.Equal(t, securityv1.Query_ServiceDesc.ServiceName, options.Query.Service)
	require.Equal(t, securityv1.Msg_ServiceDesc.ServiceName, options.Tx.Service)

	// The expected sets are read off the service descriptors rather than
	// transcribed, so a new RPC fails this test until it is described here.
	require.Equal(
		t,
		serviceMethods(securityv1.Query_ServiceDesc),
		rpcMethods(options.Query.RpcCommandOptions),
	)
	require.Equal(
		t,
		serviceMethods(securityv1.Msg_ServiceDesc),
		rpcMethods(options.Tx.RpcCommandOptions),
	)

	// Governance appoints the committee and nothing else; every emergency
	// power is the committee's own. That asymmetry is the module's whole
	// point, so the committee set is pinned by name: a fourth emergency power
	// must be added here deliberately.
	var committee []string
	for _, command := range options.Tx.RpcCommandOptions {
		isCommittee := strings.HasPrefix(command.RpcMethod, "Committee")
		require.Equal(t, !isCommittee, command.GovProposal, command.RpcMethod)
		if isCommittee {
			committee = append(committee, command.RpcMethod)
		}
	}
	slices.Sort(committee)
	require.Equal(t, []string{
		"CommitteeCancelUpgrade",
		"CommitteePlanUpgrade",
		"CommitteeRecoverClient",
	}, committee)
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
