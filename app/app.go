package app

import (
	"fmt"
	"os"
	"sync"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	testdata_pulsar "github.com/cosmos/cosmos-sdk/testutil/testdata/testpb"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	protocolpoolkeeper "github.com/cosmos/cosmos-sdk/x/protocolpool/keeper"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"

	"ark/app/params"
	marketkeeper "ark/x/market/keeper"
	oraclekeeper "ark/x/oracle/keeper"
	treasurykeeper "ark/x/treasury/keeper"
)

const (
	// Name is the binary, environment variable, and home directory prefix.
	Name = "ark"
	// AppName is the BaseApp runtime name.
	AppName = "ArkApp"
	// AccountAddressPrefix is the prefix for accounts addresses.
	AccountAddressPrefix = "ark"
	// ChainCoinType is the coin type of the chain.
	ChainCoinType = params.CoinType
)

// DefaultNodeHome default home directories for the application daemon
var DefaultNodeHome string

var (
	_ runtime.AppI            = (*ArkApp)(nil)
	_ servertypes.Application = (*ArkApp)(nil)
)

// ArkApp extends an ABCI application, but with most of its parameters exported.
// They are exported for convenience in creating helper functions, as object
// capabilities aren't needed for testing.
type ArkApp struct {
	*runtime.App
	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry codectypes.InterfaceRegistry

	// essential keepers
	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.BaseKeeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             *govkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	ConsensusParamsKeeper consensuskeeper.Keeper
	MarketKeeper          *marketkeeper.Keeper
	TreasuryKeeper        *treasurykeeper.Keeper
	OracleKeeper          *oraclekeeper.Keeper

	// supplementary keepers
	FeeGrantKeeper     feegrantkeeper.Keeper
	AuthzKeeper        authzkeeper.Keeper
	ProtocolPoolKeeper protocolpoolkeeper.Keeper

	// simulation manager
	sm *module.SimulationManager

	// app-owned oracle ABCI and client lifecycle.
	oracleRuntime *oracleRuntime
	closeOnce     sync.Once
	closeErr      error
}

func init() {
	var err error
	clienthelpers.EnvPrefix = Name
	DefaultNodeHome, err = clienthelpers.GetNodeHomeDirectory("." + Name)
	if err != nil {
		panic(err)
	}
}

// NewArkApp returns a reference to an initialised ArkApp.
func NewArkApp(
	logger log.Logger,
	db dbm.DB,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *ArkApp {
	var (
		app        = &ArkApp{}
		appBuilder *runtime.AppBuilder

		// merge the AppConfig and other configuration in one config
		appConfig = depinject.Configs(
			AppConfig,
			depinject.Supply(
				appOpts, // supply the application options
				logger,  // supply the logger
			),
		)
	)

	if err := depinject.Inject(appConfig,
		&appBuilder,
		&app.appCodec,
		&app.legacyAmino,
		&app.txConfig,
		&app.interfaceRegistry,
		&app.AccountKeeper,
		&app.BankKeeper,
		&app.StakingKeeper,
		&app.SlashingKeeper,
		&app.DistrKeeper,
		&app.GovKeeper,
		&app.UpgradeKeeper,
		&app.AuthzKeeper,
		&app.FeeGrantKeeper,
		&app.EvidenceKeeper,
		&app.ConsensusParamsKeeper,
		&app.ProtocolPoolKeeper,
		&app.MarketKeeper,
		&app.TreasuryKeeper,
		&app.OracleKeeper,
	); err != nil {
		panic(err)
	}

	// Below we could construct and set an application specific mempool and
	// ABCI 1.0 PrepareProposal and ProcessProposal handlers. These defaults are
	// already set in the SDK's BaseApp, this shows an example of how to override
	// them.
	//
	// Example:
	//
	// app.App = appBuilder.Build(...)
	// nonceMempool := mempool.NewSenderNonceMempool()
	// abciPropHandler := NewDefaultProposalHandler(nonceMempool, app.App.BaseApp)
	//
	// app.App.BaseApp.SetMempool(nonceMempool)
	// app.App.BaseApp.SetPrepareProposal(abciPropHandler.PrepareProposalHandler())
	// app.App.BaseApp.SetProcessProposal(abciPropHandler.ProcessProposalHandler())
	//
	// Alternatively, you can construct BaseApp options, append those to
	// baseAppOptions and pass them to the appBuilder.
	//
	// Example:
	//
	// prepareOpt = func(app *baseapp.BaseApp) {
	// 	abciPropHandler := baseapp.NewDefaultProposalHandler(nonceMempool, app)
	// 	app.SetPrepareProposal(abciPropHandler.PrepareProposalHandler())
	// }
	// baseAppOptions = append(baseAppOptions, prepareOpt)

	baseAppOptions = append(baseAppOptions, baseapp.SetOptimisticExecution())

	app.App = appBuilder.Build(db, baseAppOptions...)
	oracleRuntime, err := newOracleRuntime(app, appOpts, logger)
	if err != nil {
		panic(err)
	}
	app.oracleRuntime = oracleRuntime

	// register streaming services
	if err := app.RegisterStreamingServices(appOpts, app.kvStoreKeys()); err != nil {
		panic(err)
	}

	/****  Module Options ****/

	// RegisterUpgradeHandlers is used for registering any on-chain upgrades.
	app.RegisterUpgradeHandlers()

	// add test gRPC service for testing gRPC queries in isolation
	testdata_pulsar.RegisterQueryServer(app.GRPCQueryRouter(), testdata_pulsar.QueryImpl{})

	// create the simulation manager and define the order of the modules for deterministic simulations
	//
	// NOTE: this is not required apps that don't use the simulator for fuzz testing
	// transactions
	overrideModules := map[string]module.AppModuleSimulation{
		authtypes.ModuleName: auth.NewAppModule(app.appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, nil),
	}
	app.sm = module.NewSimulationManagerFromAppModules(app.ModuleManager.Modules, overrideModules)

	app.sm.RegisterStoreDecoders()

	// A custom InitChainer can be set if extra pre-init-genesis logic is required.
	// By default, when using app wiring enabled module, this is not required.
	// For instance, the upgrade module will set automatically the module version map in its init genesis thanks to app wiring.
	// However, when registering a module manually (i.e. that does not support app wiring), the module version map
	// must be set manually as follow. The upgrade module will de-duplicate the module version map.
	//
	// app.SetInitChainer(func(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	// 	app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap())
	// 	return app.App.InitChainer(ctx, req)
	// })

	// set custom ante handler
	app.setAnteHandler(app.txConfig)

	if err := app.Load(loadLatest); err != nil {
		panic(err)
	}

	// At startup, after all modules have been registered, check that all prot
	// annotations are correct.
	protoFiles, err := proto.MergedRegistry()
	if err != nil {
		panic(err)
	}
	err = msgservice.ValidateProtoAnnotations(protoFiles)
	if err != nil {
		// Once we switch to using protoreflect-based antehandlers, we might
		// want to panic here instead of logging a warning.
		fmt.Fprintln(os.Stderr, err.Error())
	}

	app.oracleRuntime.start()

	return app
}

// setAnteHandler sets custom ante handlers.
// "x/auth/tx" pre-defined ante handler have been disabled in app_config.
func (app *ArkApp) setAnteHandler(txConfig client.TxConfig) {
	anteHandler, err := ante.NewAnteHandler(
		ante.HandlerOptions{
			AccountKeeper:   app.AccountKeeper,
			BankKeeper:      app.BankKeeper,
			SignModeHandler: txConfig.SignModeHandler(),
			FeegrantKeeper:  app.FeeGrantKeeper,
			SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
			TxFeeChecker:    app.treasuryFeeChecker,
		},
	)
	if err != nil {
		panic(err)
	}

	// Set the AnteHandler for the app
	app.SetAnteHandler(app.routeStabilityTax(anteHandler))
}

// LegacyAmino returns ArkApp's amino codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *ArkApp) LegacyAmino() *codec.LegacyAmino {
	return app.legacyAmino
}

// AppCodec returns ArkApp's app codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *ArkApp) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry returns ArkApp's InterfaceRegistry.
func (app *ArkApp) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

// TxConfig returns ArkApp's TxConfig
func (app *ArkApp) TxConfig() client.TxConfig {
	return app.txConfig
}

// GetKey returns the KVStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *ArkApp) GetKey(storeKey string) *storetypes.KVStoreKey {
	sk := app.UnsafeFindStoreKey(storeKey)
	kvStoreKey, ok := sk.(*storetypes.KVStoreKey)
	if !ok {
		return nil
	}
	return kvStoreKey
}

func (app *ArkApp) kvStoreKeys() map[string]*storetypes.KVStoreKey {
	keys := make(map[string]*storetypes.KVStoreKey)
	for _, k := range app.GetStoreKeys() {
		if kv, ok := k.(*storetypes.KVStoreKey); ok {
			keys[kv.Name()] = kv
		}
	}

	return keys
}

// SimulationManager implements the ArkApp interface
func (app *ArkApp) SimulationManager() *module.SimulationManager {
	return app.sm
}

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *ArkApp) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	app.App.RegisterAPIRoutes(apiSvr, apiConfig)
	// register swagger API in app.go so that other applications can override easily
	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

// GetMaccPerms returns a copy of the module account permissions
//
// NOTE: This is solely to be used for testing purposes.
func GetMaccPerms() map[string][]string {
	dup := make(map[string][]string)
	for _, perms := range moduleAccPerms {
		dup[perms.Account] = perms.Permissions
	}

	return dup
}

// BlockedAddresses returns all the app's blocked account addresses.
func BlockedAddresses() map[string]bool {
	result := make(map[string]bool)

	if len(blockAccAddrs) > 0 {
		for _, addr := range blockAccAddrs {
			result[addr] = true
		}
	} else {
		for addr := range GetMaccPerms() {
			result[addr] = true
		}
	}

	return result
}
