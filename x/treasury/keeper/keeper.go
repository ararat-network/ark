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

	core "noah/pkg/types"
	"noah/x/treasury/types"
)

// Keeper of the treasury store
type Keeper struct {
	cdc                 codec.BinaryCodec
	storeService        store.KVStoreService
	authority           string
	rewardCollectorName string

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	ppoolKeeper   types.ProtocolpoolKeeper
	marketKeeper  types.MarketKeeper
	oracleKeeper  types.OracleKeeper
	stakingKeeper types.StakingKeeper

	Schema               collections.Schema
	Params               collections.Item[types.Params]
	TaxRate              collections.Item[math.LegacyDec]
	RewardWeight         collections.Item[math.LegacyDec]
	TaxCaps              collections.Map[string, math.Int]
	EpochTaxProceeds     collections.Item[types.EpochTaxProceeds]
	EpochInitialIssuance collections.Item[types.EpochInitialIssuance]
	EpochStates          collections.Map[uint64, types.EpochState]
}

// NewKeeper creates a new treasury Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	rewardCollectorName string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	ppoolKeeper types.ProtocolpoolKeeper,
	marketKeeper types.MarketKeeper,
	oracleKeeper types.OracleKeeper,
	stakingKeeper types.StakingKeeper,
) *Keeper {
	// ensure treasury module account is set
	if addr := accountKeeper.GetModuleAddress(types.ModuleName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:                 cdc,
		storeService:        storeService,
		authority:           authority,
		rewardCollectorName: rewardCollectorName,
		accountKeeper:       accountKeeper,
		bankKeeper:          bankKeeper,
		ppoolKeeper:         ppoolKeeper,
		marketKeeper:        marketKeeper,
		oracleKeeper:        oracleKeeper,
		stakingKeeper:       stakingKeeper,
		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		TaxRate: collections.NewItem(
			sb,
			types.TaxRateKey,
			"tax_rate",
			sdk.LegacyDecValue,
		),
		RewardWeight: collections.NewItem(
			sb,
			types.RewardWeightKey,
			"reward_weight",
			sdk.LegacyDecValue,
		),
		TaxCaps: collections.NewMap(
			sb,
			types.TaxCapsKey,
			"tax_caps",
			collections.StringKey,
			sdk.IntValue,
		),
		EpochTaxProceeds: collections.NewItem(
			sb,
			types.EpochTaxProceedsKey,
			"epoch_tax_proceeds",
			codec.CollValue[types.EpochTaxProceeds](cdc),
		),
		EpochInitialIssuance: collections.NewItem(
			sb,
			types.EpochInitialIssuanceKey,
			"epoch_initial_issuance",
			codec.CollValue[types.EpochInitialIssuance](cdc),
		),
		EpochStates: collections.NewMap(
			sb,
			types.EpochStatesKey,
			"epoch_states",
			collections.Uint64Key,
			codec.CollValue[types.EpochState](cdc),
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

// RecordEpochTaxProceeds adds tax proceeds that have been added this epoch
func (k Keeper) RecordEpochTaxProceeds(ctx context.Context, delta sdk.Coins) error {
	if delta.IsZero() {
		return nil
	}

	proceeds, err := k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting epoch tax proceeds: %w", err)
	}
	proceeds.TaxProceeds = proceeds.TaxProceeds.Add(delta...)

	if err := k.EpochTaxProceeds.Set(ctx, proceeds); err != nil {
		return fmt.Errorf("setting epoch tax proceeds: %w", err)
	}
	return nil
}

// RecordEpochInitialIssuance updates epoch initial issuance from supply keeper
func (k Keeper) RecordEpochInitialIssuance(ctx context.Context) error {
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return err
	}

	totalSupply := make(sdk.Coins, len(tobinTaxes)+1)
	totalSupply[0] = k.bankKeeper.GetSupply(ctx, core.MicroArkDenom)

	for i, denom := range tobinTaxes {
		totalSupply[i+1] = k.bankKeeper.GetSupply(ctx, denom.Denom)
	}

	epochInitialIssuance := types.EpochInitialIssuance{
		Issuance: totalSupply.Sort(),
	}
	if err := k.EpochInitialIssuance.Set(ctx, epochInitialIssuance); err != nil {
		return fmt.Errorf("setting epoch initial issuance: %w", err)
	}
	return nil
}

// ComputeEpochSeigniorage returns epoch seigniorage
func (k Keeper) ComputeEpochSeigniorage(ctx context.Context) (math.Int, error) {
	epochIssuance := k.bankKeeper.GetSupply(ctx, core.MicroArkDenom).Amount
	epochIntialIssuance, err := k.EpochInitialIssuance.Get(ctx)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("getting epoch initial issuance: %w", err)
	}
	preEpochIssuance := epochIntialIssuance.Issuance.AmountOf(core.MicroArkDenom)
	epochSeigniorage := preEpochIssuance.Sub(epochIssuance)

	if epochSeigniorage.IsNegative() {
		return math.ZeroInt(), nil
	}

	return epochSeigniorage, nil
}
