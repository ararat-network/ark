package oracle

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "ark/api/ark/oracle/module/v1"
	"ark/x/oracle/keeper"
	"ark/x/oracle/types"
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

	AccountKeeper      types.AccountKeeper
	BankKeeper         types.BankKeeper
	DistributionKeeper types.DistributionKeeper
	StakingKeeper      types.StakingKeeper
}

type ModuleOutputs struct {
	depinject.Out

	OracleKeeper *keeper.Keeper
	Module       appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
	}

	distrName := in.Config.DistributionName
	if distrName == "" {
		distrName = distrtypes.ModuleName
	}

	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authority.String(),
		distrName,
		in.AccountKeeper,
		in.BankKeeper,
		in.DistributionKeeper,
		in.StakingKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{OracleKeeper: k, Module: m}
}
