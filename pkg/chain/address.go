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

// CanonicaliseAccountAddress accepts valid Bech32 letter case and returns canonical stored
// spelling. Callers needing only bytes should parse directly.
func CanonicaliseAccountAddress(field, value string) (string, error) {
	address, err := sdk.AccAddressFromBech32(value)
	if err != nil {
		return "", fmt.Errorf("%s is invalid: %w", field, err)
	}
	return address.String(), nil
}
