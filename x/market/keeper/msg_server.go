package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"noah/x/market/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns an implementation of the market MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// Swap validates the trader address and executes a swap back to the same account.
func (m msgServer) Swap(ctx context.Context, msg *types.MsgSwap) (*types.MsgSwapResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, errors.Wrapf(errortypes.ErrInvalidAddress, "invalid trader address (%s)", err)
	}

	if err := validateInputs(msg.OfferCoin, msg.AskDenom); err != nil {
		return nil, err
	}

	// Compute exchange rates between the ask and offer
	swapDecCoin, spread, err := m.k.ComputeSwap(ctx, msg.OfferCoin, msg.AskDenom)
	if err != nil {
		return nil, errors.Wrap(err, "failed to compute swap")
	}

	outcome, err := buildSwapOutcome(swapDecCoin, spread)
	if err != nil {
		return nil, err
	}

	if err := m.settleSwap(ctx, addr, addr, msg.OfferCoin, outcome); err != nil {
		return nil, err
	}

	return &types.MsgSwapResponse{
		SwapCoin: outcome.swapCoin,
		SwapFee:  outcome.swapFee,
	}, nil
}

// SwapSend validates the sender and recipient addresses and settles the swap to the recipient.
func (m msgServer) SwapSend(ctx context.Context, msg *types.MsgSwapSend) (*types.MsgSwapSendResponse, error) {
	fromAddr, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, errors.Wrapf(errortypes.ErrInvalidAddress, "invalid from address (%s)", err)
	}
	toAddr, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, errors.Wrapf(errortypes.ErrInvalidAddress, "invalid to address (%s)", err)
	}

	if err := validateInputs(msg.OfferCoin, msg.AskDenom); err != nil {
		return nil, err
	}

	// Compute exchange rates between the ask and offer
	swapDecCoin, spread, err := m.k.ComputeSwap(ctx, msg.OfferCoin, msg.AskDenom)
	if err != nil {
		return nil, errors.Wrap(err, "failed to compute swap")
	}

	outcome, err := buildSwapOutcome(swapDecCoin, spread)
	if err != nil {
		return nil, err
	}

	if err := m.settleSwap(ctx, fromAddr, toAddr, msg.OfferCoin, outcome); err != nil {
		return nil, err
	}
	return &types.MsgSwapSendResponse{
		SwapCoin: outcome.swapCoin,
		SwapFee:  outcome.swapFee,
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

// validateInputs enforces the local swap input bounds before any pricing or state changes.
func validateInputs(offerCoin sdk.Coin, askDenom string) error {
	if offerCoin.Amount.LTE(math.ZeroInt()) || offerCoin.Amount.BigInt().BitLen() > 100 {
		return errors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}

	if offerCoin.Denom == askDenom {
		return errors.Wrap(types.ErrRecursiveSwap, askDenom)
	}

	return nil
}

type swapOutcome struct {
	swapDecCoin sdk.DecCoin
	swapCoin    sdk.Coin
	swapFee     sdk.DecCoin
}

// buildSwapOutcome applies spread, truncates the swap result, and returns the final coin and fee.
func buildSwapOutcome(swapDecCoin sdk.DecCoin, spread math.LegacyDec) (*swapOutcome, error) {
	var swapFee sdk.DecCoin
	if spread.IsPositive() {
		swapFee = sdk.NewDecCoinFromDec(swapDecCoin.Denom, spread.Mul(swapDecCoin.Amount))
	} else {
		swapFee = sdk.NewDecCoin(swapDecCoin.Denom, math.ZeroInt())
	}

	swapDecCoin.Amount = swapDecCoin.Amount.Sub(swapFee.Amount)
	swapCoin, decimalCoin := swapDecCoin.TruncateDecimal()
	if !swapCoin.IsPositive() {
		return nil, types.ErrZeroSwapCoin
	}

	swapFee = swapFee.Add(decimalCoin)

	return &swapOutcome{
		swapDecCoin: swapDecCoin,
		swapCoin:    swapCoin,
		swapFee:     swapFee,
	}, nil
}

// settleSwap applies pool changes, moves funds through the module account, and emits swap events.
func (m msgServer) settleSwap(
	ctx context.Context,
	trader sdk.AccAddress,
	receiver sdk.AccAddress,
	offerCoin sdk.Coin,
	outcome *swapOutcome,
) error {
	if err := m.k.ApplySwapToPool(ctx, offerCoin, outcome.swapDecCoin); err != nil {
		return errors.Wrap(err, "failed to apply swap to pool")
	}

	offerCoins := sdk.NewCoins(offerCoin)
	if err := m.k.bankKeeper.SendCoinsFromAccountToModule(ctx, trader, types.ModuleName, offerCoins); err != nil {
		return errors.Wrap(err, "failed to send offer coins to module")
	}

	if err := m.k.bankKeeper.BurnCoins(ctx, types.ModuleName, offerCoins); err != nil {
		return errors.Wrap(err, "failed to burn offer coins")
	}

	swapCoins := sdk.NewCoins(outcome.swapCoin)
	if err := m.k.bankKeeper.MintCoins(ctx, types.ModuleName, swapCoins); err != nil {
		return errors.Wrap(err, "failed to mint swap coins")
	}

	if err := m.k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, receiver, swapCoins); err != nil {
		return errors.Wrap(err, "failed to send swap coins to receiver")
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventSwap,
			sdk.NewAttribute(types.AttributeKeyOffer, offerCoin.String()),
			sdk.NewAttribute(types.AttributeKeyTrader, trader.String()),
			sdk.NewAttribute(types.AttributeKeyRecipient, receiver.String()),
			sdk.NewAttribute(types.AttributeKeySwapCoin, outcome.swapCoin.String()),
			sdk.NewAttribute(types.AttributeKeySwapFee, outcome.swapFee.String()),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	})

	return nil
}
