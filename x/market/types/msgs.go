package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewMsgSwap creates a MsgSwap instance
func NewMsgSwap(traderAddress sdk.AccAddress, offerCoin sdk.Coin, askDenom string) *MsgSwap {
	return &MsgSwap{
		Trader:    traderAddress.String(),
		OfferCoin: offerCoin,
		AskDenom:  askDenom,
	}
}

// NewMsgSwapSend creates a MsgSwapSend instance
func NewMsgSwapSend(fromAddress sdk.AccAddress, toAddress sdk.AccAddress, offerCoin sdk.Coin, askDenom string) *MsgSwapSend {
	return &MsgSwapSend{
		FromAddress: fromAddress.String(),
		ToAddress:   toAddress.String(),
		OfferCoin:   offerCoin,
		AskDenom:    askDenom,
	}
}

// ValidateBasic implements sdk.Msg
func (msg MsgSwap) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid trader address (%s)", err)
	}

	if msg.OfferCoin.Amount.LTE(math.ZeroInt()) || msg.OfferCoin.Amount.BigInt().BitLen() > 100 {
		return errorsmod.Wrap(errortypes.ErrInvalidCoins, msg.OfferCoin.String())
	}

	if msg.OfferCoin.Denom == msg.AskDenom {
		return errorsmod.Wrap(ErrRecursiveSwap, msg.AskDenom)
	}

	return nil
}

// ValidateBasic implements sdk.Msg
func (msg MsgSwapSend) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid from address (%s)", err)
	}

	_, err = sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid to address (%s)", err)
	}

	if msg.OfferCoin.Amount.LTE(math.ZeroInt()) || msg.OfferCoin.Amount.BigInt().BitLen() > 100 {
		return errorsmod.Wrap(errortypes.ErrInvalidCoins, msg.OfferCoin.String())
	}

	if msg.OfferCoin.Denom == msg.AskDenom {
		return errorsmod.Wrap(ErrRecursiveSwap, msg.AskDenom)
	}

	return nil
}
