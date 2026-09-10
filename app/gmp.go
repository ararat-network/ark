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

// setupGMP constructs ICS-27 v2 general message passing after Wasm and before IBC routes. Derived
// accounts have only their balances and grants; the keeper authenticates their signers before the
// shared policy router.
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
		app.executionPolicyRouter(),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	)

	if err := app.RegisterModules(gmp.NewAppModule(app.GMPKeeper)); err != nil {
		return fmt.Errorf("register GMP module: %w", err)
	}

	return nil
}
