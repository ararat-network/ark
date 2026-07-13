package keeper

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"slices"
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
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

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
	Accounting   collections.Item[types.AccountingState]
	ExchangeRate collections.Map[string, types.ExchangeRate]
	ScoreWeight  collections.Map[sdk.ValAddress, math.Int]
	MissCount    collections.Map[sdk.ValAddress, uint64]
	VoteTargets  collections.Item[types.VoteTargetState]
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
			codec.CollValue[types.AccountingState](cdc),
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
			sdk.IntValue,
		),
		MissCount: collections.NewMap(
			sb,
			types.MissCountKey,
			"miss_count",
			sdk.ValAddressKey,
			collections.Uint64Value,
		),
		VoteTargets: collections.NewItem(
			sb,
			types.VoteTargetsKey,
			"vote_targets",
			codec.CollValue[types.VoteTargetState](cdc),
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
	if denom == chain.MicroNoahDenom {
		return math.LegacyOneDec(), nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting params: %w", err)
	}

	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	return k.getExchangeRate(ctx, denom, currentTime, params.MaxExchangeRateAge)
}

func (k Keeper) getExchangeRate(ctx context.Context, denom string, currentTime time.Time, maxAge time.Duration) (math.LegacyDec, error) {
	exchangeRate, err := k.ExchangeRate.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
		}
		return math.LegacyZeroDec(), fmt.Errorf("getting exchange rate for denom %s: %w", denom, err)
	}
	if err := validateExchangeRate(denom, exchangeRate, currentTime, maxAge); err != nil {
		return math.LegacyZeroDec(), err
	}

	return exchangeRate.Rate, nil
}

func validateExchangeRate(
	denom string,
	exchangeRate types.ExchangeRate,
	currentTime time.Time,
	maxAge time.Duration,
) error {
	if err := chain.ValidateMicroDenom(denom); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidExchangeRate, "invalid stored denom %q: %v", denom, err)
	}
	if exchangeRate.Denom != denom {
		return sdkerrors.Wrapf(
			types.ErrInvalidExchangeRate,
			"stored denom %q does not match collection key %q",
			exchangeRate.Denom,
			denom,
		)
	}
	if exchangeRate.Rate.IsNil() {
		return sdkerrors.Wrapf(
			types.ErrInvalidExchangeRate,
			"%s rate is unset",
			denom,
		)
	}
	if !exchangeRate.Rate.IsPositive() {
		return sdkerrors.Wrapf(
			types.ErrInvalidExchangeRate,
			"%s rate %s",
			denom,
			exchangeRate.Rate,
		)
	}
	if exchangeRate.BlockTimestamp.After(currentTime) {
		return sdkerrors.Wrapf(
			types.ErrInvalidExchangeRate,
			"%s rate timestamp %s is after current block time %s",
			denom,
			exchangeRate.BlockTimestamp,
			currentTime,
		)
	}
	if age := currentTime.Sub(exchangeRate.BlockTimestamp); age > maxAge {
		return sdkerrors.Wrapf(
			types.ErrStaleExchangeRate,
			"%s rate age %s exceeds maximum %s (updated %s, current %s)",
			denom,
			age,
			maxAge,
			exchangeRate.BlockTimestamp,
			currentTime,
		)
	}

	return nil
}

// SetExchangeRateWithEvent stores an exchange rate and emits an update event.
func (k Keeper) SetExchangeRateWithEvent(ctx context.Context, exchangeRate types.ExchangeRate) error {
	if err := chain.ValidateMicroDenom(exchangeRate.Denom); err != nil {
		return fmt.Errorf("invalid exchange rate denom: %w", err)
	}
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
	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, exchangeRate types.ExchangeRate) (bool, error) {
		if err := validateExchangeRate(denom, exchangeRate, currentTime, params.MaxExchangeRateAge); err != nil {
			if errors.Is(err, types.ErrStaleExchangeRate) {
				return false, nil
			}
			return true, err
		}
		actives = append(actives, denom)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating active oracle denoms: %w", err)
	}

	return actives, nil
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
func (k Keeper) GetTobinTaxes(ctx context.Context) (types.TobinTaxes, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}

	return slices.Clone(params.TobinTaxes), nil
}

// GetVoteTargets returns the staged vote-target denoms.
func (k Keeper) GetVoteTargets(ctx context.Context) ([]string, error) {
	voteTargets, err := k.VoteTargets.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting vote targets: %w", err)
	}

	return slices.Clone(voteTargets.Denoms), nil
}

// SyncVoteTargets applies the configured denoms to the staged vote-target set
// and prunes exchange rates for removed targets.
func (k Keeper) SyncVoteTargets(ctx context.Context, oldVoteTargets []string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	denoms := make([]string, len(params.TobinTaxes))
	for i, tobinTax := range params.TobinTaxes {
		denoms[i] = tobinTax.Denom
	}
	slices.Sort(denoms)
	if slices.Equal(oldVoteTargets, denoms) {
		return nil
	}

	configured := make(map[string]struct{}, len(denoms))
	for _, denom := range denoms {
		configured[denom] = struct{}{}
	}

	for _, denom := range oldVoteTargets {
		if _, ok := configured[denom]; ok {
			continue
		}
		if err := k.ExchangeRate.Remove(ctx, denom); err != nil {
			return fmt.Errorf("removing exchange rate for vote target %s: %w", denom, err)
		}
	}

	if err := k.VoteTargets.Set(ctx, types.VoteTargetState{Denoms: denoms}); err != nil {
		return fmt.Errorf("setting vote targets: %w", err)
	}

	return nil
}

func (k Keeper) registerTobinTaxMetadata(ctx context.Context, denom string) {
	if _, ok := k.bankKeeper.GetDenomMetaData(ctx, denom); ok {
		return
	}

	display := denom[1:]
	k.bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
		Description: "The native stable token of Ark Icarus.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: "u" + display, Exponent: uint32(0), Aliases: []string{"micro" + display}},
			{Denom: "m" + display, Exponent: uint32(3), Aliases: []string{"milli" + display}},
			{Denom: display, Exponent: uint32(6), Aliases: []string{}},
		},
		Base:    denom,
		Display: display,
		Name:    fmt.Sprintf("%s ARK", strings.ToUpper(display)),
		Symbol:  fmt.Sprintf("%sA", strings.ToUpper(display[:len(display)-1])),
	})
}

// RecordVoteAccounting records score weight and miss status for the validator
// resolved from a consensus address. If the validator no longer resolves,
// accounting is skipped.
func (k Keeper) RecordVoteAccounting(
	ctx context.Context,
	consAddr sdk.ConsAddress,
	scoreWeight math.Int,
	missed bool,
) error {
	if scoreWeight.IsNil() {
		return errors.New("score weight must be set")
	}
	if scoreWeight.IsNegative() {
		return fmt.Errorf("score weight must not be negative: %s", scoreWeight)
	}
	if scoreWeight.IsZero() && !missed {
		return nil
	}

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

	updatedScoreWeight := math.ZeroInt()
	if scoreWeight.IsPositive() {
		currentScoreWeight, err := k.ScoreWeight.Get(ctx, valAddr)
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return fmt.Errorf("getting score weight: %w", err)
			}
			currentScoreWeight = math.ZeroInt()
		}
		updatedScoreWeight = currentScoreWeight.Add(scoreWeight)
	}

	var updatedMissCount uint64
	if missed {
		currentMissCount, err := k.MissCount.Get(ctx, valAddr)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting miss count: %w", err)
		}
		var carry uint64
		updatedMissCount, carry = bits.Add64(currentMissCount, 1, 0)
		if carry != 0 {
			return fmt.Errorf("miss count overflow for validator %s", valAddr)
		}
	}

	if scoreWeight.IsPositive() {
		if err := k.ScoreWeight.Set(ctx, valAddr, updatedScoreWeight); err != nil {
			return fmt.Errorf("setting score weight for validator %s: %w", validator, err)
		}
	}
	if missed {
		if err := k.MissCount.Set(ctx, valAddr, updatedMissCount); err != nil {
			return fmt.Errorf("setting miss count for validator %s: %w", validator, err)
		}
	}

	return nil
}
