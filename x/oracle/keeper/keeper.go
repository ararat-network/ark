package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	core "noah/types"
	"noah/x/oracle/types"
)

// Keeper of the oracle store
type Keeper struct {
	cdc              codec.BinaryCodec
	storeService     store.KVStoreService
	authority        string
	distributionName string

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	distrKeeper   types.DistributionKeeper
	stakingKeeper types.StakingKeeper

	Schema       collections.Schema
	Params       collections.Item[types.Params]
	ExchangeRate collections.Map[string, types.ExchangeRate]
	ScoreWeight  collections.Map[sdk.ValAddress, uint64]
	MissCount    collections.Map[sdk.ValAddress, uint64]
}

// NewKeeper constructs a new keeper for oracle
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
	// ensure oracle module account is set
	if addr := accountKeeper.GetModuleAddress(types.ModuleName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:              cdc,
		storeService:     storeService,
		authority:        authority,
		distributionName: distributionName,
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
		ExchangeRate: collections.NewMap(
			sb,
			types.ExchangeRateKey,
			"exchange_rate",
			collections.StringKey,
			codec.CollValue[types.ExchangeRate](cdc),
		),
		ScoreWeight: collections.NewMap(
			sb,
			types.ScoreWeightKey,
			"socre_weight",
			sdk.ValAddressKey,
			collections.Uint64Value,
		),
		MissCount: collections.NewMap(
			sb,
			types.MissCountKey,
			"miss_counter",
			sdk.ValAddressKey,
			collections.Uint64Value,
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

// GetParams returns the stored params
func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Params{}, err
	}

	return params, nil
}

// GetExchangeRate gets the consensus exchange rate of Ark denominated in the denom asset from the store.
func (k Keeper) GetExchangeRate(ctx context.Context, denom string) (math.LegacyDec, error) {
	if denom == core.MicroArkDenom {
		return math.LegacyOneDec(), nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting params: %w", err)
	}

	exchangeRate, err := k.ExchangeRate.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
		}
		return math.LegacyZeroDec(), fmt.Errorf("getting exchange rate for denom %s: %w", denom, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := uint64(sdkCtx.BlockHeight())
	if currentHeight > exchangeRate.BlockHeight &&
		currentHeight-exchangeRate.BlockHeight > params.MaxExchangeRateAge {
		return math.LegacyZeroDec(), sdkerrors.Wrapf(
			types.ErrStaleExchangeRate,
			"%s rate height %d, current height %d",
			denom,
			exchangeRate.BlockHeight,
			currentHeight,
		)
	}

	return exchangeRate.Rate, nil
}

// SetExchangeRateWithEvent sets the consensus exchange rate of Ark denominated in the denom asset to the
// store with ABCI event
func (k Keeper) SetExchangeRateWithEvent(ctx context.Context, exchangeRate types.ExchangeRate) error {
	if err := k.ExchangeRate.Set(ctx, exchangeRate.Denom, exchangeRate); err != nil {
		return fmt.Errorf("setting exchange rate with event for denom %s: %w", exchangeRate.Denom, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeExchangeRateUpdate,
			sdk.NewAttribute(types.AttributeKeyDenom, exchangeRate.Denom),
			sdk.NewAttribute(types.AttributeKeyExchangeRate, exchangeRate.Rate.String()),
		),
	)

	return nil
}

// GetActives returns a list of denoms that with a stored exchange rate with fresh prices
func (k Keeper) GetActives(ctx context.Context) ([]string, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}

	var actives []string
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := uint64(sdkCtx.BlockHeight())
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, exchangeRate types.ExchangeRate) (bool, error) {
		if currentHeight > exchangeRate.BlockHeight &&
			currentHeight-exchangeRate.BlockHeight > params.MaxExchangeRateAge {
			return false, nil
		}
		actives = append(actives, denom)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating active oracle denoms: %w", err)
	}

	return actives, nil
}

// GetTobinTaxes gets all tobin taxes
func (k Keeper) GetTobinTaxes(ctx context.Context) (types.TobinTaxes, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	return params.TobinTaxes, nil
}

// GetMaxTobinTax gets the tobin tax of a given denom
func (k Keeper) GetMaxTobinTax(ctx context.Context, denoms ...string) (math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("getting params: %w", err)
	}

	wanted := make(map[string]struct{}, len(denoms))
	for _, denom := range denoms {
		wanted[denom] = struct{}{}
	}

	maxTax := math.LegacyZeroDec()
	found := 0

	for _, tt := range params.TobinTaxes {
		if _, ok := wanted[tt.Denom]; !ok {
			continue
		}

		found++
		if tt.TobinTax.GT(maxTax) {
			maxTax = tt.TobinTax
		}
	}

	if found != len(wanted) {
		return math.LegacyDec{}, types.ErrUnknownDenom
	}

	return maxTax, nil
}

// ApplyTobinTaxChanges registers metadata for active Tobin tax denoms and prunes exchange rates for removed denoms.
func (k Keeper) ApplyTobinTaxChanges(ctx context.Context, tobinTaxes types.TobinTaxes) error {
	active := make(map[string]struct{}, len(tobinTaxes))
	for _, tt := range tobinTaxes {
		active[tt.Denom] = struct{}{}
		// Register meta data to bank module
		if _, ok := k.bankKeeper.GetDenomMetaData(ctx, tt.Denom); !ok {
			base := tt.Denom
			display := base[1:]

			k.bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
				Description: "The native stable token of Noah Icarus.",
				DenomUnits: []*banktypes.DenomUnit{
					{Denom: "u" + display, Exponent: uint32(0), Aliases: []string{"micro" + display}},
					{Denom: "m" + display, Exponent: uint32(3), Aliases: []string{"milli" + display}},
					{Denom: display, Exponent: uint32(6), Aliases: []string{}},
				},
				Base:    base,
				Display: display,
				Name:    fmt.Sprintf("%s NOAH", strings.ToUpper(display)),
				Symbol:  fmt.Sprintf("%sN", strings.ToUpper(display[:len(display)-1])),
			})
		}
	}

	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, _ types.ExchangeRate) (bool, error) {
		if _, ok := active[denom]; ok {
			return false, nil
		}

		return false, k.ExchangeRate.Remove(ctx, denom)
	}); err != nil {
		return err
	}

	return nil
}

// IncrementMissCount adds the miss count to the given validator
func (k Keeper) IncrementMissCount(ctx context.Context, validator sdk.ValAddress) error {
	missCount, err := k.MissCount.Get(ctx, validator)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("getting miss count: %w", err)
	}
	if err := k.MissCount.Set(ctx, validator, missCount+1); err != nil {
		return fmt.Errorf("setting miss count: %w", err)
	}

	return nil
}

// AddScoreWeight adds to the score weight for the given validator
func (k Keeper) AddScoreWeight(ctx context.Context, validator sdk.ValAddress, scoreWeight uint64) error {
	currentScoreWeight, err := k.ScoreWeight.Get(ctx, validator)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("getting score weight: %w", err)
	}
	if err := k.ScoreWeight.Set(ctx, validator, currentScoreWeight+scoreWeight); err != nil {
		return fmt.Errorf("setting score weight for validator %s: %w", validator, err)
	}

	return nil
}
