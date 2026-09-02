package reserve_test

import (
	"encoding/json"
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
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	reservev1 "github.com/ararat-network/ark/api/ark/reserve/v1"
	reservemodule "github.com/ararat-network/ark/x/reserve/module"
	"github.com/ararat-network/ark/x/reserve/testutil"
	"github.com/ararat-network/ark/x/reserve/types"
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
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	assetKeeper := testutil.NewMockAssetKeeper(ctrl)

	// The constructor resolves and asserts the custody address, so this has to
	// answer before ProvideModule rather than at genesis import.
	reserveAccount := authtypes.NewEmptyModuleAccount(types.StrategicReserveName)
	accountKeeper.EXPECT().
		GetModuleAddress(types.StrategicReserveName).
		Return(reserveAccount.GetAddress()).
		AnyTimes()

	outputs := reservemodule.ProvideModule(reservemodule.ModuleInputs{
		Cdc:           cdc,
		StoreService:  storeService,
		AccountKeeper: accountKeeper,
		BankKeeper:    bankKeeper,
		OracleKeeper:  oracleKeeper,
		AssetKeeper:   assetKeeper,
	})
	require.NotNil(t, outputs.ReserveKeeper)
	require.IsType(t, reservemodule.AppModule{}, outputs.Module)
	// Bank collects send restrictions into a module-keyed map and asserts its
	// length against RestrictionsOrder, so a module that stops providing one
	// fails app construction rather than silently unguarding its account.
	require.NotNil(t, outputs.SendRestriction)

	appModule := reservemodule.NewAppModule(outputs.ReserveKeeper)
	require.Equal(t, types.ModuleName, appModule.Name())
	require.Equal(t, uint64(1), appModule.ConsensusVersion())

	defaultGenesis := appModule.DefaultGenesis(cdc)
	require.NoError(t, appModule.ValidateGenesis(cdc, nil, defaultGenesis))

	invalidGenesis := types.DefaultGenesisState()
	invalidGenesis.NextPositionId = 0
	require.Error(
		t,
		appModule.ValidateGenesis(cdc, nil, cdc.MustMarshalJSON(invalidGenesis)),
	)
	require.Error(t, appModule.ValidateGenesis(cdc, nil, []byte("{")))

	grpcServer := grpc.NewServer()
	require.NoError(t, appModule.RegisterServices(grpcServer))
	serviceInfo := grpcServer.GetServiceInfo()
	require.Contains(t, serviceInfo, reservev1.Msg_ServiceDesc.ServiceName)
	require.Contains(t, serviceInfo, reservev1.Query_ServiceDesc.ServiceName)

	// Genesis import walks the custody balance and asks x/asset whether each
	// denomination is still a member, which is state reached through the
	// expected keepers.
	accountKeeper.EXPECT().
		GetModuleAccount(gomock.Any(), types.StrategicReserveName).
		Return(reserveAccount).
		AnyTimes()
	bankKeeper.EXPECT().
		GetAllBalances(gomock.Any(), reserveAccount.GetAddress()).
		Return(sdk.NewCoins()).
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
		GenState:  map[string]json.RawMessage{},
	}
	appModule.GenerateGenesisState(&simulationState)
	var simulated types.GenesisState
	cdc.MustUnmarshalJSON(simulationState.GenState[types.ModuleName], &simulated)
	require.NoError(t, simulated.Validate())

	storeDecoders := simtypes.StoreDecoderRegistry{}
	appModule.RegisterStoreDecoder(storeDecoders)
	require.Contains(t, storeDecoders, types.StoreKey)
	// Every reserve message is committee-gated, so random single-key signing
	// cannot hold the seat and the module registers no operations.
	require.Empty(t, appModule.WeightedOperations(simulationState))
}

func TestAutoCLIOptionsCoverReserveServices(t *testing.T) {
	options := reservemodule.NewAppModule(nil).AutoCLIOptions()
	require.NotNil(t, options)
	require.NotNil(t, options.Query)
	require.NotNil(t, options.Tx)
	require.Equal(t, reservev1.Query_ServiceDesc.ServiceName, options.Query.Service)
	require.Equal(t, reservev1.Msg_ServiceDesc.ServiceName, options.Tx.Service)

	// The expected sets are read off the service descriptors rather than
	// transcribed, so a new RPC fails this test until it is described here.
	require.Equal(
		t,
		serviceMethods(reservev1.Query_ServiceDesc),
		rpcMethods(options.Query.RpcCommandOptions),
	)
	require.Equal(
		t,
		serviceMethods(reservev1.Msg_ServiceDesc),
		rpcMethods(options.Tx.RpcCommandOptions),
	)

	// Committee messages are signed by the mandate committee, never submitted
	// as governance proposals, and the Committee prefix is what says which is
	// which. Pinning the set as well as the rule is what stops a new
	// committee power shipping unnoticed once it is described in AutoCLI.
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
		"CommitteeAttributeReturn",
		"CommitteeBurnPaper",
		"CommitteeBurnSurplus",
		"CommitteeClearImpairment",
		"CommitteeClosePosition",
		"CommitteeCorrectPosition",
		"CommitteeDeploy",
		"CommitteeFundBuffer",
		"CommitteeFundInsurance",
		"CommitteeMarkImpaired",
		"CommitteeRecordUpdate",
		"CommitteeReverseReturn",
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
