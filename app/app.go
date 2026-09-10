// Package app assembles the Ark chain's modules and keepers, installs
// transaction admission and consensus handlers, and wires IBC, GMP,
// and CosmWasm execution through the chain's execution policy.
//
// It also owns application genesis, continuation export, and upgrades.
// Its integration tests exercise behaviour across module boundaries.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"
	ibcwasmkeeper "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/keeper"
	gmpkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/keeper"
	icacontrollerkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/keeper"
	icahostkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/keeper"
	packetforwardkeeper "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/keeper"
	ratelimitkeeper "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/keeper"
	transferkeeper "github.com/cosmos/ibc-go/v11/modules/apps/transfer/keeper"
	ibckeeper "github.com/cosmos/ibc-go/v11/modules/core/keeper"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	clientv2helpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"

	"github.com/ararat-network/ark/app/ante"
	"github.com/ararat-network/ark/app/mempool"
	pricefeedclient "github.com/ararat-network/ark/pricefeed/client"
	assetkeeper "github.com/ararat-network/ark/x/asset/keeper"
	claimskeeper "github.com/ararat-network/ark/x/claims/keeper"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	reservekeeper "github.com/ararat-network/ark/x/reserve/keeper"
	securitykeeper "github.com/ararat-network/ark/x/security/keeper"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
)

const (
	// Name is the binary and home directory prefix; the environment variable
	// prefix is its uppercase.
	Name = "ark"
	// AppName is the BaseApp runtime name.
	AppName = "ArkApp"
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

	lanePool   *mempool.Pool
	privileges mempool.Set

	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry codectypes.InterfaceRegistry

	// keepers injected from AppConfig, in injection order
	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.BaseKeeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             *govkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	AuthzKeeper           authzkeeper.Keeper
	FeeGrantKeeper        feegrantkeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	ConsensusParamsKeeper consensuskeeper.Keeper
	MarketKeeper          *marketkeeper.Keeper
	TreasuryKeeper        *treasurykeeper.Keeper
	ClaimsKeeper          *claimskeeper.Keeper
	ReserveKeeper         *reservekeeper.Keeper
	OracleKeeper          *oraclekeeper.Keeper
	AssetKeeper           *assetkeeper.Keeper
	SecurityKeeper        *securitykeeper.Keeper

	// keepers wired by hand after Build (ibc-go and wasmd ship no depinject
	// modules), in setup order
	IBCKeeper           *ibckeeper.Keeper
	TransferKeeper      *transferkeeper.Keeper
	RateLimitKeeper     *ratelimitkeeper.Keeper
	PacketForwardKeeper *packetforwardkeeper.Keeper
	ICAControllerKeeper *icacontrollerkeeper.Keeper
	ICAHostKeeper       *icahostkeeper.Keeper
	WasmKeeper          wasmkeeper.Keeper
	WasmClientKeeper    ibcwasmkeeper.Keeper
	GMPKeeper           *gmpkeeper.Keeper

	// simulation manager
	sm *module.SimulationManager

	// app-owned price-feed client; the start command runs it.
	priceFeedClient *pricefeedclient.Client
	closeOnce       sync.Once
	closeErr        error
}

func init() {
	var err error
	clientv2helpers.EnvPrefix = strings.ToUpper(Name)
	DefaultNodeHome, err = clientv2helpers.GetNodeHomeDirectory("." + Name)
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
				// The appointing modules read the contract store through
				// this pointer. The contract runtime is built after depinject
				// (setupWasm), so the field is empty here and filled before
				// any appointment can run.
				&app.WasmKeeper,
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
		&app.MarketKeeper,
		&app.TreasuryKeeper,
		&app.ClaimsKeeper,
		&app.ReserveKeeper,
		&app.OracleKeeper,
		&app.AssetKeeper,
		&app.SecurityKeeper,
	); err != nil {
		panic(err)
	}

	// The feed-removal guard set is wiring-owned and holds exactly the foreign
	// consumers that exist: x/asset answers for a live asset's claim on its own
	// feed, and x/reserve for a credited eligibility entry or an open position
	// whose return attribution the feed's removal would strand. The
	// protocol reference is x/oracle's own state and needs no guard. A basket
	// guard joins here with its spec.
	app.OracleKeeper.SetFeedReferentGuards(app.AssetKeeper, app.ReserveKeeper)

	// Both reference-unit executors exist from this line on, so a reference
	// re-point executes atomically — Market's base pool and Treasury's tax cap
	// re-denominate inside the same MsgSetReferenceDenom transaction — instead of
	// failing atomically as it did while either executor was missing.
	app.OracleKeeper.SetReferenceDenomConsumers(app.MarketKeeper, app.TreasuryKeeper)

	// The capital contract runs both ways, and only this direction is wired by
	// hand: Treasury injects the Reserve to read recognised capital, so the
	// Reserve cannot inject Treasury to read the requirement it is measured
	// against. Only a surplus burn takes this read.
	app.ReserveKeeper.SetTreasuryCapitalReader(app.TreasuryKeeper)

	// No EnableBlockGasMeter: comet's max_gas gate and the per-tx meters already
	// bound a block, and the meter is mutually exclusive with block-stm execution.
	baseAppOptions = append(
		baseAppOptions,
		baseapp.SetOptimisticExecution(),
	)

	// Always install the lane pool so local sizing cannot change validation.
	poolConfig, err := mempoolConfig(appOpts)
	if err != nil {
		panic(err)
	}
	app.lanePool = mempool.NewPool(poolConfig)
	baseAppOptions = append(baseAppOptions, baseapp.SetMempool(app.lanePool))

	app.App = appBuilder.Build(db, baseAppOptions...)

	// Keepers, then the contract runtime, then the routes: Wasm needs the IBC
	// channel keepers, and the IBC routers need Wasm's contract handlers.
	if err := app.setupIBCKeepers(); err != nil {
		panic(err)
	}
	wasmNodeConfig, wasmTxCounterStore, err := app.setupWasm(appOpts)
	if err != nil {
		panic(err)
	}
	if err := app.setupGMP(); err != nil {
		panic(err)
	}
	if err := app.setupWasmLightClient(appOpts); err != nil {
		panic(err)
	}
	if err := app.setupIBCRoutes(); err != nil {
		panic(err)
	}

	// Install transaction validation and execution handlers.
	app.privileges = app.newPrivileges()
	app.SetAnteHandler(app.lanePool.WithReservations(ante.NewAnteHandler(
		app.appCodec,
		app.txConfig,
		app.AccountKeeper,
		app.BankKeeper,
		app.FeeGrantKeeper,
		app.StakingKeeper,
		app.TreasuryKeeper,
		app.privileges,
		app.IBCKeeper,
		app.WasmKeeper.GetGasRegister(),
		wasmNodeConfig,
		wasmTxCounterStore,
	)))

	app.SetPrepareCheckStater(app.lanePool.PrepareCheckState)
	app.SetPostHandler(ante.NewPostHandler(app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper))

	if err := app.setupOracleABCI(logger, appOpts); err != nil {
		panic(err)
	}

	// Seed the upgrade version map at InitChain so the first upgrade migrates
	// the manually registered modules rather than re-running their InitGenesis.
	// Must precede Load, which installs the default InitChainer when none is set.
	app.SetInitChainer(func(ctx sdk.Context, req *cmtabci.RequestInitChain) (*cmtabci.ResponseInitChain, error) {
		if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
			return nil, err
		}
		return app.InitChainer(ctx, req)
	})

	if err := app.setupUpgrades(); err != nil {
		panic(err)
	}

	// Streaming must follow the hand-wired RegisterStores calls above:
	// kvStoreKeys reads the runtime's key list, so registering earlier would
	// silently hide the IBC, Wasm, GMP, and 08-wasm stores from listeners.
	if err := app.RegisterStreamingServices(appOpts, app.kvStoreKeys()); err != nil {
		panic(err)
	}

	// Load runs the registered store loader and then seals the baseapp: every
	// baseapp registration above is now fixed, and none can follow.
	if err := app.Load(loadLatest); err != nil {
		panic(err)
	}
	// Re-warm the pinned-code caches over the loaded store: the pin lists live
	// in state, but both VM caches are per-process, so a restart that skips
	// this silently demotes every pinned code to the slow path.
	if loadLatest {
		ctx := app.NewContext(true)
		if err := app.WasmKeeper.InitializePinnedCodes(ctx); err != nil {
			panic(fmt.Errorf("initialise pinned Wasm codes: %w", err))
		}
		if err := app.WasmClientKeeper.InitializePinnedCodes(ctx); err != nil {
			panic(fmt.Errorf("initialise pinned 08-wasm codes: %w", err))
		}
	}

	// The simulation manager gathers every module that opts into simulation,
	// in sorted-name order so runs are seed-deterministic. It must be built
	// after the hand-wired RegisterModules calls above, or those modules'
	// store decoders silently drop out of simulation runs. Auth is overridden
	// because the depinject-built module carries no account generator, and the
	// simulator needs RandomGenesisAccounts to seed each run.
	overrideModules := map[string]module.AppModuleSimulation{
		authtypes.ModuleName: authSimModule{
			auth.NewAppModule(app.appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, nil),
		},
		banktypes.ModuleName: bankSimModule{
			AppModule: bank.NewAppModule(app.appCodec, app.BankKeeper, app.AccountKeeper, nil),
			cdc:       app.appCodec,
		},
	}
	app.sm = module.NewSimulationManagerFromAppModules(app.ModuleManager.Modules, overrideModules)
	app.sm.RegisterStoreDecoders()

	// At startup, after all modules have been registered, check that all proto
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

	return app
}

// RunPriceFeed runs the node-side price-feed client until ctx is cancelled. It
// is blocking and single-use; the start command owns the goroutine and the
// context, so only a running node carries the client. Cancellation is the
// clean exit and reports nil.
func (app *ArkApp) RunPriceFeed(ctx context.Context) error {
	err := app.priceFeedClient.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Close closes the embedded app. It is safe to call multiple times as
// required by servertypes.Application. The price-feed client needs no closing
// here: the start command's errgroup cancels it before app cleanup runs.
func (app *ArkApp) Close() error {
	app.closeOnce.Do(func() {
		app.closeErr = app.App.Close()
	})

	return app.closeErr
}

// GetKey returns the KVStoreKey for the provided store key.
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

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *ArkApp) RegisterAPIRoutes(apiSvr *api.Server, apiConfig serverconfig.APIConfig) {
	app.App.RegisterAPIRoutes(apiSvr, apiConfig)
	// register swagger API in app.go so that other applications can override easily
	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

// The accessors below exist for tests only; production code receives these
// objects through depinject.

// LegacyAmino returns the amino codec the test helpers hand to client contexts.
func (app *ArkApp) LegacyAmino() *codec.LegacyAmino {
	return app.legacyAmino
}

// AppCodec returns the application codec; it also serves the IBC-Go testing
// application interface next to the Get* accessors in ibc.go.
func (app *ArkApp) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry returns the codec's interface registry.
func (app *ArkApp) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

// TxConfig returns the transaction encoding configuration. The simulation
// harness asks for it under this name; the IBC-Go testing interface asks for
// the same object as GetTxConfig, in ibc.go.
func (app *ArkApp) TxConfig() client.TxConfig {
	return app.txConfig
}

// SimulationManager returns the manager the simulation harness drives.
func (app *ArkApp) SimulationManager() *module.SimulationManager {
	return app.sm
}
