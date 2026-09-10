package reserve

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "github.com/ararat-network/ark/api/ark/reserve/module/v1"
	"github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/types"
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

	AccountKeeper types.AccountKeeper
	WasmKeeper    types.WasmKeeper
	BankKeeper    types.BankKeeper
	OracleKeeper  types.OracleKeeper
	AssetKeeper   types.AssetKeeper
}

type ModuleOutputs struct {
	depinject.Out

	ReserveKeeper *keeper.Keeper
	Module        appmodule.AppModule
	// SendRestriction guards the strategic Reserve account. Bank collects these
	// into a module-keyed map, so every module providing one must also be named
	// in bank's RestrictionsOrder or app construction fails on a length
	// mismatch.
	SendRestriction banktypes.SendRestrictionFn
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		in.AccountKeeper,
		in.WasmKeeper,
		in.BankKeeper,
		in.OracleKeeper,
		in.AssetKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{
		ReserveKeeper:   k,
		Module:          m,
		SendRestriction: k.SendRestriction,
	}
}
