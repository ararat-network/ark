package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// Keeper owns Treasury policy state. Fund custody remains in Bank, and each
// committee-operated fund is owned by its own module.
type Keeper struct {
	cdc                   codec.BinaryCodec
	storeService          store.KVStoreService
	transientStoreService store.TransientStoreService
	authority             string

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	oracleKeeper  types.OracleKeeper
	assetKeeper   types.AssetKeeper
	claimsKeeper  types.ClaimsKeeper
	reserveKeeper types.ReserveKeeper

	// fundAddresses is the custody set SendRestriction guards, keyed by raw
	// address bytes. It is built from the same FundAccountNames() walk that
	// asserts registration, so a fund cannot be declared reachable and left
	// unguarded: an address the account keeper does not know is a panic here,
	// never a silently empty entry in a hand-written map.
	fundAddresses map[string]struct{}

	Schema collections.Schema
	Params collections.Item[types.Params]
	// ConversionFactors holds every derived cross rate from the protocol
	// reference — each member's and, once first derivable, NOAH's — refreshed
	// every block the oracle can serve it and kept at its last derived value
	// when it cannot. The tax base is every entry except NOAH's: GetTaxCap
	// owns that exclusion, and nothing stores a resolved cap. Membership
	// drift needs no flag: the registry is ground truth, re-read every block,
	// and every block is the retry.
	ConversionFactors collections.Map[string, types.ConversionFactor]
	RewardFunding     collections.Item[types.RewardFundingState]
	EconomicMandate   collections.Item[types.EconomicMandate]
	EconomicPolicy    collections.Item[types.EconomicPolicy]
	// ExposureState holds the risk estimate behind the fund-target multiplier:
	// two per-block series, the inputs of the last completed refresh, and the
	// multiplier itself. Sampling writes it every block from settlement;
	// application rewrites it on the governed period.
	ExposureState collections.Item[types.ExposureState]
	// ExposureRefreshPending records that a recomputation is owed because a
	// cadence boundary passed without one succeeding, on the same terms as
	// TaxCapRefreshPending: the boundary is an instant, so a period that
	// elapsed while liability could not be valued stays owed rather than being
	// forgiven, and every later block retries until it lands.
	ExposureRefreshPending collections.Item[bool]
	// BaseGasPrice is the base-fee controller's live price in reference base
	// units per gas unit: the fee gate reads it at ante, the EndBlock update
	// rewrites it from the block's gas tally.
	BaseGasPrice collections.Item[math.LegacyDec]
}

// NewKeeper creates a Treasury keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	transientStoreService store.TransientStoreService,
	authority string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
	assetKeeper types.AssetKeeper,
	claimsKeeper types.ClaimsKeeper,
	reserveKeeper types.ReserveKeeper,
) *Keeper {
	fundNames := types.FundAccountNames()
	fundAddresses := make(map[string]struct{}, len(fundNames))
	for _, moduleName := range fundNames {
		addr := accountKeeper.GetModuleAddress(moduleName)
		if addr == nil {
			panic(fmt.Sprintf("%s module account has not been set", moduleName))
		}
		fundAddresses[string(addr)] = struct{}{}
	}
	if addr := accountKeeper.GetModuleAddress(types.TransferTaxCollectorName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.TransferTaxCollectorName))
	}
	// Not a Treasury account, but the liability fold reads its balances
	// directly — the self-held netting is Treasury's own accounting (D66) — so
	// its registration is asserted where every other account this keeper reads
	// is asserted.
	if addr := accountKeeper.GetModuleAddress(reservetypes.StrategicReserveName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", reservetypes.StrategicReserveName))
	}
	// Nor is the fee collector, but the reward-funding window reads its balances
	// to value the fees validators already earned. A nil address there does not
	// fail: it reads as an empty coin set, the window accrues no fee value, and
	// settlement mints subsidy for a shortfall the fees had covered.
	if addr := accountKeeper.GetModuleAddress(authtypes.FeeCollectorName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", authtypes.FeeCollectorName))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:                   cdc,
		storeService:          storeService,
		transientStoreService: transientStoreService,
		authority:             authority,
		accountKeeper:         accountKeeper,
		bankKeeper:            bankKeeper,
		oracleKeeper:          oracleKeeper,
		assetKeeper:           assetKeeper,
		claimsKeeper:          claimsKeeper,
		reserveKeeper:         reserveKeeper,
		fundAddresses:         fundAddresses,
		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		ConversionFactors: collections.NewMap(
			sb,
			types.ConversionFactorsKey,
			"conversion_factors",
			collections.StringKey,
			codec.CollValue[types.ConversionFactor](cdc),
		),
		RewardFunding: collections.NewItem(
			sb,
			types.RewardFundingKey,
			"reward_funding",
			codec.CollValue[types.RewardFundingState](cdc),
		),
		EconomicMandate: collections.NewItem(
			sb,
			types.EconomicMandateKey,
			"economic_mandate",
			codec.CollValue[types.EconomicMandate](cdc),
		),
		ExposureState: collections.NewItem(
			sb,
			types.ExposureStateKey,
			"exposure_state",
			codec.CollValue[types.ExposureState](cdc),
		),
		ExposureRefreshPending: collections.NewItem(
			sb,
			types.ExposureRefreshPendingKey,
			"exposure_refresh_pending",
			collections.BoolValue,
		),
		EconomicPolicy: collections.NewItem(
			sb,
			types.EconomicPolicyKey,
			"economic_policy",
			codec.CollValue[types.EconomicPolicy](cdc),
		),
		BaseGasPrice: collections.NewItem(
			sb,
			types.BaseGasPriceKey,
			"base_gas_price",
			sdk.LegacyDecValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// getBalance reads a module account's NOAH holding. Treasury sizes funds and
// subsidies in NOAH throughout, so no caller needs the full coin set.
func (k Keeper) getBalance(ctx context.Context, moduleName string) math.Int {
	addr := k.accountKeeper.GetModuleAddress(moduleName)
	return k.bankKeeper.GetBalance(ctx, addr, chain.NoahBaseDenom).Amount
}

// shortfall returns how far actual falls below target, and zero once it does
// not. Every gap Treasury funds — fund capital, block rewards — is one-sided:
// an overshoot is not a negative requirement to be netted off elsewhere.
func shortfall(target, actual math.Int) math.Int {
	if target.LTE(actual) {
		return math.ZeroInt()
	}
	return target.Sub(actual)
}
