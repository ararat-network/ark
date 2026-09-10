package treasury

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	modulev1 "github.com/ararat-network/ark/api/ark/treasury/module/v1"
	"github.com/ararat-network/ark/x/treasury/keeper"
	"github.com/ararat-network/ark/x/treasury/types"
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
	WasmKeeper    types.WasmKeeper
	BankKeeper    types.BankKeeper
	OracleKeeper  types.OracleKeeper
	AssetKeeper   types.AssetKeeper
	// ClaimsKeeper and ReserveKeeper report the two committee-operated funds'
	// recognised capital. Both satisfy the same method set, so depinject cannot
	// tell them apart by scanning provided types: app wiring binds each named
	// interface to its concrete keeper with depinject.BindInterface.
	ClaimsKeeper  types.ClaimsKeeper
	ReserveKeeper types.ReserveKeeper
}

type ModuleOutputs struct {
	depinject.Out

	TreasuryKeeper *keeper.Keeper
	Module         appmodule.AppModule
	// SendRestriction guards the Treasury custody accounts. Bank collects these
	// into a module-keyed map, so every module providing one must also be named
	// in bank's RestrictionsOrder or app construction fails on a length
	// mismatch.
	SendRestriction banktypes.SendRestrictionFn
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.TransientStoreService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		in.AccountKeeper,
		in.WasmKeeper,
		in.BankKeeper,
		in.OracleKeeper,
		in.AssetKeeper,
		in.ClaimsKeeper,
		in.ReserveKeeper,
	)

	m := NewAppModule(k)

	return ModuleOutputs{
		TreasuryKeeper: k,
		Module:         m,
		// The restriction is a keeper method because the guarded addresses are
		// the ones the constructor resolved and asserted.
		SendRestriction: k.SendRestriction,
	}
}
