package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/market/types"
)

// Keeper of the market store
type Keeper struct {
	cdc                   codec.BinaryCodec
	storeService          store.KVStoreService
	transientStoreService store.TransientStoreService
	authority             string
	accountKeeper         types.AccountKeeper
	bankKeeper            types.BankKeeper
	oracleKeeper          types.OracleKeeper
	treasuryKeeper        types.TreasuryKeeper
	assetKeeper           types.AssetKeeper

	Schema            collections.Schema
	Params            collections.Item[types.Params]
	ArkPoolDelta      collections.Item[math.LegacyDec]
	TobinTaxOverrides collections.Map[string, math.LegacyDec]
	ConversionPolicy  collections.Item[types.ConversionPolicy]
	ConversionMandate collections.Item[types.ConversionMandate]
}

// NewKeeper creates a new market Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	transientStoreService store.TransientStoreService,
	authority string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
	treasuryKeeper types.TreasuryKeeper,
	assetKeeper types.AssetKeeper,
) *Keeper {
	// ensure market module account is set
	if addr := accountKeeper.GetModuleAddress(types.ModuleName); addr == nil {
		panic(fmt.Sprintf("the x/%s module account has not been set", types.ModuleName))
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
		treasuryKeeper:        treasuryKeeper,
		assetKeeper:           assetKeeper,
		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		ArkPoolDelta: collections.NewItem(
			sb,
			types.ArkPoolDeltaKey,
			"ark_pool_delta",
			sdk.LegacyDecValue,
		),
		TobinTaxOverrides: collections.NewMap(
			sb,
			types.TobinTaxOverridesKey,
			"tobin_tax_overrides",
			collections.StringKey,
			sdk.LegacyDecValue,
		),
		ConversionPolicy: collections.NewItem(
			sb,
			types.ConversionPolicyKey,
			"capacity_policy",
			codec.CollValue[types.ConversionPolicy](cdc),
		),
		ConversionMandate: collections.NewItem(
			sb,
			types.ConversionMandateKey,
			"capacity_mandate",
			codec.CollValue[types.ConversionMandate](cdc),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// ReplenishPools replenishes each pool(Ark,Noah) to BasePool
func (k Keeper) ReplenishPools(ctx context.Context) error {
	poolDelta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return err
	}
	if poolDelta.IsZero() {
		return nil
	}

	capacity, err := k.ConversionPolicy.Get(ctx)
	if err != nil {
		return err
	}
	poolRecoveryPeriod := math.NewIntFromUint64(capacity.PoolRecoveryPeriod)
	poolRegressionAmt := poolDelta.QuoInt(poolRecoveryPeriod)
	if poolRegressionAmt.IsZero() {
		return nil
	}

	// Replenish pools towards each base pool
	// regressionAmt cannot make delta zero
	poolDelta = poolDelta.Sub(poolRegressionAmt)

	if err := k.ArkPoolDelta.Set(ctx, poolDelta); err != nil {
		return err
	}
	return nil
}

// GetActiveDenoms returns the convertible denominations for market simulation.
// It reads the asset registry rather than the rate store: a rate exists for
// every active feed, including feeds whose asset is suspended or not yet
// listed, and simulation should only offer swaps a real trader could make.
func (k Keeper) GetActiveDenoms(ctx context.Context) ([]string, error) {
	return k.assetKeeper.OraclePricedDenoms(ctx)
}
