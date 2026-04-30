package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

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

	Schema           collections.Schema
	Params           collections.Item[types.Params]
	FeederDelegation collections.Map[sdk.ValAddress, sdk.AccAddress]
	ExchangeRate     collections.Map[string, math.LegacyDec]
	MissCount        collections.Map[sdk.ValAddress, uint64]
	Prevote          collections.Map[sdk.ValAddress, types.Prevote]
	Vote             collections.Map[sdk.ValAddress, types.Vote]
	TobinTax         collections.Map[string, math.LegacyDec]
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
		FeederDelegation: collections.NewMap(
			sb,
			types.FeederDelegationKey,
			"feeder_delegation",
			sdk.ValAddressKey,
			collcodec.KeyToValueCodec(sdk.AccAddressKey),
		),
		ExchangeRate: collections.NewMap(
			sb,
			types.ExchangeRateKey,
			"exchange_rate",
			collections.StringKey,
			sdk.LegacyDecValue,
		),
		MissCount: collections.NewMap(
			sb,
			types.MissCountKey,
			"miss_counter",
			sdk.ValAddressKey,
			collections.Uint64Value,
		),
		Prevote: collections.NewMap(
			sb,
			types.PrevoteKey,
			"aggregate_exchange_rate_prevote",
			sdk.ValAddressKey,
			codec.CollValue[types.Prevote](cdc),
		),
		Vote: collections.NewMap(
			sb,
			types.VoteKey,
			"aggregate_exchange_rate_vote",
			sdk.ValAddressKey,
			codec.CollValue[types.Vote](cdc),
		),
		TobinTax: collections.NewMap(
			sb,
			types.TobinTaxKey,
			"tobin_tax",
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

// GetFeederDelegation gets the account address that the validator operator delegated oracle vote rights to
func (k Keeper) GetFeederDelegation(ctx context.Context, operator sdk.ValAddress) (sdk.AccAddress, error) {
	accAddress, err := k.FeederDelegation.Get(ctx, operator)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return sdk.AccAddress(operator), nil
		}
		return nil, fmt.Errorf("getting feeder delegation for validator %s: %w", operator, err)
	}

	return accAddress, nil
}

// GetExchangeRate gets the consensus exchange rate of Ark denominated in the denom asset from the store.
func (k Keeper) GetExchangeRate(ctx context.Context, denom string) (math.LegacyDec, error) {
	if denom == core.MicroArkDenom {
		return math.LegacyOneDec(), nil
	}

	exchangeRate, err := k.ExchangeRate.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
		}
		return math.LegacyZeroDec(), fmt.Errorf("getting exchange rate for denom %s: %w", denom, err)
	}

	return exchangeRate, nil
}

// SetExchangeRate sets the consensus exchange rate of Ark denominated in the denom asset.
func (k Keeper) SetExchangeRate(ctx context.Context, denom string, rate math.LegacyDec) error {
	if err := k.ExchangeRate.Set(ctx, denom, rate); err != nil {
		return fmt.Errorf("setting exchange rate for denom %s: %w", denom, err)
	}

	return nil
}

// IterateExchangeRates iterates over all stored exchange rates until the handler returns true.
func (k Keeper) IterateExchangeRates(ctx context.Context, handler func(denom string, rate math.LegacyDec) (stop bool)) error {
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, rate math.LegacyDec) (bool, error) {
		return handler(denom, rate), nil
	}); err != nil {
		return err
	}

	return nil
}

// SetExchangeRateWithEvent sets the consensus exchange rate of Ark denominated in the denom asset to the
// store with ABCI event
func (k Keeper) SetExchangeRateWithEvent(ctx context.Context, denom string, exchangeRate math.LegacyDec) error {
	if err := k.ExchangeRate.Set(ctx, denom, exchangeRate); err != nil {
		return fmt.Errorf("setting exchange rate with event for denom %s: %w", denom, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeExchangeRateUpdate,
			sdk.NewAttribute(types.AttributeKeyDenom, denom),
			sdk.NewAttribute(types.AttributeKeyExchangeRate, exchangeRate.String()),
		),
	)

	return nil
}

// ValidateFeeder validates if the given feeder is allowed to feed the message or not
func (k Keeper) ValidateFeeder(ctx context.Context, feederAddr sdk.AccAddress, validatorAddr sdk.ValAddress) error {
	if !feederAddr.Equals(validatorAddr) {
		delegate, err := k.GetFeederDelegation(ctx, validatorAddr)
		if err != nil {
			return fmt.Errorf("getting feeder delegation for validator %s while validating feeder %s: %w", validatorAddr, feederAddr, err)
		}
		if !delegate.Equals(feederAddr) {
			return sdkerrors.Wrap(types.ErrNoVotingPermission, feederAddr.String())
		}
	}

	// Check that the given validator exists
	if val, err := k.stakingKeeper.Validator(ctx, validatorAddr); err != nil {
		return fmt.Errorf("getting validator %s while validating feeder %s: %w", validatorAddr, feederAddr, err)
	} else if val == nil || !val.IsBonded() {
		return sdkerrors.Wrapf(stakingtypes.ErrNoValidatorFound, "validator %s is not active set", validatorAddr.String())
	}

	return nil
}

// GetTobinTax gets the tobin tax of the specified denom
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

// SetTobinTax sets the tobin tax for the denom.
func (k Keeper) SetTobinTax(ctx context.Context, denom string, tobinTax math.LegacyDec) error {
	if err := k.TobinTax.Set(ctx, denom, tobinTax); err != nil {
		return fmt.Errorf("setting tobin tax for denom %s: %w", denom, err)
	}

	return nil
}

// GetTobinTaxes gets the stored tobin taxes.
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

// SetTobinTaxes replaces the stored tobin taxes and registers bank metadata for active denoms.
func (k Keeper) SetTobinTaxes(ctx context.Context, tobinTaxes types.TobinTaxes) error {
	if err := k.TobinTax.Walk(
		ctx,
		nil,
		func(denom string, _ math.LegacyDec) (bool, error) {
			if err := k.TobinTax.Remove(ctx, denom); err != nil {
				return false, err
			}

			return false, nil
		},
	); err != nil {
		return err
	}

	for _, item := range tobinTaxes {
		if err := k.TobinTax.Set(ctx, item.Denom, item.TobinTax); err != nil {
			return err
		}

		// Register meta data to bank module
		if _, ok := k.bankKeeper.GetDenomMetaData(ctx, item.Denom); !ok {
			base := item.Denom
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

	return nil
}

// SyncTobinTaxes replaces stored Tobin taxes only when params differ.
func (k Keeper) SyncTobinTaxes(ctx context.Context, stored map[string]math.LegacyDec, tobinTaxes types.TobinTaxes) error {
	if len(stored) != len(tobinTaxes) {
		return k.SetTobinTaxes(ctx, tobinTaxes)
	}

	for _, item := range tobinTaxes {
		tobinTax, ok := stored[item.Denom]
		if !ok || !tobinTax.Equal(item.TobinTax) {
			return k.SetTobinTaxes(ctx, tobinTaxes)
		}
	}

	return nil
}

// These are light wrappers only used for simulation

func (k Keeper) GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error) {
	return k.stakingKeeper.GetAllValidators(ctx)
}

func (k Keeper) Validator(ctx context.Context, address sdk.ValAddress) (stakingtypes.ValidatorI, error) {
	return k.stakingKeeper.Validator(ctx, address)
}

func (k Keeper) ValidatorAddressCodec() address.Codec {
	return k.stakingKeeper.ValidatorAddressCodec()
}
