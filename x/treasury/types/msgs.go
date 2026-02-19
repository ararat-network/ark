package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgAddBurnTaxExemptionAddress{}
	_ sdk.Msg = &MsgRemoveBurnTaxExemptionAddress{}
	_ sdk.Msg = &MsgUpdateParams{}
)

// NewMsgSwap creates a MsgSwap instance
func NewMsgAddBurnTaxExemptionAddress(authority string, addresses []string) *MsgAddBurnTaxExemptionAddress {
	return &MsgAddBurnTaxExemptionAddress{
		Authority: authority,
		Addresses: addresses,
	}
}

// NewMsgSwapSend creates a MsgSwapSend instance
func NewMsgRemoveBurnTaxExemptionAddress(authority string, addresses []string) *MsgRemoveBurnTaxExemptionAddress {
	return &MsgRemoveBurnTaxExemptionAddress{
		Authority: authority,
		Addresses: addresses,
	}
}
