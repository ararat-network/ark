package disbursement

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "github.com/ararat-network/ark/api/ark/disbursement/module/v1"
	assetkeeper "github.com/ararat-network/ark/x/asset/keeper"
	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/types"
)

var _ depinject.OnePerModuleType = AppModule{}

// IsOnePerModuleType identifies this runtime module.
func (AppModule) IsOnePerModuleType() {}

func init() { appmodule.Register(&modulev1.Module{}, appmodule.Provide(ProvideModule)) }

// ModuleInputs supplies the native disbursement capabilities.
type ModuleInputs struct {
	depinject.In
	Cdc                codec.Codec
	StoreService       store.KVStoreService
	AccountKeeper      types.AccountKeeper
	WasmKeeper         types.WasmKeeper
	BankKeeper         types.BankKeeper
	DistributionKeeper types.DistributionKeeper
	StakingKeeper      types.StakingKeeper
	AssetKeeper        *assetkeeper.Keeper
}

// ModuleOutputs registers the keeper and module.
type ModuleOutputs struct {
	depinject.Out
	DisbursementKeeper *keeper.Keeper
	Module             appmodule.AppModule
}

// ProvideModule constructs the governance-controlled disbursement module.
func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(in.Cdc, in.StoreService, authtypes.NewModuleAddress(govtypes.ModuleName).String(), in.AccountKeeper, in.WasmKeeper, in.BankKeeper, in.DistributionKeeper, in.StakingKeeper, in.AssetKeeper.Assets)
	return ModuleOutputs{DisbursementKeeper: k, Module: NewAppModule(k)}
}
