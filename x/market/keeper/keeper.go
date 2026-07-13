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

	"ark/x/market/types"
)

// Keeper of the market store
type Keeper struct {
	cdc           codec.BinaryCodec
	storeService  store.KVStoreService
	authority     string
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	oracleKeeper  types.OracleKeeper

	Schema       collections.Schema
	Params       collections.Item[types.Params]
	ArkPoolDelta collections.Item[math.LegacyDec]
}

// NewKeeper creates a new market Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
) *Keeper {
	// ensure market module account is set
	if addr := accountKeeper.GetModuleAddress(types.ModuleName); addr == nil {
		panic(fmt.Sprintf("the x/%s module account has not been set", types.ModuleName))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:           cdc,
		storeService:  storeService,
		authority:     authority,
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		oracleKeeper:  oracleKeeper,
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
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the x/market module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", "x/"+types.ModuleName)
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

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	poolRecoveryPeriod := math.NewIntFromUint64(params.PoolRecoveryPeriod)
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

// GetActiveDenoms returns all denoms that have active oracle exchange rates.
// Used for simulation
func (k Keeper) GetActiveDenoms(ctx context.Context) ([]string, error) {
	actives, err := k.oracleKeeper.GetActives(ctx)
	if err != nil {
		return nil, err
	}
	return actives, nil
}
