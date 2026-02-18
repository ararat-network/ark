package keeper

import (
	"context"

	"noah/x/market/types"
	oracletypes "noah/x/oracle/types"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct {
	k *Keeper
	types.UnimplementedMsgServer
}

// NewMsgServerImpl returns an implementation of the market MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

var _ types.MsgServer = msgServer{}

func (m msgServer) Swap(ctx context.Context, msg *types.MsgSwap) (*types.MsgSwapResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, errors.Wrapf(errortypes.ErrInvalidAddress, "invalid trader address (%s)", err)
	}

	return m.handleSwapRequest(ctx, addr, addr, msg.OfferCoin, msg.AskDenom)
}

func (m msgServer) SwapSend(ctx context.Context, msg *types.MsgSwapSend) (*types.MsgSwapSendResponse, error) {
	fromAddr, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, errors.Wrapf(errortypes.ErrInvalidAddress, "invalid from address (%s)", err)
	}
	toAddr, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, errors.Wrapf(errortypes.ErrInvalidAddress, "invalid to address (%s)", err)
	}

	res, err := m.handleSwapRequest(ctx, fromAddr, toAddr, msg.OfferCoin, msg.AskDenom)
	if err != nil {
		return nil, err
	}

	return &types.MsgSwapSendResponse{
		SwapCoin: res.SwapCoin,
		SwapFee:  res.SwapFee,
	}, nil
}

// UpdateParams updates the params.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if m.k.authority != msg.Authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", m.k.authority, msg.Authority)
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// handleSwapRequest handles the logic of a MsgSwap.
// This function does not repeat checks already performed in msg.ValidateBasic().
func (m msgServer) handleSwapRequest(
	ctx context.Context,
	trader sdk.AccAddress,
	receiver sdk.AccAddress,
	offerCoin sdk.Coin,
	askDenom string,
) (*types.MsgSwapResponse, error) {
	// Compute exchange rates between the ask and offer
	swapDecCoin, spread, err := m.k.ComputeSwap(ctx, offerCoin, askDenom)
	if err != nil {
		return nil, errors.Wrap(err, "failed to compute swap")
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
		return nil, errors.Wrap(err, "failed to apply swap to pool")
	}

	// Send offer coins to module account
	offerCoins := sdk.NewCoins(offerCoin)
	err = m.k.BankKeeper.SendCoinsFromAccountToModule(ctx, trader, types.ModuleName, offerCoins)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send offer coins to module")
	}

	// Burn offered coins and subtract from the trader's account
	err = m.k.BankKeeper.BurnCoins(ctx, types.ModuleName, offerCoins)
	if err != nil {
		return nil, errors.Wrap(err, "failed to burn offer coins")
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
		return nil, errors.Wrap(err, "failed to mint swap coins")
	}

	// Send swap coin to the trader
	swapCoins := sdk.NewCoins(swapCoin)
	err = m.k.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, receiver, swapCoins)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send swap coins to receiver")
	}

	// Send swap fee to oracle account
	if feeCoin.IsPositive() {
		feeCoins := sdk.NewCoins(feeCoin)
		err = m.k.BankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, oracletypes.ModuleName, feeCoins)
		if err != nil {
			return nil, errors.Wrap(err, "failed to send swap fee to oracle")
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

	return &types.MsgSwapResponse{
		SwapCoin: swapCoin,
		SwapFee:  feeCoin,
	}, nil
}
