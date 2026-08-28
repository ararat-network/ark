package security

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"

	modulev1 "github.com/ararat-network/ark/api/ark/security/module/v1"
	"github.com/ararat-network/ark/x/security/keeper"
	"github.com/ararat-network/ark/x/security/types"
)

var _ depinject.OnePerModuleType = AppModule{}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

func init() {
	appmodule.Register(
		&modulev1.Module{},
		appmodule.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	Cdc          codec.Codec
	StoreService store.KVStoreService

	// MsgServiceRouter is how a committee action reaches the module that owns
	// the power, the same input x/gov takes to execute a passed proposal.
	MsgServiceRouter baseapp.MessageRouter

	AccountKeeper types.AccountKeeper
	UpgradeKeeper *upgradekeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	SecurityKeeper *keeper.Keeper
	Module         appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		in.MsgServiceRouter,
		in.AccountKeeper,
		in.UpgradeKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{
		SecurityKeeper: k,
		Module:         m,
	}
}
