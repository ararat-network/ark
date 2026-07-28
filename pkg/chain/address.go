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
