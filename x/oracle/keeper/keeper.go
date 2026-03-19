package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

// Keeper of the oracle store
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string
	distrName    string

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	distrKeeper   types.DistributionKeeper
	stakingKeeper types.StakingKeeper

	Schema                       collections.Schema
	Params                       collections.Item[types.Params]
	FeederDelegation             collections.Map[sdk.ValAddress, sdk.AccAddress]
	ExchangeRate                 collections.Map[string, math.LegacyDec]
	MissCounter                  collections.Map[sdk.ValAddress, uint64]
	AggregateExchangeRatePrevote collections.Map[sdk.ValAddress, types.AggregateExchangeRatePrevote]
	AggregateExchangeRateVote    collections.Map[sdk.ValAddress, types.AggregateExchangeRateVote]
	TobinTax                     collections.Map[string, math.LegacyDec]
}

// NewKeeper constructs a new keeper for oracle
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	distrName string,
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
		cdc:           cdc,
		storeService:  storeService,
		authority:     authority,
		distrName:     distrName,
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		distrKeeper:   distrKeeper,
		stakingKeeper: stakingKeeper,
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
		MissCounter: collections.NewMap(
			sb,
			types.MissCounterKey,
			"miss_counter",
			sdk.ValAddressKey,
			collections.Uint64Value,
		),
		AggregateExchangeRatePrevote: collections.NewMap(
			sb,
			types.AggregateExchangeRatePrevoteKey,
			"aggregate_exchange_rate_prevote",
			sdk.ValAddressKey,
			codec.CollValue[types.AggregateExchangeRatePrevote](cdc),
		),
		AggregateExchangeRateVote: collections.NewMap(
			sb,
			types.AggregateExchangeRateVoteKey,
			"aggregate_exchange_rate_vote",
			sdk.ValAddressKey,
			codec.CollValue[types.AggregateExchangeRateVote](cdc),
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
		return nil, fmt.Errorf("getting feeder delegation: %w", err)
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
		return math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
	}

	return exchangeRate, nil
}

// SetExchangeRateWithEvent sets the consensus exchange rate of Ark
// denominated in the denom asset to the store with ABCI event
func (k Keeper) SetExchangeRateWithEvent(ctx context.Context, denom string, exchangeRate math.LegacyDec) error {
	if err := k.ExchangeRate.Set(ctx, denom, exchangeRate); err != nil {
		return sdkerrors.Wrap(errortypes.ErrIO, err.Error())
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
			return err
		}
		if !delegate.Equals(feederAddr) {
			return sdkerrors.Wrap(types.ErrNoVotingPermission, feederAddr.String())
		}
	}

	// Check that the given validator exists
	if val := k.stakingKeeper.Validator(ctx, validatorAddr); val == nil || !val.IsBonded() {
		return sdkerrors.Wrapf(stakingtypes.ErrNoValidatorFound, "validator %s is not active set", validatorAddr.String())
	}

	return nil
}

// These are light wrappers only used for simulation

func (k Keeper) GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error) {
	return k.stakingKeeper.GetAllValidators(ctx)
}

func (k Keeper) Validator(ctx context.Context, address sdk.ValAddress) stakingtypes.ValidatorI {
	return k.stakingKeeper.Validator(ctx, address)
}

func (k Keeper) ValidatorAddressCodec() address.Codec {
	return k.stakingKeeper.ValidatorAddressCodec()
}
