package treasury

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "ark/api/ark/treasury/module/v1"
	oracletypes "ark/x/oracle/types"
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

	Config       *modulev1.Module
	Cdc          codec.Codec
	StoreService store.KVStoreService

	AccountKeeper      types.AccountKeeper
	BankKeeper         types.BankKeeper
	StakingKeeper      types.StakingKeeper
	DistributionKeeper types.ProtocolpoolKeeper
	OracleKeeper       types.OracleKeeper
}

type ModuleOutputs struct {
	depinject.Out

	TreasuryKeeper *keeper.Keeper
	Module         appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
	}

	rewardCollectorName := in.Config.RewardCollectorName
	if rewardCollectorName == "" {
		rewardCollectorName = oracletypes.ModuleName
	}

	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authority.String(),
		rewardCollectorName,
		in.AccountKeeper,
		in.BankKeeper,
		in.DistributionKeeper,
		in.OracleKeeper,
		in.StakingKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{TreasuryKeeper: k, Module: m}
}
