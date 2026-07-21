package app

import (
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	ica "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts"
	icacontroller "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller"
	icacontrollerkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/keeper"
	icacontrollertypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/types"
	icahost "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host"
	icahostkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/keeper"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	packetforward "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware"
	packetforwardkeeper "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/keeper"
	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratelimiting "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting"
	ratelimitkeeper "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/keeper"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	ratelimitingv2 "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/v2"
	"github.com/cosmos/ibc-go/v11/modules/apps/transfer"
	transferkeeper "github.com/cosmos/ibc-go/v11/modules/apps/transfer/keeper"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	transferv2 "github.com/cosmos/ibc-go/v11/modules/apps/transfer/v2"
	ibc "github.com/cosmos/ibc-go/v11/modules/core"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcante "github.com/cosmos/ibc-go/v11/modules/core/ante"
	ibcapi "github.com/cosmos/ibc-go/v11/modules/core/api"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v11/modules/core/keeper"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
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
	)
}

func (app *ArkApp) setupIBC() error {
	ibcKey := storetypes.NewKVStoreKey(ibcexported.StoreKey)
	transferKey := storetypes.NewKVStoreKey(transfertypes.StoreKey)
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
		app.MsgServiceRouter(),
		app.GRPCQueryRouter(),
		authority,
	)

	ibcRouter := porttypes.NewRouter()
	transferStack := porttypes.NewIBCStackBuilder(app.IBCKeeper.ChannelKeeper)
	transferStack.Base(transfer.NewIBCModule(app.TransferKeeper)).
		Next(packetforward.NewIBCMiddleware(
			app.PacketForwardKeeper,
			0,
			packetforwardkeeper.DefaultForwardTransferPacketTimeoutTimestamp,
		)).
		Next(ratelimiting.NewIBCMiddleware(app.RateLimitKeeper))
	ibcRouter.AddRoute(transfertypes.ModuleName, transferStack.Build())
	ibcRouter.AddRoute(
		icacontrollertypes.SubModuleName,
		icacontroller.NewIBCMiddleware(app.ICAControllerKeeper),
	)
	ibcRouter.AddRoute(
		icahosttypes.SubModuleName,
		icahost.NewIBCModule(app.ICAHostKeeper),
	)

	ibcRouterV2 := ibcapi.NewRouter()
	transferStackV2 := ratelimitingv2.NewIBCMiddleware(
		*app.RateLimitKeeper,
		transferv2.NewIBCModule(app.TransferKeeper),
		app.IBCKeeper.ChannelKeeperV2,
		app.IBCKeeper.ChannelKeeperV2,
	)
	ibcRouterV2.AddRoute(transfertypes.PortID, transferStackV2)

	app.IBCKeeper.SetRouter(ibcRouter)
	app.IBCKeeper.SetRouterV2(ibcRouterV2)

	storeProvider := app.IBCKeeper.ClientKeeper.GetStoreProvider()
	tendermintLightClient := ibctm.NewLightClientModule(app.appCodec, storeProvider)
	app.IBCKeeper.ClientKeeper.AddRoute(ibctm.ModuleName, &tendermintLightClient)

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

	app.SetInitChainer(func(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
		if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
			return nil, err
		}
		return app.App.InitChainer(ctx, req)
	})

	return nil
}

func (app *ArkApp) withIBCAnte(next sdk.AnteHandler) sdk.AnteHandler {
	redundantRelay := sdk.ChainAnteDecorators(ibcante.NewRedundantRelayDecorator(app.IBCKeeper))
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		newCtx, err := next(ctx, tx, simulate)
		if err != nil {
			return newCtx, err
		}
		return redundantRelay(newCtx, tx, simulate)
	}
}

// GetBaseApp implements the IBC-Go testing application interface.
func (app *ArkApp) GetBaseApp() *baseapp.BaseApp {
	return app.BaseApp
}

// GetIBCKeeper implements the IBC-Go testing application interface.
func (app *ArkApp) GetIBCKeeper() *ibckeeper.Keeper {
	return app.IBCKeeper
}

// GetTxConfig implements the IBC-Go testing application interface.
func (app *ArkApp) GetTxConfig() client.TxConfig {
	return app.txConfig
}
