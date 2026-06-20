package keeper

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	chain "noah/pkg/chain"
	"noah/x/oracle/types"
)

// Keeper stores oracle module state.
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
	TobinTax     collections.Map[string, math.LegacyDec]
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
			"miss_count",
			sdk.ValAddressKey,
			collections.Uint64Value,
		),
		TobinTax: collections.NewMap(
			sb,
			types.TobinTaxKey,
			"pending_tobin_taxes",
			collections.StringKey,
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

// GetParams returns oracle params.
func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Params{}, err
	}

	return params, nil
}

// GetExchangeRate returns the consensus Ark exchange rate for a denom.
func (k Keeper) GetExchangeRate(ctx context.Context, denom string) (math.LegacyDec, error) {
	if denom == chain.MicroArkDenom {
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

// SetExchangeRateWithEvent stores an exchange rate and emits an update event.
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

// GetActives returns denoms with non-stale exchange rates.
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

// GetTobinTax returns the active Tobin tax for a denom.
func (k Keeper) GetTobinTax(ctx context.Context, denom string) (math.LegacyDec, error) {
	tobinTax, err := k.TobinTax.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
		}
		return math.LegacyZeroDec(), fmt.Errorf("getting tobin tax for denom %s: %w", denom, err)
	}

	return tobinTax, nil
}

// GetTobinTaxes returns active Tobin taxes.
func (k Keeper) GetTobinTaxes(ctx context.Context) (types.TobinTaxes, error) {
	tobinTaxes := types.TobinTaxes{}
	if err := k.TobinTax.Walk(ctx, nil, func(denom string, tobinTax math.LegacyDec) (bool, error) {
		tobinTaxes = append(tobinTaxes, types.TobinTax{Denom: denom, TobinTax: tobinTax})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating tobin taxes: %w", err)
	}

	return tobinTaxes, nil
}

// GetVoteTargets returns active vote targets keyed by denom.
func (k Keeper) GetVoteTargets(ctx context.Context) (map[string]math.LegacyDec, error) {
	voteTargets := make(map[string]math.LegacyDec)
	if err := k.TobinTax.Walk(ctx, nil, func(denom string, tobinTax math.LegacyDec) (bool, error) {
		voteTargets[denom] = tobinTax
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating tobin taxes: %w", err)
	}

	return voteTargets, nil
}

// SyncTobinTax applies params Tobin taxes to the active set and prunes removed denoms.
func (k Keeper) SyncTobinTax(ctx context.Context, oldTobinTaxes map[string]math.LegacyDec) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	added := []types.TobinTax{}
	modified := []types.TobinTax{}
	removed := maps.Clone(oldTobinTaxes)
	for _, tt := range params.TobinTaxes {
		if tax, ok := removed[tt.Denom]; !ok {
			added = append(added, tt)
		} else if !tt.TobinTax.Equal(tax) {
			modified = append(modified, tt)
		}
		delete(removed, tt.Denom)
	}

	for _, tt := range added {
		if err := k.TobinTax.Set(ctx, tt.Denom, tt.TobinTax); err != nil {
			return fmt.Errorf("setting tobin tax: %w", err)
		}
		k.registerTobinTaxMetadata(ctx, tt.Denom)
	}

	for _, tt := range modified {
		if err := k.TobinTax.Set(ctx, tt.Denom, tt.TobinTax); err != nil {
			return fmt.Errorf("setting tobin tax: %w", err)
		}
	}

	for denom := range removed {
		if err := k.ExchangeRate.Remove(ctx, denom); err != nil {
			return fmt.Errorf("removing exchange rate: %w", err)
		}
		if err := k.TobinTax.Remove(ctx, denom); err != nil {
			return fmt.Errorf("removing tobin tax: %w", err)
		}
	}

	return nil
}

func (k Keeper) registerTobinTaxMetadata(ctx context.Context, denom string) {
	if _, ok := k.bankKeeper.GetDenomMetaData(ctx, denom); ok {
		return
	}

	display := denom[1:]
	k.bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
		Description: "The native stable token of Noah Icarus.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: "u" + display, Exponent: uint32(0), Aliases: []string{"micro" + display}},
			{Denom: "m" + display, Exponent: uint32(3), Aliases: []string{"milli" + display}},
			{Denom: display, Exponent: uint32(6), Aliases: []string{}},
		},
		Base:    denom,
		Display: display,
		Name:    fmt.Sprintf("%s NOAH", strings.ToUpper(display)),
		Symbol:  fmt.Sprintf("%sN", strings.ToUpper(display[:len(display)-1])),
	})
}

// IncrementMissCount increments the miss count for the validator resolved from a consensus address.
// If the consensus address no longer resolves to a staking validator, accounting is skipped.
func (k Keeper) IncrementMissCount(ctx context.Context, consAddr sdk.ConsAddress) error {
	validator, err := k.stakingKeeper.ValidatorByConsAddr(ctx, consAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return nil
		}
		return fmt.Errorf("getting validator by consensus address %s: %w", consAddr, err)
	}
	if validator == nil {
		return nil
	}

	valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
	if err != nil {
		return fmt.Errorf("parsing validator operator address %q: %w", validator.GetOperator(), err)
	}
	missCount, err := k.MissCount.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("getting miss count: %w", err)
	}
	if err := k.MissCount.Set(ctx, valAddr, missCount+1); err != nil {
		return fmt.Errorf("setting miss count: %w", err)
	}

	return nil
}

// AddScoreWeight adds score weight for the validator resolved from a consensus address.
// If the consensus address no longer resolves to a staking validator, accounting is skipped.
func (k Keeper) AddScoreWeight(ctx context.Context, consAddr sdk.ConsAddress, scoreWeight uint64) error {
	validator, err := k.stakingKeeper.ValidatorByConsAddr(ctx, consAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return nil
		}
		return fmt.Errorf("getting validator by consensus address %s: %w", consAddr, err)
	}
	if validator == nil {
		return nil
	}

	valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
	if err != nil {
		return fmt.Errorf("parsing validator operator address %q: %w", validator.GetOperator(), err)
	}
	currentScoreWeight, err := k.ScoreWeight.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("getting score weight: %w", err)
	}
	if err := k.ScoreWeight.Set(ctx, valAddr, currentScoreWeight+scoreWeight); err != nil {
		return fmt.Errorf("setting score weight for validator %s: %w", validator, err)
	}

	return nil
}
