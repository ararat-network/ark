package keeper

import (
	"context"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"noah/x/oracle/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns an implementation of the oracle MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{k: k}
}

// Prevote submits a hashed exchange rate vote for the current period
func (m msgServer) Prevote(ctx context.Context, msg *types.MsgPrevote) (*types.MsgPrevoteResponse, error) {
	valAddr, err := sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return nil, err
	}

	feederAddr, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return nil, err
	}

	if err := m.k.ValidateFeeder(ctx, feederAddr, valAddr); err != nil {
		return nil, err
	}

	// Convert hex string to votehash
	voteHash, err := types.VoteHashFromHexString(msg.Hash)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidHash, err.Error())
	}

	// HEX encoding doubles the hash length
	if len(msg.Hash) != types.TruncatedHashSize*2 {
		return nil, types.ErrInvalidHashLength
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	aggregatePrevote := types.NewPrevote(voteHash, valAddr, uint64(sdkCtx.BlockHeight()))
	if err := m.k.Prevote.Set(ctx, valAddr, aggregatePrevote); err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrIO, err.Error())
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypePrevote,
			sdk.NewAttribute(types.AttributeKeyVoter, msg.Validator),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
			sdk.NewAttribute(sdk.AttributeKeySender, msg.Feeder),
		),
	})

	return &types.MsgPrevoteResponse{}, nil
}

// Vote reveals exchange rates for a previously submitted prevote
func (m msgServer) Vote(ctx context.Context, msg *types.MsgVote) (*types.MsgVoteResponse, error) {
	valAddr, err := sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return nil, err
	}
	feederAddr, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return nil, err
	}
	if err := m.k.ValidateFeeder(ctx, feederAddr, valAddr); err != nil {
		return nil, err
	}

	// Validate ExchangeRates
	if l := len(msg.ExchangeRates); l == 0 {
		return nil, sdkerrors.Wrap(errortypes.ErrUnknownRequest, "must provide at least one oracle exchange rate")
	} else if l > 4096 {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidRequest, "exchange rates string can not exceed 4096 characters")
	}

	// Validate Salt
	if len(msg.Salt) > 4 || len(msg.Salt) < 1 {
		return nil, sdkerrors.Wrap(types.ErrInvalidSaltLength, "salt length must be [1, 4]")
	}

	params, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrIO, err.Error())
	}

	aggregatePrevote, err := m.k.Prevote.Get(ctx, valAddr)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// Check a msg is submitted proper period
	if (uint64(sdkCtx.BlockHeight())/params.VotePeriod)-(aggregatePrevote.SubmitBlock/params.VotePeriod) != 1 {
		return nil, types.ErrRevealPeriodMissMatch
	}

	exchangeRateTuples, err := types.ParseExchangeRates(msg.ExchangeRates)
	if err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, err.Error())
	}
	// check all denoms are in the vote target
	for _, tuple := range exchangeRateTuples {
		// Check overflow bit length
		if tuple.Rate.BigInt().BitLen() > 255+math.LegacyDecimalPrecisionBits {
			return nil, sdkerrors.Wrap(types.ErrInvalidExchangeRate, "overflow")
		}
		if has, err := m.k.TobinTax.Has(ctx, tuple.Denom); err != nil {
			return nil, sdkerrors.Wrap(errortypes.ErrIO, err.Error())
		} else if !has {
			return nil, sdkerrors.Wrap(types.ErrUnknownDenom, tuple.Denom)
		}
	}

	// Verify an exchange rate with aggregate prevote hash
	hash := types.GetVoteHash(msg.Salt, msg.ExchangeRates, valAddr)
	if aggregatePrevote.Hash != hash.String() {
		return nil, sdkerrors.Wrapf(types.ErrVerificationFailed, "must be given %s not %s", aggregatePrevote.Hash, hash)
	}

	// Move aggregate prevote to aggregate vote with given exchange rates
	if err := m.k.Vote.Set(ctx, valAddr, types.NewVote(exchangeRateTuples, valAddr)); err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrIO, err.Error())
	}
	if err := m.k.Prevote.Remove(ctx, valAddr); err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrIO, err.Error())
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeVote,
			sdk.NewAttribute(types.AttributeKeyVoter, msg.Validator),
			sdk.NewAttribute(types.AttributeKeyExchangeRates, msg.ExchangeRates),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
			sdk.NewAttribute(sdk.AttributeKeySender, msg.Feeder),
		),
	})

	return &types.MsgVoteResponse{}, nil
}

// DelegateFeedConsent authorises another address to submit oracle votes on behalf of a validator
func (m msgServer) DelegateFeedConsent(ctx context.Context, msg *types.MsgDelegateFeedConsent) (*types.MsgDelegateFeedConsentResponse, error) {
	operatorAddr, err := sdk.ValAddressFromBech32(msg.Operator)
	if err != nil {
		return nil, err
	}
	delegateAddr, err := sdk.AccAddressFromBech32(msg.Delegate)
	if err != nil {
		return nil, err
	}

	// Check the delegator is a validator
	val := m.k.stakingKeeper.Validator(ctx, operatorAddr)
	if val == nil {
		return nil, sdkerrors.Wrap(stakingtypes.ErrNoValidatorFound, msg.Operator)
	}

	// Set the delegation
	if err := m.k.FeederDelegation.Set(ctx, operatorAddr, delegateAddr); err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrIO, err.Error())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeFeedDelegate,
			sdk.NewAttribute(types.AttributeKeyFeeder, msg.Delegate),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
			sdk.NewAttribute(sdk.AttributeKeySender, msg.Operator),
		),
	})

	return &types.MsgDelegateFeedConsentResponse{}, nil
}

// UpdateParams updates the params.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if m.k.authority != msg.Authority {
		return nil, sdkerrors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", m.k.authority, msg.Authority)
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
