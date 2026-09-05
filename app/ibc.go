package app

import (
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	ibcwasm "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	gmp "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"
	ica "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts"
	icacontroller "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller"
	icacontrollerkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/keeper"
	icacontrollertypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/types"
	icahost "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host"
	icahostkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/keeper"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	ibccallbacks "github.com/cosmos/ibc-go/v11/modules/apps/callbacks"
	ibccallbacksv2 "github.com/cosmos/ibc-go/v11/modules/apps/callbacks/v2"
	packetforward "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware"
	packetforwardkeeper "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/keeper"
	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratelimiting "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting"
	ratelimitkeeper "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/keeper"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	ratelimitingv2 "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/v2"
	"github.com/cosmos/ibc-go/v11/modules/apps/transfer"
	transferkeeper "github.com/cosmos/ibc-go/v11/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	transferv2 "github.com/cosmos/ibc-go/v11/modules/apps/transfer/v2"
	ibc "github.com/cosmos/ibc-go/v11/modules/core"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcapi "github.com/cosmos/ibc-go/v11/modules/core/api"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v11/modules/core/keeper"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// IBCModuleBasics returns the stateless IBC modules needed by command and
// genesis construction, which run without constructing an ArkApp instance.
func IBCModuleBasics() module.BasicManager {
	return module.NewBasicManager(
		ibc.AppModuleBasic{},
		transfer.AppModuleBasic{},
		ratelimiting.AppModuleBasic{},
		packetforward.AppModuleBasic{},
		ica.AppModuleBasic{},
		ibctm.AppModuleBasic{},
		ibcwasm.AppModuleBasic{},
	)
}

// setupIBCKeepers registers the IBC stores and constructs the keepers. The
// routers are installed separately by setupIBCRoutes, because the Wasm keeper
// built between the two needs the channel keepers, while its contract IBC
// handlers must join the routers before they are sealed.
func (app *ArkApp) setupIBCKeepers() error {
	ibcKey := storetypes.NewKVStoreKey(ibcexported.StoreKey)
	transferKey := storetypes.NewKVStoreKey(ibctransfertypes.StoreKey)
	rateLimitKey := storetypes.NewKVStoreKey(ratelimittypes.StoreKey)
	packetForwardKey := storetypes.NewKVStoreKey(packetforwardtypes.StoreKey)
	icaControllerKey := storetypes.NewKVStoreKey(icacontrollertypes.StoreKey)
	icaHostKey := storetypes.NewKVStoreKey(icahosttypes.StoreKey)

	if err := app.RegisterStores(
		ibcKey,
		transferKey,
		rateLimitKey,
		packetForwardKey,
		icaControllerKey,
		icaHostKey,
	); err != nil {
		return fmt.Errorf("register IBC stores: %w", err)
	}

	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	app.IBCKeeper = ibckeeper.NewKeeper(
		app.appCodec,
		runtime.NewKVStoreService(ibcKey),
		app.UpgradeKeeper,
		authority,
	)
	app.TransferKeeper = transferkeeper.NewKeeper(
		app.appCodec,
		app.AccountKeeper.AddressCodec(),
		runtime.NewKVStoreService(transferKey),
		app.IBCKeeper.ChannelKeeper,
		app.MsgServiceRouter(),
		app.AccountKeeper,
		app.BankKeeper,
		authority,
	)
	app.RateLimitKeeper = ratelimitkeeper.NewKeeper(
		app.appCodec,
		app.AccountKeeper.AddressCodec(),
		runtime.NewKVStoreService(rateLimitKey),
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.ClientKeeper,
		app.BankKeeper,
		authority,
	)
	app.PacketForwardKeeper = packetforwardkeeper.NewKeeper(
		app.appCodec,
		app.AccountKeeper.AddressCodec(),
		runtime.NewKVStoreService(packetForwardKey),
		app.TransferKeeper,
		app.IBCKeeper.ChannelKeeper,
		app.BankKeeper,
		authority,
	)
	app.ICAControllerKeeper = icacontrollerkeeper.NewKeeper(
		app.appCodec,
		runtime.NewKVStoreService(icaControllerKey),
		app.IBCKeeper.ChannelKeeper,
		app.MsgServiceRouter(),
		authority,
	)
	app.ICAHostKeeper = icahostkeeper.NewKeeper(
		app.appCodec,
		runtime.NewKVStoreService(icaHostKey),
		app.IBCKeeper.ChannelKeeper,
		app.AccountKeeper,
		// The policy router, so an interchain account pays stability
		// tax as a contract does. The launch allowlist is empty, but a raw
		// router would let one governance vote on AllowMessages reopen
		// untaxed sends.
		app.executionPolicyRouter(),
		app.GRPCQueryRouter(),
		authority,
	)

	return nil
}

// setupIBCRoutes installs both port routers and registers the IBC modules. It
// runs after setupWasm, so the contract handlers are in hand when the routers
// are built.
func (app *ArkApp) setupIBCRoutes() error {
	// One handler serves three roles: the native contract channel route below,
	// and the contract keeper behind callbacks on both transfer stacks. Wasmd
	// supplies the callback authorisation and gas rules; Ark chooses only where
	// in each stack the middleware sits.
	//
	// The tax contract needs no wiring of its own here and holds either way.
	// Delivering a callback moves no funds, so the calculator extracts nothing
	// from it and it is not a taxable transfer; whatever the woken contract then
	// dispatches is an ordinary execution-generated message and is charged by
	// the policy router the Wasm keeper already holds (D42, D47).
	wasmIBCHandler := wasm.NewIBCHandler(
		app.WasmKeeper,
		app.IBCKeeper.ChannelKeeper,
		app.TransferKeeper,
		app.IBCKeeper.ChannelKeeper,
	)

	ibcRouter := porttypes.NewRouter()
	// Base is innermost and each Next wraps outward, so the inbound order is
	// rate limit, packet forward, callbacks, transfer. Callbacks sits directly
	// over transfer because a callback is a consequence of a delivered transfer:
	// it must not run for a packet the rate limiter or a forward hop rejected,
	// and a forwarded hop is a continuation rather than a delivery to this
	// chain, so it carries no callback of its own.
	transferStack := porttypes.NewIBCStackBuilder(app.IBCKeeper.ChannelKeeper)
	transferStack.Base(transfer.NewIBCModule(app.TransferKeeper)).
		Next(ibccallbacks.NewIBCMiddleware(wasmIBCHandler, wasm.DefaultMaxIBCCallbackGas)).
		Next(packetforward.NewIBCMiddleware(
			app.PacketForwardKeeper,
			0,
			packetforwardkeeper.DefaultForwardTransferPacketTimeoutTimestamp,
		)).
		Next(ratelimiting.NewIBCMiddleware(app.RateLimitKeeper))
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack.Build())
	ibcRouter.AddRoute(
		icacontrollertypes.SubModuleName,
		icacontroller.NewIBCMiddleware(app.ICAControllerKeeper),
	)
	ibcRouter.AddRoute(
		icahosttypes.SubModuleName,
		icahost.NewIBCModule(app.ICAHostKeeper),
	)
	// Native contract channels. This is the path for custom contract protocols;
	// ICS-20 transfer-and-call is the callbacks middleware above (D47).
	ibcRouter.AddRoute(wasmtypes.ModuleName, wasmIBCHandler)

	ibcRouterV2 := ibcapi.NewRouter()
	// The v2 stack has no packet forwarding to place, so the inbound order is
	// rate limit, callbacks, transfer — the same relative placement as Classic.
	transferStackV2 := ratelimitingv2.NewIBCMiddleware(
		*app.RateLimitKeeper,
		ibccallbacksv2.NewIBCMiddleware(
			transferv2.NewIBCModule(app.TransferKeeper),
			app.IBCKeeper.ChannelKeeperV2,
			wasmIBCHandler,
			app.IBCKeeper.ChannelKeeperV2,
			wasm.DefaultMaxIBCCallbackGas,
		),
		app.IBCKeeper.ChannelKeeperV2,
		app.IBCKeeper.ChannelKeeperV2,
	)
	ibcRouterV2.AddRoute(ibctransfertypes.PortID, transferStackV2)
	// Contract v2 ports are per-contract, so they route by prefix rather than
	// by an exact port ID.
	ibcRouterV2.AddPrefixRoute(wasmkeeper.PortIDPrefixV2, wasmkeeper.NewIBC2Handler(app.WasmKeeper))
	// Remote SDK-message execution through a chain-derived account (D48). It
	// carries no middleware: rate limiting and callbacks are properties of a
	// token transfer, and GMP moves no tokens of its own.
	ibcRouterV2.AddRoute(gmptypes.PortID, gmp.NewIBCModule(app.GMPKeeper))

	app.IBCKeeper.SetRouter(ibcRouter)
	app.IBCKeeper.SetRouterV2(ibcRouterV2)

	storeProvider := app.IBCKeeper.ClientKeeper.GetStoreProvider()
	tendermintLightClient := ibctm.NewLightClientModule(app.appCodec, storeProvider)
	app.IBCKeeper.ClientKeeper.AddRoute(ibctm.ModuleName, &tendermintLightClient)
	wasmLightClient := ibcwasm.NewLightClientModule(app.WasmClientKeeper, storeProvider)
	app.IBCKeeper.ClientKeeper.AddRoute(ibcwasmtypes.ModuleName, &wasmLightClient)

	if err := app.RegisterModules(
		ibc.NewAppModule(app.IBCKeeper),
		transfer.NewAppModule(app.TransferKeeper),
		ratelimiting.NewAppModule(app.RateLimitKeeper),
		packetforward.NewAppModule(app.PacketForwardKeeper),
		ica.NewAppModule(app.ICAControllerKeeper, app.ICAHostKeeper),
		ibctm.NewAppModule(tendermintLightClient),
	); err != nil {
		return fmt.Errorf("register IBC modules: %w", err)
	}

	return nil
}

// The three accessors below implement the IBC-Go testing application
// interface; the compile-time pin lives in ibc_test.go.

// GetBaseApp returns the embedded BaseApp.
func (app *ArkApp) GetBaseApp() *baseapp.BaseApp {
	return app.BaseApp
}

// GetIBCKeeper returns the IBC keeper.
func (app *ArkApp) GetIBCKeeper() *ibckeeper.Keeper {
	return app.IBCKeeper
}

// GetTxConfig returns the transaction encoding configuration.
func (app *ArkApp) GetTxConfig() client.TxConfig {
	return app.txConfig
}
