package app

import (
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	ibcwasm "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11"
	"github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/blsverifier"
	ibcwasmkeeper "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/keeper"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	"github.com/spf13/cast"

	corestoretypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/runtime"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// WasmModuleBasics returns the stateless Wasm module needed by command and
// genesis construction, which run without constructing an ArkApp instance.
//
// The basics carry wasmd's own default genesis, which opens upload and
// instantiation to everybody. The launch posture — shut until the Phase 4
// activation matrix passes — lives in app/genesis/genesis.json, not in code
// defaults; a genesis generated from these defaults is a scratch genesis.
func WasmModuleBasics() module.BasicManager {
	return module.NewBasicManager(wasm.AppModuleBasic{})
}

// setupWasm registers the Wasm store and constructs the contract runtime. It
// runs between setupIBCKeepers and setupIBCRoutes: the keeper needs the channel
// keepers, and its contract IBC handlers must be in hand when the routers are
// built. It returns the node config and the Wasm store service for the ante
// chain: the store backs the per-block transaction count that makes
// an instantiated contract's address deterministic.
//
// The runtime ships inert. Ark's launch genesis permits nobody to upload or
// instantiate, and the execution policy router, the tax query, callbacks,
// and GMP are all still absent, so no contract path is reachable until the
// Phase 4 activation matrix passes.
func (app *ArkApp) setupWasm(appOpts servertypes.AppOptions) (wasmtypes.NodeConfig, corestoretypes.KVStoreService, error) {
	wasmKey := storetypes.NewKVStoreKey(wasmtypes.StoreKey)
	if err := app.RegisterStores(wasmKey); err != nil {
		return wasmtypes.NodeConfig{}, nil, fmt.Errorf("register Wasm store: %w", err)
	}
	wasmStoreService := runtime.NewKVStoreService(wasmKey)

	nodeConfig, err := wasm.ReadNodeConfig(appOpts)
	if err != nil {
		return wasmtypes.NodeConfig{}, nil, fmt.Errorf("read Wasm node config: %w", err)
	}

	// Contracts read through an accept list that is empty at launch, and write
	// through one advisory tax query. Both live in app/wasm_query.go.
	accepted, err := acceptedQueries(app.interfaceRegistry)
	if err != nil {
		return wasmtypes.NodeConfig{}, nil, fmt.Errorf("building the Wasm query accept list: %w", err)
	}
	encoders := wasmkeeper.DefaultEncoders(app.interfaceRegistry, app.TransferKeeper)

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
		// gate — vote floor, MultiSend guard, stability tax — on the way
		// through (D41, D42). Signed top-level messages take BaseApp's own
		// router and stay ante-owned.
		app.executionPolicyRouter(),
		app.GRPCQueryRouter(),
		cast.ToString(appOpts.Get(flags.FlagHome)),
		nodeConfig,
		wasmtypes.VMConfig{},
		wasmkeeper.BuiltInCapabilities(),
		authority,
		wasmkeeper.WithQueryHandlerDecorator(app.wasmQueryDecorator(encoders, accepted)),
	)

	if err := app.RegisterModules(wasm.NewAppModule(
		app.appCodec,
		&app.WasmKeeper,
		app.StakingKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		app.MsgServiceRouter(),
		// No legacy param subspace; Ark has never had one, as with x/auth.
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

// setupWasmLightClient registers the 08-wasm store and constructs the keeper
// that hosts Wasm IBC light clients, so a counterparty on non-CometBFT
// consensus can be verified without a binary upgrade. It runs after
// setupIBCKeepers, which built the client keeper it registers against; its
// light-client route joins the client router in setupIBCRoutes.
//
// It ships inert, like the contract runtime: the launch genesis carries no
// client code, uploading it is authority-gated to governance, and the only
// query surface beyond the module's stargate defaults is the BLS12-381
// verifier below, pure deterministic crypto with no state access. The VM
// is its own instance with the module's defaults — iterator capability only,
// data under <home>/ibc_08-wasm_client_data — because light-client code is
// consensus verification logic with a narrower contract than x/wasm's.
func (app *ArkApp) setupWasmLightClient(appOpts servertypes.AppOptions) error {
	wasmClientKey := storetypes.NewKVStoreKey(ibcwasmtypes.StoreKey)
	if err := app.RegisterStores(wasmClientKey); err != nil {
		return fmt.Errorf("register 08-wasm store: %w", err)
	}

	// BLS12-381 aggregate verification for Ethereum-consensus light clients
	// (Union). Merged over the defaults, so the stargate accept list stays at
	// the module's own (VerifyMembership only); widening it is a per-client
	// decision, not a default.
	wasmLightClientQueriers := ibcwasmkeeper.QueryPlugins{
		Custom: blsverifier.CustomQuerier(),
	}
	app.WasmClientKeeper = ibcwasmkeeper.NewKeeperWithConfig(
		app.appCodec,
		runtime.NewKVStoreService(wasmClientKey),
		app.IBCKeeper.ClientKeeper,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		ibcwasmtypes.DefaultWasmConfig(cast.ToString(appOpts.Get(flags.FlagHome))),
		app.GRPCQueryRouter(),
		ibcwasmkeeper.WithQueryPlugins(&wasmLightClientQueriers),
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
