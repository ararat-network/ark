package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/pkg/decimal"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	types.UnimplementedMsgServer

	k *Keeper
}

// NewMsgServerImpl returns an implementation of the market MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// UpdateParams updates the params.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	currentParams, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting current market params: %w", err)
	}
	oldDelta, err := m.k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting ark pool delta: %w", err)
	}

	appliedParams := msg.Params
	if msg.Params.BasePool.Denom != currentParams.BasePool.Denom {
		if _, err := m.k.oracleKeeper.GetTobinTax(ctx, msg.Params.BasePool.Denom); err != nil {
			if errors.Is(err, oracletypes.ErrUnknownDenom) {
				return nil, sdkerrors.Wrapf(
					errortypes.ErrInvalidRequest,
					"base pool denom %s is not configured in oracle: %v",
					msg.Params.BasePool.Denom,
					err,
				)
			}
			return nil, fmt.Errorf("checking base pool denom %s in oracle: %w", msg.Params.BasePool.Denom, err)
		}
		rates, err := m.k.oracleKeeper.GetRateSet(
			ctx,
			currentParams.BasePool.Denom,
			msg.Params.BasePool.Denom,
		)
		if err != nil {
			return nil, marketRateError(err)
		}
		appliedParams.BasePool, err = rates.Convert(currentParams.BasePool, msg.Params.BasePool.Denom)
		if err != nil {
			return nil, err
		}
	}

	newDelta := oldDelta
	if !appliedParams.BasePool.Amount.Equal(currentParams.BasePool.Amount) {
		scaledDelta, err := decimal.Mul(oldDelta, appliedParams.BasePool.Amount)
		if err != nil {
			return nil, sdkerrors.Wrapf(
				types.ErrArithmeticOutOfRange,
				"rescaling ark pool delta: %v",
				err,
			)
		}
		newDelta, err = decimal.Quo(scaledDelta, currentParams.BasePool.Amount)
		if err != nil {
			return nil, sdkerrors.Wrapf(
				types.ErrArithmeticOutOfRange,
				"rescaling ark pool delta: %v",
				err,
			)
		}
	}
	if _, err := types.NewEffectivePools(appliedParams.BasePool.Amount, newDelta); err != nil {
		return nil, sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"invalid effective pools: %v",
			err,
		)
	}

	if err := m.k.Params.Set(ctx, appliedParams); err != nil {
		return nil, err
	}
	if err := m.k.ArkPoolDelta.Set(ctx, newDelta); err != nil {
		return nil, err
	}

	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventPoolUpdated{
		OldBasePoolDenom:  currentParams.BasePool.Denom,
		OldBasePoolAmount: currentParams.BasePool.Amount,
		NewBasePoolDenom:  appliedParams.BasePool.Denom,
		NewBasePoolAmount: appliedParams.BasePool.Amount,
		OldArkPoolDelta:   oldDelta,
		NewArkPoolDelta:   newDelta,
	}); err != nil {
		return nil, fmt.Errorf("emitting Market pool update event: %w", err)
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// Swap validates the trader address and executes a swap back to the same account.
func (m msgServer) Swap(ctx context.Context, msg *types.MsgSwap) (*types.MsgSwapResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid trader address (%s)", err)
	}

	if err := validateMinimumReceive(msg.MinimumReceive, msg.AskDenom); err != nil {
		return nil, err
	}

	// Compute exchange rates between the ask and offer
	quote, err := m.k.quoteSwap(ctx, msg.OfferCoin, msg.AskDenom)
	if err != nil {
		return nil, sdkerrors.Wrapf(err, "computing swap from %s to %s", msg.OfferCoin, msg.AskDenom)
	}

	if quote.swapCoin.Amount.LT(msg.MinimumReceive.Amount) {
		return nil, sdkerrors.Wrapf(
			types.ErrMinimumReceiveNotMet,
			"minimum %s, received %s",
			msg.MinimumReceive,
			quote.swapCoin,
		)
	}

	if err := m.k.settleSwap(ctx, addr, addr, msg.OfferCoin, quote); err != nil {
		return nil, err
	}

	return &types.MsgSwapResponse{
		SwapCoin: quote.swapCoin,
		SwapFee:  quote.swapFee,
	}, nil
}

// SwapSend validates the sender and recipient addresses and settles the swap to the recipient.
func (m msgServer) SwapSend(ctx context.Context, msg *types.MsgSwapSend) (*types.MsgSwapSendResponse, error) {
	fromAddr, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid from address (%s)", err)
	}
	toAddr, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid to address (%s)", err)
	}

	if err := validateMinimumReceive(msg.MinimumReceive, msg.AskDenom); err != nil {
		return nil, err
	}

	// Compute exchange rates between the ask and offer
	quote, err := m.k.quoteSwap(ctx, msg.OfferCoin, msg.AskDenom)
	if err != nil {
		return nil, sdkerrors.Wrapf(err, "computing swap from %s to %s", msg.OfferCoin, msg.AskDenom)
	}

	if quote.swapCoin.Amount.LT(msg.MinimumReceive.Amount) {
		return nil, sdkerrors.Wrapf(
			types.ErrMinimumReceiveNotMet,
			"minimum %s, received %s",
			msg.MinimumReceive,
			quote.swapCoin,
		)
	}

	if err := m.k.settleSwap(ctx, fromAddr, toAddr, msg.OfferCoin, quote); err != nil {
		return nil, err
	}
	return &types.MsgSwapSendResponse{
		SwapCoin: quote.swapCoin,
		SwapFee:  quote.swapFee,
	}, nil
}

func validateMinimumReceive(minimumReceive sdk.Coin, askDenom string) error {
	if err := minimumReceive.Validate(); err != nil {
		return sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "invalid minimum receive: %v", err)
	}
	if !minimumReceive.IsPositive() {
		return sdkerrors.Wrap(errortypes.ErrInvalidCoins, minimumReceive.String())
	}
	if minimumReceive.Denom != askDenom {
		return sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"minimum receive denom %q does not match ask denom %q",
			minimumReceive.Denom,
			askDenom,
		)
	}

	return nil
}
