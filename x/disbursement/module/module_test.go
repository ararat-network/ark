package disbursement_test

import (
	"encoding/json"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	disbursementv1 "github.com/ararat-network/ark/api/ark/disbursement/v1"
	"github.com/ararat-network/ark/pkg/chain"
	disbursementmodule "github.com/ararat-network/ark/x/disbursement/module"
	"github.com/ararat-network/ark/x/disbursement/testutil"
	"github.com/ararat-network/ark/x/disbursement/types"
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

	// The constructor resolves both custody addresses for the send
	// restriction, so these have to answer before ProvideModule.
	accounts := map[string]*authtypes.ModuleAccount{}
	for _, name := range types.PoolAccountNames() {
		accounts[name] = authtypes.NewEmptyModuleAccount(name)
		accountKeeper.EXPECT().
			GetModuleAddress(name).
			Return(accounts[name].GetAddress()).
			AnyTimes()
	}

	outputs := disbursementmodule.ProvideModule(disbursementmodule.ModuleInputs{
		Cdc:           cdc,
		StoreService:  storeService,
		AccountKeeper: accountKeeper,
		BankKeeper:    bankKeeper,
	})
	require.NotNil(t, outputs.DisbursementKeeper)
	require.IsType(t, disbursementmodule.AppModule{}, outputs.Module)
	// Bank collects send restrictions into a module-keyed map and asserts its
	// length against RestrictionsOrder, so a module that stops providing one
	// fails app construction rather than silently unguarding its accounts.
	require.NotNil(t, outputs.SendRestriction)

	appModule := disbursementmodule.NewAppModule(outputs.DisbursementKeeper)
	require.Equal(t, types.ModuleName, appModule.Name())
	require.Equal(t, uint64(1), appModule.ConsensusVersion())

	defaultGenesis := appModule.DefaultGenesis(cdc)
	require.NoError(t, appModule.ValidateGenesis(cdc, nil, defaultGenesis))

	invalidGenesis := types.DefaultGenesisState()
	invalidGenesis.Params.MemberAmount = math.ZeroInt()
	require.Error(
		t,
		appModule.ValidateGenesis(cdc, nil, cdc.MustMarshalJSON(invalidGenesis)),
	)
	require.Error(t, appModule.ValidateGenesis(cdc, nil, []byte("{")))

	grpcServer := grpc.NewServer()
	require.NoError(t, appModule.RegisterServices(grpcServer))
	serviceInfo := grpcServer.GetServiceInfo()
	require.Contains(t, serviceInfo, disbursementv1.Msg_ServiceDesc.ServiceName)
	require.Contains(t, serviceInfo, disbursementv1.Query_ServiceDesc.ServiceName)

	// Genesis import holds each custody account to its admission rule and
	// backs every reservation and tranche with its Bank balance.
	for name, account := range accounts {
		accountKeeper.EXPECT().
			GetModuleAccount(gomock.Any(), name).
			Return(account).
			AnyTimes()
		bankKeeper.EXPECT().
			GetAllBalances(gomock.Any(), account.GetAddress()).
			Return(sdk.NewCoins()).
			AnyTimes()
		bankKeeper.EXPECT().
			GetBalance(gomock.Any(), account.GetAddress(), chain.NoahBaseDenom).
			Return(chain.NoahCoin(math.ZeroInt())).
			AnyTimes()
	}

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

func TestAutoCLIOptionsCoverDisbursementServices(t *testing.T) {
	options := disbursementmodule.NewAppModule(nil).AutoCLIOptions()
	require.NotNil(t, options)
	require.NotNil(t, options.Query)
	require.NotNil(t, options.Tx)
	require.Equal(t, disbursementv1.Query_ServiceDesc.ServiceName, options.Query.Service)
	require.Equal(t, disbursementv1.Msg_ServiceDesc.ServiceName, options.Tx.Service)

	// The expected sets are read off the service descriptors rather than
	// transcribed, so a new RPC fails this test until it is described here.
	require.Equal(
		t,
		serviceMethods(disbursementv1.Query_ServiceDesc),
		rpcMethods(options.Query.RpcCommandOptions),
	)
	require.Equal(
		t,
		serviceMethods(disbursementv1.Msg_ServiceDesc),
		rpcMethods(options.Tx.RpcCommandOptions),
	)

	// The committee, the registrar, and a beneficiary's controller sign these
	// directly; everything else is a governance proposal. Pinning the split
	// stops a new directly signed message shipping unnoticed.
	signedCommands := map[string]bool{
		"CommitteeRegistrar":  true,
		"RegisterMembers":     true,
		"CommitteeSuspend":    true,
		"CommitteeReinstate":  true,
		"CommitteeCompensate": true,
		"SetPayee":            true,
		"SetController":       true,
	}
	for _, command := range options.Tx.RpcCommandOptions {
		require.Equal(
			t,
			!signedCommands[command.RpcMethod],
			command.GovProposal,
			command.RpcMethod,
		)
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
