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

	chain "ark/pkg/chain"
	"ark/x/treasury/types"
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

	Schema          collections.Schema
	Params          collections.Item[types.Params]
	TaxCaps         collections.Map[string, math.Int]
	RewardFunding   collections.Item[types.RewardFundingState]
	MonetaryMandate collections.Item[types.MonetaryMandate]
	MonetaryPolicy  collections.Item[types.MonetaryPolicy]
	// TaxCapRefreshPending records that a complete cap derivation is owed: a
	// cadence boundary passed, or a pass left seeded or kept-through-outage
	// caps behind. Only a pass that derives every member lowers it, so an
	// incomplete refresh retries every block until rates return. Membership
	// drift needs no flag: the registry is ground truth, re-read every block.
	TaxCapRefreshPending collections.Item[bool]
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
	for _, moduleName := range types.FundAccountNames() {
		if addr := accountKeeper.GetModuleAddress(moduleName); addr == nil {
			panic(fmt.Sprintf("%s module account has not been set", moduleName))
		}
	}
	if addr := accountKeeper.GetModuleAddress(types.StabilityTaxCollectorName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.StabilityTaxCollectorName))
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
		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		TaxCaps: collections.NewMap(
			sb,
			types.TaxCapsKey,
			"tax_caps",
			collections.StringKey,
			sdk.IntValue,
		),
		RewardFunding: collections.NewItem(
			sb,
			types.RewardFundingKey,
			"reward_funding",
			codec.CollValue[types.RewardFundingState](cdc),
		),
		MonetaryMandate: collections.NewItem(
			sb,
			types.MonetaryMandateKey,
			"monetary_mandate",
			codec.CollValue[types.MonetaryMandate](cdc),
		),
		MonetaryPolicy: collections.NewItem(
			sb,
			types.MonetaryPolicyKey,
			"monetary_policy",
			codec.CollValue[types.MonetaryPolicy](cdc),
		),
		TaxCapRefreshPending: collections.NewItem(
			sb,
			types.TaxCapRefreshPendingKey,
			"tax_cap_refresh_pending",
			collections.BoolValue,
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

// balance reads a module account's NOAH holding. Treasury sizes funds and
// subsidies in NOAH throughout, so no caller needs the full coin set.
func (k Keeper) balance(ctx context.Context, moduleName string) math.Int {
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
