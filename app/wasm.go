package app

import (
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	ibcwasm "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11"
	ibcwasmkeeper "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/keeper"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cast"

	"cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/runtime"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
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

// setupWasm constructs the contract runtime after IBC keepers and before routes. It returns the
// node config and counter store for ante handling. Execution policy, the query accept list, and IBC
// client policy bound contract capabilities.
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

	// Contracts read through a hand-written accept list that is empty at
	// launch (app/wasm_query.go). No custom querier: the tax estimate D43
	// asks for is Treasury's ComputeTax query, listed there like any other
	// path (D74).
	wasmOpts := []wasmkeeper.Option{
		wasmkeeper.WithQueryPlugins(app.wasmQueryPlugins(acceptedQueries())),
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
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.ChannelKeeperV2,
		app.TransferKeeper,
		// Every message a contract dispatches clears the execution policy
		// gate — vote floor, MultiSend guard, transfer tax — on the way
		// through (D41, D42). Signed top-level messages take BaseApp's own
		// router and stay ante-owned.
		app.executionPolicyRouter(),
		app.GRPCQueryRouter(),
		cast.ToString(appOpts.Get(flags.FlagHome)),
		nodeConfig,
		wasmtypes.VMConfig{},
		wasmkeeper.BuiltInCapabilities(),
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

// setupWasmLightClient constructs the governance-controlled 08-wasm client keeper after IBC
// keepers. Its separate VM uses iterator capability and no custom queries; launch genesis
// contains no client code. See README.md for runtime boundaries.
func (app *ArkApp) setupWasmLightClient(appOpts servertypes.AppOptions) error {
	wasmClientKey := storetypes.NewKVStoreKey(ibcwasmtypes.StoreKey)
	if err := app.RegisterStores(wasmClientKey); err != nil {
		return fmt.Errorf("register 08-wasm store: %w", err)
	}

	// No BLS querier: ibc-go's blsverifier imports GPLv3 Prysm, and a GPLv3 node
	// conflicts with wasmvm's BUSL Singlepass compiler (THIRD_PARTY_NOTICES.md).
	app.WasmClientKeeper = ibcwasmkeeper.NewKeeperWithConfig(
		app.appCodec,
		runtime.NewKVStoreService(wasmClientKey),
		app.IBCKeeper.ClientKeeper,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		ibcwasmtypes.DefaultWasmConfig(cast.ToString(appOpts.Get(flags.FlagHome))),
		app.GRPCQueryRouter(),
	)

	if err := app.RegisterModules(ibcwasm.NewAppModule(app.WasmClientKeeper)); err != nil {
		return fmt.Errorf("register 08-wasm module: %w", err)
	}

	// Client bytecode lives outside the IAVL tree, as contract code does, so
	// state sync needs its own extension to carry it.
	if manager := app.SnapshotManager(); manager != nil {
		if err := manager.RegisterExtensions(
			ibcwasmkeeper.NewWasmSnapshotter(app.CommitMultiStore(), &app.WasmClientKeeper),
		); err != nil {
			return fmt.Errorf("register 08-wasm snapshot extension: %w", err)
		}
	}

	return nil
}
