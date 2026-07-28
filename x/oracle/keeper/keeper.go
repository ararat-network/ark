package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

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

	Schema       collections.Schema
	Params       collections.Item[types.Params]
	Accounting   collections.Item[types.Accounting]
	ExchangeRate collections.Map[string, types.ExchangeRate]
	RewardWeight collections.Map[sdk.ValAddress, math.Int]
	Attendance   collections.Map[sdk.ValAddress, types.Attendance]
	VoteTargets  collections.Item[types.VoteTargets]
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
		VoteTargets: collections.NewItem(
			sb,
			types.VoteTargetsKey,
			"vote_targets",
			codec.CollValue[types.VoteTargets](cdc),
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

	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting params: %w", err)
	}

	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	return k.getExchangeRate(ctx, denom, currentTime, params.MaxExchangeRateAge)
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
		if currentTime.Sub(exchangeRate.BlockTimestamp) > params.MaxExchangeRateAge {
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
	if err := chain.ValidateNativeBaseDenom(exchangeRate.Denom); err != nil {
		return fmt.Errorf("invalid exchange rate denom: %w", err)
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

// GetTobinTax returns the configured Tobin tax for a denom.
func (k Keeper) GetTobinTax(ctx context.Context, denom string) (math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting params: %w", err)
	}

	for _, tobinTax := range params.TobinTaxes {
		if tobinTax.Denom == denom {
			return tobinTax.TobinTax, nil
		}
	}

	return math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
}

// GetTobinTaxes returns configured Tobin taxes.
func (k Keeper) GetTobinTaxes(ctx context.Context) ([]types.TobinTax, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}

	return params.TobinTaxes, nil
}

// GetVoteTargets returns the target epoch validators must report at voteHeight.
func (k Keeper) GetVoteTargets(ctx context.Context, voteHeight int64) (types.VoteTargetSet, error) {
	voteTargets, err := k.VoteTargets.Get(ctx)
	if err != nil {
		return types.VoteTargetSet{}, fmt.Errorf("getting vote targets: %w", err)
	}

	return voteTargets.AtHeight(voteHeight), nil
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

func (k Keeper) registerTobinTaxMetadata(ctx context.Context, denom string) {
	if _, ok := k.bankKeeper.GetDenomMetaData(ctx, denom); ok {
		return
	}

	display := denom[1:]
	k.bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
		Description: "The native stable token of Ark Icarus.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: denom, Exponent: 0},
			{Denom: display, Exponent: chain.NativeDisplayExponent},
		},
		Base:    denom,
		Display: display,
		Name:    fmt.Sprintf("%s ARK", strings.ToUpper(display)),
		Symbol:  fmt.Sprintf("%sA", strings.ToUpper(display[:len(display)-1])),
	})
}
