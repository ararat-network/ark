package app

import (
	"fmt"

	gmp "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp"
	gmpkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/keeper"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// GMPModuleBasics returns the stateless GMP module needed by command and
// genesis construction, which run without constructing an ArkApp instance.
func GMPModuleBasics() module.BasicManager {
	return module.NewBasicManager(gmp.NewAppModuleBasic(gmp.AppModule{}))
}

// setupGMP registers the ICS-27 v2 general message passing store and keeper.
//
// GMP lets a remote caller execute SDK messages here through an account this
// chain derives for it. That account is an ordinary account with no standing
// authority: it may do exactly what its own balance and grants allow, and the
// keeper refuses any message whose signer is not that account before routing it.
//
// It runs after setupWasm and before setupIBCRoutes, because it takes the same
// Treasury-aware router the contract runtime does and its route joins the v2
// router the latter builds.
func (app *ArkApp) setupGMP() error {
	gmpKey := storetypes.NewKVStoreKey(gmptypes.StoreKey)
	if err := app.RegisterStores(gmpKey); err != nil {
		return fmt.Errorf("register GMP store: %w", err)
	}

	app.GMPKeeper = gmpkeeper.NewKeeper(
		app.appCodec,
		runtime.NewKVStoreService(gmpKey),
		app.AccountKeeper,
		// The same router contracts dispatch through, so a derived account pays
		// execution-generated tax on exactly the terms a contract does (D48).
		// The outer fee payer and any feegrant sponsor neither.
		app.treasuryMessageRouter(),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	)

	if err := app.RegisterModules(gmp.NewAppModule(app.GMPKeeper)); err != nil {
		return fmt.Errorf("register GMP module: %w", err)
	}

	return nil
}
