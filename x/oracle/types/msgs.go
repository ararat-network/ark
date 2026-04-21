package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ensure Msg interface compliance at compile time
var (
	_ sdk.Msg = &MsgDelegateFeedConsent{}
	_ sdk.Msg = &MsgPrevote{}
	_ sdk.Msg = &MsgVote{}
)

// NewMsgPrevote returns MsgPrevote instance
func NewMsgPrevote(hash VoteHash, feeder sdk.AccAddress, validator sdk.ValAddress) *MsgPrevote {
	return &MsgPrevote{
		Hash:      hash.String(),
		Feeder:    feeder.String(),
		Validator: validator.String(),
	}
}

// NewMsgVote returns MsgVote instance
func NewMsgVote(salt, exchangeRates string, feeder sdk.AccAddress, validator sdk.ValAddress) *MsgVote {
	return &MsgVote{
		Salt:          salt,
		ExchangeRates: exchangeRates,
		Feeder:        feeder.String(),
		Validator:     validator.String(),
	}
}

// NewMsgDelegateFeedConsent creates a MsgDelegateFeedConsent instance
func NewMsgDelegateFeedConsent(operatorAddress sdk.ValAddress, feederAddress sdk.AccAddress) *MsgDelegateFeedConsent {
	return &MsgDelegateFeedConsent{
		Operator: operatorAddress.String(),
		Delegate: feederAddress.String(),
	}
}
