package keeper

import (
	"context"
	"fmt"
	"noah/x/market/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper of the market store
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService
	authority    string

	AccountKeeper types.AccountKeeper
	BankKeeper    types.BankKeeper
	OracleKeeper  types.OracleKeeper

	Schema        collections.Schema
	Params        collections.Item[types.Params]
	NoahPoolDelta collections.Item[math.LegacyDec]
}

// NewKeeper creates a new market Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
	authority string,
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
		AccountKeeper: accountKeeper,
		BankKeeper:    bankKeeper,
		OracleKeeper:  oracleKeeper,
		Params:        collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		NoahPoolDelta: collections.NewItem(sb, types.NoahPoolDeltaKey, "noah_pool_delta", sdk.LegacyDecValue),
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

// ReplenishPools replenishes each pool(Noah,Ark) to BasePool
func (k Keeper) ReplenishPools(ctx context.Context) error {
	poolDelta, err := k.NoahPoolDelta.Get(ctx)
	if err != nil {
		return err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	poolRecoveryPeriod := int64(params.PoolRecoveryPeriod)
	poolRegressionAmt := poolDelta.QuoInt64(poolRecoveryPeriod)

	// Replenish pools towards each base pool
	// regressionAmt cannot make delta zero
	poolDelta = poolDelta.Sub(poolRegressionAmt)

	if err := k.NoahPoolDelta.Set(ctx, poolDelta); err != nil {
		return err
	}
	return nil
}
