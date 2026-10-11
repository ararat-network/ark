package disbursement

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "github.com/ararat-network/ark/api/ark/disbursement/module/v1"
	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/types"
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

// ModuleInputs defines the dependencies required to construct x/disbursement.
type ModuleInputs struct {
	depinject.In

	Cdc          codec.Codec
	StoreService store.KVStoreService

	AccountKeeper      types.AccountKeeper
	WasmKeeper         types.WasmKeeper
	BankKeeper         types.BankKeeper
	DistributionKeeper types.DistributionKeeper
	MarketKeeper       types.MarketKeeper
	AssetKeeper        types.AssetKeeper
	OracleKeeper       types.OracleKeeper
}

// ModuleOutputs defines the x/disbursement keeper, application module, and send restriction.
type ModuleOutputs struct {
	depinject.Out

	DisbursementKeeper *keeper.Keeper
	Module             appmodule.AppModule
	// SendRestriction guards both custody accounts. Bank collects these into a module-keyed map, so
	// the module is also named in bank's RestrictionsOrder.
	SendRestriction banktypes.SendRestrictionFn
}

// ProvideModule constructs the x/disbursement keeper and application module.
func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		in.AccountKeeper,
		in.WasmKeeper,
		in.BankKeeper,
		in.DistributionKeeper,
		in.MarketKeeper,
		in.AssetKeeper,
		in.OracleKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{
		DisbursementKeeper: k,
		Module:             m,
		SendRestriction:    k.SendRestriction,
	}
}
