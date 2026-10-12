package app

import (
	"fmt"
	"slices"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cast"

	"cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/runtime"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// WasmModuleBasics supplies stateless defaults for CLI and genesis construction without an ArkApp.
// Wasmd defaults permit public upload and instantiation; reviewed launch policy lives in
// genesis/genesis.json.
func WasmModuleBasics() module.BasicManager {
	return module.NewBasicManager(wasm.AppModuleBasic{})
}

// WasmVMCacheMetricsRegistererOpt is the app option under which `arkd start`
// hands the metrics endpoint's registry to the contract runtime, so wasmvm's
// cache counters export beside the node's own meters. Set in memory by start,
// never read from a file; absent, the counters are not exported.
const WasmVMCacheMetricsRegistererOpt = "ark.wasm.vm-cache-metrics-registerer"

// setupWasm constructs the contract runtime. It returns the node config and counter store for ante
// handling. Execution policy and the query accept list bound contract capabilities; the chain has
// no IBC, so a contract's IBC messages and queries are refused and its IBC entry points are
// unreachable.
func (app *ArkApp) setupWasm(appOpts servertypes.AppOptions) (wasmtypes.NodeConfig, store.KVStoreService, error) {
	wasmKey := storetypes.NewKVStoreKey(wasmtypes.StoreKey)
	if err := app.RegisterStores(wasmKey); err != nil {
		return wasmtypes.NodeConfig{}, nil, fmt.Errorf("register Wasm store: %w", err)
	}
	wasmStoreService := runtime.NewKVStoreService(wasmKey)

	nodeConfig, err := wasm.ReadNodeConfig(appOpts)
	if err != nil {
		return wasmtypes.NodeConfig{}, nil, fmt.Errorf("read Wasm node config: %w", err)
	}

	// Contracts read through a hand-written accept list of query paths
	// (app/wasm_query.go). No custom querier: the tax estimate a contract
	// needs is Treasury's ComputeTax query, listed there like any other
	// path.
	wasmOpts := []wasmkeeper.Option{
		wasmkeeper.WithQueryPlugins(app.wasmQueryPlugins(acceptedQueries())),
		wasmkeeper.WithMessageHandler(app.wasmMessageHandler()),
		wasmkeeper.WithWasmEngineDecorator(func(engine wasmtypes.WasmEngine) wasmtypes.WasmEngine {
			app.wasmEngine = engine
			return engine
		}),
	}
	if registerer, ok := appOpts.Get(WasmVMCacheMetricsRegistererOpt).(prometheus.Registerer); ok {
		wasmOpts = append(wasmOpts, wasmkeeper.WithVMCacheMetrics(registerer))
	}

	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	app.WasmKeeper = wasmkeeper.NewKeeper(
		app.appCodec,
		wasmStoreService,
		app.AccountKeeper,
		app.BankKeeper,
		app.StakingKeeper,
		// Contracts read rewards through the query surface, not the keeper.
		distrkeeper.NewQuerier(app.DistrKeeper),
		// No IBC: the default handler, encoders, and querier built over these
		// are replaced by wasmMessageHandler and wasmQueryPlugins, so nothing
		// dereferences them.
		nil,
		nil,
		nil,
		nil,
		// Every message a contract dispatches clears the execution policy
		// gate — vote floor, MultiSend guard, transfer tax — on the way
		// through. Signed top-level messages take BaseApp's own
		// router and stay ante-owned.
		app.executionPolicyRouter(),
		app.GRPCQueryRouter(),
		cast.ToString(appOpts.Get(flags.FlagHome)),
		nodeConfig,
		wasmtypes.VMConfig{},
		contractCapabilities(),
		authority,
		wasmOpts...,
	)

	if err := app.RegisterModules(wasm.NewAppModule(
		app.appCodec,
		&app.WasmKeeper,
		app.StakingKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		app.MsgServiceRouter(),
		// Ark has no legacy Wasm parameter subspace.
		nil,
	)); err != nil {
		return wasmtypes.NodeConfig{}, nil, fmt.Errorf("register Wasm module: %w", err)
	}

	// Contract code lives outside the IAVL tree, so state sync needs its own
	// extension to carry it. A node restored without this comes up with the
	// metadata for every contract and the bytecode for none.
	if manager := app.SnapshotManager(); manager != nil {
		if err := manager.RegisterExtensions(
			wasmkeeper.NewWasmSnapshotter(app.CommitMultiStore(), &app.WasmKeeper),
		); err != nil {
			return wasmtypes.NodeConfig{}, nil, fmt.Errorf("register Wasm snapshot extension: %w", err)
		}
	}

	return nodeConfig, wasmStoreService, nil
}

// contractCapabilities is wasmd's built-in set without ibc2, so a contract
// that requires IBC v2 entry points is refused at upload rather than
// instantiated against a chain that cannot open a channel.
func contractCapabilities() []string {
	capabilities := wasmkeeper.BuiltInCapabilities()
	return slices.DeleteFunc(capabilities, func(c string) bool { return c == "ibc2" })
}

// wasmMessageHandler is wasmd's default chain without its IBC packet handlers:
// SDK messages through the policy router, then burns. The IBC and IBC v2
// encoders refuse, so a contract's IBC message fails with that error rather
// than reaching a nil channel keeper.
func (app *ArkApp) wasmMessageHandler() wasmkeeper.Messenger {
	refuse := errorsmod.Wrap(wasmtypes.ErrUnsupportedForContract, "ibc not supported")
	encoders := wasmkeeper.DefaultEncoders(app.appCodec, nil).Merge(&wasmkeeper.MessageEncoders{
		IBC: func(sdk.Context, sdk.AccAddress, string, *wasmvmtypes.IBCMsg) ([]sdk.Msg, error) {
			return nil, refuse
		},
		IBC2: func(sdk.AccAddress, *wasmvmtypes.IBC2Msg) ([]sdk.Msg, error) {
			return nil, refuse
		},
	})
	return wasmkeeper.NewMessageHandlerChain(
		wasmkeeper.NewSDKMessageHandler(app.appCodec, app.executionPolicyRouter(), encoders),
		wasmkeeper.NewBurnCoinMessageHandler(app.BankKeeper),
	)
}
