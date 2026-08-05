package keeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

// Keeper stores oracle module state.
type Keeper struct {
	cdc              codec.BinaryCodec
	storeService     store.KVStoreService
	authority        string
	distributionName string
	moduleAddress    sdk.AccAddress

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	distrKeeper   types.DistributionKeeper
	stakingKeeper types.StakingKeeper

	// Market and Treasury already depend on x/oracle, so their reference
	// rebase executors are injected after construction to avoid a dependency
	// cycle. These are the only oracle-to-consumer edges and exist solely to
	// re-denominate consumer reference-unit state atomically with a reference
	// move.
	marketReferenceKeeper   types.MarketReferenceDenomKeeper
	treasuryReferenceKeeper types.TreasuryReferenceDenomKeeper

	Schema                      collections.Schema
	Params                      collections.Item[types.Params]
	Accounting                  collections.Item[types.Accounting]
	ExchangeRate                collections.Map[string, types.ExchangeRate]
	RewardWeight                collections.Map[sdk.ValAddress, math.Int]
	Attendance                  collections.Map[sdk.ValAddress, types.Attendance]
	Feeds                       collections.Item[types.Feeds]
	ReferenceDenom              collections.Item[string]
	MaxExchangeRateAgeOverrides collections.Map[string, time.Duration]

	// feedReferentGuards answer, at removal time, whether a consumer still
	// depends on a feed. They derive from the consumer's own state; nothing is
	// indexed here.
	feedReferentGuards []FeedReferentGuard
}

// NewKeeper constructs an oracle keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	distributionName string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	distrKeeper types.DistributionKeeper,
	stakingKeeper types.StakingKeeper,
) *Keeper {
	// Ensure the oracle module account is configured.
	moduleAddress := accountKeeper.GetModuleAddress(types.ModuleName)
	if moduleAddress == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}
	if addr := accountKeeper.GetModuleAddress(distributionName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", distributionName))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:              cdc,
		storeService:     storeService,
		authority:        authority,
		distributionName: distributionName,
		moduleAddress:    moduleAddress,
		accountKeeper:    accountKeeper,
		bankKeeper:       bankKeeper,
		distrKeeper:      distrKeeper,
		stakingKeeper:    stakingKeeper,
		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		Accounting: collections.NewItem(
			sb,
			types.AccountingKey,
			"accounting",
			codec.CollValue[types.Accounting](cdc),
		),
		ExchangeRate: collections.NewMap(
			sb,
			types.ExchangeRateKey,
			"exchange_rate",
			collections.StringKey,
			codec.CollValue[types.ExchangeRate](cdc),
		),
		RewardWeight: collections.NewMap(
			sb,
			types.RewardWeightKey,
			"reward_weight",
			sdk.ValAddressKey,
			sdk.IntValue,
		),
		Attendance: collections.NewMap(
			sb,
			types.AttendanceKey,
			"attendance",
			sdk.ValAddressKey,
			codec.CollValue[types.Attendance](cdc),
		),
		Feeds: collections.NewItem(
			sb,
			types.FeedsKey,
			"feeds",
			codec.CollValue[types.Feeds](cdc),
		),
		ReferenceDenom: collections.NewItem(
			sb,
			types.ReferenceDenomKey,
			"reference_denom",
			collections.StringValue,
		),
		MaxExchangeRateAgeOverrides: collections.NewMap(
			sb,
			types.MaxExchangeRateAgeOverridesKey,
			"max_exchange_rate_age_overrides",
			collections.StringKey,
			types.DurationValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// SetReferenceDenomConsumers injects the Market and Treasury rebase executors run
// whenever governance re-points the protocol reference, which is the only way
// it moves. It must be called once during application wiring, after both
// consumer keepers exist.
func (k *Keeper) SetReferenceDenomConsumers(
	marketKeeper types.MarketReferenceDenomKeeper,
	treasuryKeeper types.TreasuryReferenceDenomKeeper,
) {
	k.marketReferenceKeeper = marketKeeper
	k.treasuryReferenceKeeper = treasuryKeeper
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetParams returns oracle params.
func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Params{}, err
	}

	return params, nil
}

// GetExchangeRate returns the consensus Noah exchange rate for a denom.
func (k Keeper) GetExchangeRate(ctx context.Context, denom string) (math.LegacyDec, error) {
	if denom == chain.NoahBaseDenom {
		return math.LegacyOneDec(), nil
	}

	maxAge, err := k.MaxAgeFor(ctx, denom)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	return k.getExchangeRate(ctx, denom, currentTime, maxAge)
}

// GetExchangeRates returns all non-stale stored exchange rates.
func (k Keeper) GetExchangeRates(ctx context.Context) (sdk.DecCoins, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}

	var exchangeRates sdk.DecCoins
	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, exchangeRate types.ExchangeRate) (bool, error) {
		maxAge, err := k.maxAgeFor(ctx, params, denom)
		if err != nil {
			return false, err
		}
		if currentTime.Sub(exchangeRate.BlockTimestamp) > maxAge {
			return false, nil
		}
		exchangeRates = append(exchangeRates, sdk.NewDecCoinFromDec(denom, exchangeRate.Rate))
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating exchange rates: %w", err)
	}

	return exchangeRates, nil
}

// SetExchangeRateWithEvent stores an exchange rate and emits an update event.
func (k Keeper) SetExchangeRateWithEvent(ctx context.Context, exchangeRate types.ExchangeRate) error {
	// Rates are keyed by the denomination the feed prices, which may run ahead
	// of that denomination being listed as an asset. The numeraire is excluded
	// for the same reason it has no feed.
	if err := chain.ValidatePricedDenom(exchangeRate.Denom); err != nil {
		return fmt.Errorf("invalid exchange rate feed: %w", err)
	}
	if exchangeRate.Rate.IsNil() {
		return sdkerrors.Wrapf(types.ErrInvalidExchangeRate, "%s rate is unset", exchangeRate.Denom)
	}
	if !exchangeRate.Rate.IsInValidRange() {
		return sdkerrors.Wrapf(types.ErrInvalidExchangeRate, "%s rate is not representable", exchangeRate.Denom)
	}
	if !exchangeRate.Rate.IsPositive() {
		return sdkerrors.Wrapf(types.ErrInvalidExchangeRate, "%s rate %s is not positive", exchangeRate.Denom, exchangeRate.Rate)
	}

	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	if exchangeRate.BlockTimestamp.After(currentTime) {
		return sdkerrors.Wrapf(
			types.ErrInvalidExchangeRate,
			"%s rate timestamp %s is after current block time %s",
			exchangeRate.Denom,
			exchangeRate.BlockTimestamp,
			currentTime,
		)
	}
	if err := k.ExchangeRate.Set(ctx, exchangeRate.Denom, exchangeRate); err != nil {
		return fmt.Errorf("setting exchange rate with event for denom %s: %w", exchangeRate.Denom, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventExchangeRateUpdate{
		Denom:        exchangeRate.Denom,
		ExchangeRate: exchangeRate.Rate,
	}); err != nil {
		return fmt.Errorf("emitting exchange rate update event for %s: %w", exchangeRate.Denom, err)
	}

	return nil
}

func (k Keeper) getExchangeRate(ctx context.Context, denom string, currentTime time.Time, maxAge time.Duration) (math.LegacyDec, error) {
	exchangeRate, err := k.ExchangeRate.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
		}
		return math.LegacyZeroDec(), fmt.Errorf("getting exchange rate for denom %s: %w", denom, err)
	}
	if currentTime.Sub(exchangeRate.BlockTimestamp) > maxAge {
		return math.LegacyZeroDec(), sdkerrors.Wrapf(
			types.ErrStaleExchangeRate,
			"%s rate age exceeds maximum (updated %s, current %s)",
			denom,
			exchangeRate.BlockTimestamp,
			currentTime,
		)
	}

	return exchangeRate.Rate, nil
}
