package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// ValidatorScore is an interface that directs its rewards to an attached validator
type ValidatorScore struct {
	Power     uint64
	Weight    uint64
	WinCount  uint64
	Recipient sdk.ConsAddress
}

// NewValidatorScore generates a ValidatorScore instance.
func NewValidatorScore(power, weight, winCount uint64, recipient sdk.ConsAddress) ValidatorScore {
	return ValidatorScore{
		Power:     power,
		Weight:    weight,
		WinCount:  winCount,
		Recipient: recipient,
	}
}
