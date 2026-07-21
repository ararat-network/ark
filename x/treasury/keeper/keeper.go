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

	"ark/x/treasury/types"
)

// Keeper owns Treasury policy and claims state. Fund custody remains in Bank.
type Keeper struct {
	cdc                   codec.BinaryCodec
	storeService          store.KVStoreService
	transientStoreService store.TransientStoreService
	authority             string

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	oracleKeeper  types.OracleKeeper

	Schema              collections.Schema
	Params              collections.Item[types.Params]
	TaxCaps             collections.Map[string, math.Int]
	ClaimsMandate       collections.Item[types.ClaimsMandate]
	ClaimsAllowanceUsed collections.Item[math.Int]
	InsuranceReserved   collections.Item[math.Int]
	NextClaimID         collections.Sequence
	Claims              collections.Map[uint64, types.Claim]
	RewardFunding       collections.Item[types.RewardFundingState]
	MonetaryMandate     collections.Item[types.MonetaryMandate]
	MonetaryPolicy      collections.Item[types.MonetaryPolicy]
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
		ClaimsMandate: collections.NewItem(
			sb,
			types.ClaimsMandateKey,
			"claims_mandate",
			codec.CollValue[types.ClaimsMandate](cdc),
		),
		ClaimsAllowanceUsed: collections.NewItem(
			sb,
			types.ClaimsAllowanceUsedKey,
			"claims_allowance_used",
			sdk.IntValue,
		),
		InsuranceReserved: collections.NewItem(
			sb,
			types.InsuranceReservedKey,
			"insurance_reserved",
			sdk.IntValue,
		),
		NextClaimID: collections.NewSequence(
			sb,
			types.NextClaimIDKey,
			"next_claim_id",
		),
		Claims: collections.NewMap(
			sb,
			types.ClaimsKey,
			"claims",
			collections.Uint64Key,
			codec.CollValue[types.Claim](cdc),
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
