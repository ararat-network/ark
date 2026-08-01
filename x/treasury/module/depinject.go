package treasury

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "ark/api/ark/treasury/module/v1"
	"ark/x/treasury/keeper"
	"ark/x/treasury/types"
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

	Cdc                   codec.Codec
	StoreService          store.KVStoreService
	TransientStoreService store.TransientStoreService

	AccountKeeper types.AccountKeeper
	BankKeeper    types.BankKeeper
	OracleKeeper  types.OracleKeeper
	AssetKeeper   types.AssetKeeper
}

type ModuleOutputs struct {
	depinject.Out

	TreasuryKeeper  *keeper.Keeper
	Module          appmodule.AppModule
	SendRestriction banktypes.SendRestrictionFn
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.TransientStoreService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		in.AccountKeeper,
		in.BankKeeper,
		in.OracleKeeper,
		in.AssetKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{
		TreasuryKeeper:  k,
		Module:          m,
		SendRestriction: TreasurySendRestriction,
	}
}
