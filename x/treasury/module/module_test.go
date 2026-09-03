package treasury_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"slices"
	"strings"
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

	treasuryv1 "github.com/ararat-network/ark/api/ark/treasury/v1"
	treasurymodule "github.com/ararat-network/ark/x/treasury/module"
	"github.com/ararat-network/ark/x/treasury/testutil"
	"github.com/ararat-network/ark/x/treasury/types"
)

func TestAppModuleStandalone(t *testing.T) {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_treasury")
	storeService := runtime.NewKVStoreService(key)
	transientStoreService := runtime.NewTransientStoreService(transientKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, transientKey)

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	assetKeeper := testutil.NewMockAssetKeeper(ctrl)
	claimsKeeper := testutil.NewMockClaimsKeeper(ctrl)
	reserveKeeper := testutil.NewMockReserveKeeper(ctrl)

	// The constructor resolves every fund account plus the tax collector, the
	// strategic reserve, and the fee collector, and panics on any that is
	// unset. Deriving the address rather than listing the names keeps this
	// answering when a fund is added.
	accountKeeper.EXPECT().
		GetModuleAddress(gomock.Any()).
		DoAndReturn(authtypes.NewModuleAddress).
		AnyTimes()

	outputs := treasurymodule.ProvideModule(treasurymodule.ModuleInputs{
		Cdc:                   cdc,
		StoreService:          storeService,
		TransientStoreService: transientStoreService,
		AccountKeeper:         accountKeeper,
		BankKeeper:            bankKeeper,
		OracleKeeper:          oracleKeeper,
		AssetKeeper:           assetKeeper,
		ClaimsKeeper:          claimsKeeper,
		ReserveKeeper:         reserveKeeper,
	})
	require.NotNil(t, outputs.TreasuryKeeper)
	require.IsType(t, treasurymodule.AppModule{}, outputs.Module)
	// Bank collects send restrictions into a module-keyed map and asserts its
	// length against RestrictionsOrder, so a module that stops providing one
	// fails app construction rather than silently unguarding its funds.
	require.NotNil(t, outputs.SendRestriction)

	appModule := treasurymodule.NewAppModule(outputs.TreasuryKeeper)
	require.Equal(t, types.ModuleName, appModule.Name())
	require.Equal(t, uint64(1), appModule.ConsensusVersion())

	defaultGenesis := appModule.DefaultGenesis(cdc)
	require.NoError(t, appModule.ValidateGenesis(cdc, nil, defaultGenesis))

	invalidGenesis := types.DefaultGenesisState()
	invalidGenesis.ConversionFactors = append(
		invalidGenesis.ConversionFactors,
		types.ConversionFactor{Denom: "not a denom", Factor: math.LegacyOneDec()},
	)
	require.Error(
		t,
		appModule.ValidateGenesis(cdc, nil, cdc.MustMarshalJSON(invalidGenesis)),
	)
	require.Error(t, appModule.ValidateGenesis(cdc, nil, []byte("{")))

	grpcServer := grpc.NewServer()
	require.NoError(t, appModule.RegisterServices(grpcServer))
	serviceInfo := grpcServer.GetServiceInfo()
	require.Contains(t, serviceInfo, treasuryv1.Msg_ServiceDesc.ServiceName)
	require.Contains(t, serviceInfo, treasuryv1.Query_ServiceDesc.ServiceName)

	// Genesis import refuses a reference that disagrees with the configured
	// protocol reference, and walks every fund account's balance. x/oracle and
	// x/asset both import first on a real chain, so their state answers here.
	var defaults types.GenesisState
	cdc.MustUnmarshalJSON(defaultGenesis, &defaults)
	oracleKeeper.EXPECT().
		GetReferenceDenom(gomock.Any()).
		Return(defaults.Params.ReferenceDenom, nil).
		AnyTimes()
	assetKeeper.EXPECT().
		OraclePricedDenoms(gomock.Any()).
		Return([]string{}, nil).
		AnyTimes()
	assetKeeper.EXPECT().
		HasAsset(gomock.Any(), gomock.Any()).
		Return(true, nil).
		AnyTimes()
	accountKeeper.EXPECT().
		GetModuleAccount(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, name string) sdk.ModuleAccountI {
			return authtypes.NewEmptyModuleAccount(name)
		}).
		AnyTimes()
	bankKeeper.EXPECT().
		GetAllBalances(gomock.Any(), gomock.Any()).
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

func TestAutoCLIOptionsCoverTreasuryServices(t *testing.T) {
	options := treasurymodule.NewAppModule(nil).AutoCLIOptions()
	require.NotNil(t, options)
	require.NotNil(t, options.Query)
	require.NotNil(t, options.Tx)
	require.Equal(t, treasuryv1.Query_ServiceDesc.ServiceName, options.Query.Service)
	require.Equal(t, treasuryv1.Msg_ServiceDesc.ServiceName, options.Tx.Service)

	// The expected sets are read off the service descriptors rather than
	// transcribed, so a new RPC fails this test until it is described here.
	require.Equal(
		t,
		serviceMethods(treasuryv1.Query_ServiceDesc),
		rpcMethods(options.Query.RpcCommandOptions),
	)
	require.Equal(
		t,
		serviceMethods(treasuryv1.Msg_ServiceDesc),
		rpcMethods(options.Tx.RpcCommandOptions),
	)

	// The economic committee's one power is a policy move inside the corridor
	// governance appointed. Pinning the set as well as the Committee-prefix
	// rule is what stops a second committee power shipping unnoticed.
	var committee []string
	for _, command := range options.Tx.RpcCommandOptions {
		isCommittee := strings.HasPrefix(command.RpcMethod, "Committee")
		require.Equal(t, !isCommittee, command.GovProposal, command.RpcMethod)
		if isCommittee {
			committee = append(committee, command.RpcMethod)
		}
	}
	slices.Sort(committee)
	require.Equal(t, []string{"CommitteeUpdatePolicy"}, committee)
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
