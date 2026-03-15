package market

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "noah/api/noah/market/module/v1"
	"noah/x/market/keeper"
	"noah/x/market/types"
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

	Config       *modulev1.Module
	Cdc          codec.Codec
	StoreService store.KVStoreService

	AccountKeeper types.AccountKeeper
	BankKeeper    types.BankKeeper
	OracleKeeper  types.OracleKeeper
}

type ModuleOutputs struct {
	depinject.Out

	MarketKeeper *keeper.Keeper
	Module       appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
	}

	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authority.String(),
		in.AccountKeeper,
		in.BankKeeper,
		in.OracleKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{MarketKeeper: k, Module: m}
}
