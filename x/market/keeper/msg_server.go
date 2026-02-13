package keeper

import (
	"context"

	marketv1 "noah/api/noah/market/v1"
	"noah/x/market/types"
	oracletypes "noah/x/oracle/types"

	base "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

type msgServer struct {
	k Keeper
	marketv1.UnimplementedMsgServer
}

// NewMsgServerImpl returns an implementation of the market MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k Keeper) marketv1.MsgServer {
	return msgServer{k: k}
}

var _ marketv1.MsgServer = msgServer{}

func (m msgServer) Swap(ctx context.Context, msg *marketv1.MsgSwap) (*marketv1.MsgSwapResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid trader address: %s", err)
	}

	amt, ok := math.NewIntFromString(msg.OfferCoin.Amount)
	if !ok {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "invalid offer coin amount: %s", msg.OfferCoin.Amount)
	}
	offerCoin := sdk.NewCoin(msg.OfferCoin.Denom, amt)
	if offerCoin.Amount.LTE(math.ZeroInt()) || offerCoin.Amount.BigInt().BitLen() > 100 {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}

	if offerCoin.Denom == msg.AskDenom {
		return nil, sdkerrors.Wrap(types.ErrRecursiveSwap, msg.AskDenom)
	}

	return m.handleSwapRequest(ctx, addr, addr, offerCoin, msg.AskDenom)
}

func (m msgServer) SwapSend(ctx context.Context, msg *marketv1.MsgSwapSend) (*marketv1.MsgSwapSendResponse, error) {
	fromAddr, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid from address: %s", err)
	}

	toAddr, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid to address: %s", err)
	}

	amt, ok := math.NewIntFromString(msg.OfferCoin.Amount)
	if !ok {
		return nil, sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "invalid offer coin amount: %s", msg.OfferCoin.Amount)
	}
	offerCoin := sdk.NewCoin(msg.OfferCoin.Denom, amt)
	if offerCoin.Amount.LTE(math.ZeroInt()) || offerCoin.Amount.BigInt().BitLen() > 100 {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}

	if offerCoin.Denom == msg.AskDenom {
		return nil, sdkerrors.Wrap(types.ErrRecursiveSwap, msg.AskDenom)
	}

	res, err := m.handleSwapRequest(ctx, fromAddr, toAddr, offerCoin, msg.AskDenom)
	if err != nil {
		return nil, err
	}

	return &marketv1.MsgSwapSendResponse{
		SwapCoin: res.SwapCoin,
		SwapFee:  res.SwapFee,
	}, nil
}

// handleMsgSwap handles the logic of a MsgSwap
// This function does not repeat checks that have already been performed in msg.ValidateBasic()
// Ex) assert(offerCoin.Denom != askDenom)
func (m msgServer) handleSwapRequest(
	ctx context.Context,
	trader sdk.AccAddress,
	receiver sdk.AccAddress,
	offerCoin sdk.Coin,
	askDenom string,
) (*marketv1.MsgSwapResponse, error) {
	// Compute exchange rates between the ask and offer
	swapDecCoin, spread, err := m.k.ComputeSwap(ctx, offerCoin, askDenom)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to compute swap")
	}

	// Charge a spread if applicable; the spread is burned
	var feeDecCoin sdk.DecCoin
	if spread.IsPositive() {
		feeDecCoin = sdk.NewDecCoinFromDec(swapDecCoin.Denom, spread.Mul(swapDecCoin.Amount))
	} else {
		feeDecCoin = sdk.NewDecCoin(swapDecCoin.Denom, math.ZeroInt())
	}

	// Subtract fee from the swap coin
	swapDecCoin.Amount = swapDecCoin.Amount.Sub(feeDecCoin.Amount)

	// Update pool delta
	err = m.k.ApplySwapToPool(ctx, offerCoin, swapDecCoin)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to apply swap to pool")
	}

	// Send offer coins to module account
	offerCoins := sdk.NewCoins(offerCoin)
	err = m.k.BankKeeper.SendCoinsFromAccountToModule(ctx, trader, types.ModuleName, offerCoins)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to send offer coins to module")
	}

	// Burn offered coins and subtract from the trader's account
	err = m.k.BankKeeper.BurnCoins(ctx, types.ModuleName, offerCoins)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to burn offer coins")
	}

	// Mint asked coins and credit Trader's account
	swapCoin, decimalCoin := swapDecCoin.TruncateDecimal()

	// Ensure to fail the swap tx when zero swap coin
	if !swapCoin.IsPositive() {
		return nil, types.ErrZeroSwapCoin
	}

	feeDecCoin = feeDecCoin.Add(decimalCoin) // add truncated decimalCoin to swapFee
	feeCoin, _ := feeDecCoin.TruncateDecimal()

	mintCoins := sdk.NewCoins(swapCoin.Add(feeCoin))
	err = m.k.BankKeeper.MintCoins(ctx, types.ModuleName, mintCoins)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to mint swap coins")
	}

	// Send swap coin to the trader
	swapCoins := sdk.NewCoins(swapCoin)
	err = m.k.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, receiver, swapCoins)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to send swap coins to receiver")
	}

	// Send swap fee to oracle account
	if feeCoin.IsPositive() {
		feeCoins := sdk.NewCoins(feeCoin)
		err = m.k.BankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, oracletypes.ModuleName, feeCoins)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "failed to send swap fee to oracle")
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventSwap,
			sdk.NewAttribute(types.AttributeKeyOffer, offerCoin.String()),
			sdk.NewAttribute(types.AttributeKeyTrader, trader.String()),
			sdk.NewAttribute(types.AttributeKeyRecipient, receiver.String()),
			sdk.NewAttribute(types.AttributeKeySwapCoin, swapCoin.String()),
			sdk.NewAttribute(types.AttributeKeySwapFee, feeCoin.String()),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	})

	return &marketv1.MsgSwapResponse{
		SwapCoin: &base.Coin{
			Denom:  swapCoin.Denom,
			Amount: swapCoin.Amount.String(),
		},
		SwapFee: &base.Coin{
			Denom:  feeCoin.Denom,
			Amount: feeCoin.Amount.String(),
		},
	}, nil
}
