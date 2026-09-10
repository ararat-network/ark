package asset

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "github.com/ararat-network/ark/api/ark/asset/module/v1"
	"github.com/ararat-network/ark/x/asset/keeper"
	"github.com/ararat-network/ark/x/asset/types"
)

var _ depinject.OnePerModuleType = AppModule{}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (AppModule) IsOnePerModuleType() {}

func init() {
	appmodule.Register(
		&modulev1.Module{},
		appmodule.Provide(ProvideModule),
	)
}

// ModuleInputs defines the dependencies required to construct x/asset.
type ModuleInputs struct {
	depinject.In

	Cdc          codec.Codec
	StoreService store.KVStoreService

	AccountKeeper types.AccountKeeper
	WasmKeeper    types.WasmKeeper
	BankKeeper    types.BankKeeper
	OracleKeeper  types.OracleKeeper
}

// ModuleOutputs defines the x/asset keeper and application module outputs.
type ModuleOutputs struct {
	depinject.Out

	AssetKeeper *keeper.Keeper
	Module      appmodule.AppModule
}

// ProvideModule constructs the x/asset keeper and application module.
func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		in.AccountKeeper,
		in.WasmKeeper,
		in.BankKeeper,
		in.OracleKeeper,
	)

	return ModuleOutputs{
		AssetKeeper: k,
		Module:      NewAppModule(k),
	}
}
