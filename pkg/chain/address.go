package chain

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ParseCanonicalAccountAddress parses one canonical bech32 account address.
func ParseCanonicalAccountAddress(field, value string) (sdk.AccAddress, error) {
	address, err := sdk.AccAddressFromBech32(value)
	if err != nil {
		return nil, fmt.Errorf("%s is invalid: %w", field, err)
	}
	if address.String() != value {
		return nil, fmt.Errorf("%s must be a canonical account address", field)
	}
	return address, nil
}

// ParseCanonicalValidatorAddress parses one canonical bech32 validator address.
func ParseCanonicalValidatorAddress(field, value string) (sdk.ValAddress, error) {
	address, err := sdk.ValAddressFromBech32(value)
	if err != nil {
		return nil, fmt.Errorf("%s is invalid: %w", field, err)
	}
	if address.String() != value {
		return nil, fmt.Errorf("%s must be a canonical validator address", field)
	}
	return address, nil
}

// CanonicaliseAccountAddress returns value in its canonical spelling, accepting
// any letter case bech32 allows. Transaction inputs reaching state pass through
// here, so state holds only this spelling — which is what keeps the canonical
// checks above true of everything already at rest. Callers that only need the
// decoded bytes parse directly instead; there is nothing to canonicalise about
// an address that is never rendered back to a string.
func CanonicaliseAccountAddress(field, value string) (string, error) {
	address, err := sdk.AccAddressFromBech32(value)
	if err != nil {
		return "", fmt.Errorf("%s is invalid: %w", field, err)
	}
	return address.String(), nil
}
