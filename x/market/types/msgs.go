package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgSwap{}
	_ sdk.Msg = &MsgSwapSend{}
	_ sdk.Msg = &MsgUpdateParams{}
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
