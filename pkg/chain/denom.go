package chain

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	MicroNoahDenom = "unoah"
	MicroUSDDenom  = "uusd"
	MicroKRWDenom  = "ukrw"
	MicroSDRDenom  = "usdr"
	MicroCNYDenom  = "ucny"
	MicroJPYDenom  = "ujpy"
	MicroEURDenom  = "ueur"
	MicroGBPDenom  = "ugbp"
	MicroMNTDenom  = "umnt"

	MicroUnit = int64(1e6)
)

// ValidateMicroDenom validates denom as a canonical lowercase micro denom.
func ValidateMicroDenom(denom string) error {
	if len(denom) < 3 || denom[0] != 'u' {
		return fmt.Errorf("denom must be a micro denom beginning with u: %s", denom)
	}
	if err := sdk.ValidateDenom(denom); err != nil ||
		denom != strings.ToLower(denom) ||
		strings.Contains(denom[1:], "/") {
		return fmt.Errorf("denom must be a canonical lowercase micro denom without path separators: %s", denom)
	}

	return nil
}
