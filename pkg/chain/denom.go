package chain

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	NoahBaseDenom = "anoah"
	USDBaseDenom  = "ausd"
	KRWBaseDenom  = "akrw"
	SDRBaseDenom  = "asdr"
	CNYBaseDenom  = "acny"
	JPYBaseDenom  = "ajpy"
	EURBaseDenom  = "aeur"
	GBPBaseDenom  = "agbp"
	MNTBaseDenom  = "amnt"

	NativeDisplayExponent = 18
)

// NativeBaseAmount converts a whole Ark-native display amount into base units.
func NativeBaseAmount(wholeUnits int64) math.Int {
	return math.NewIntWithDecimal(wholeUnits, NativeDisplayExponent)
}

// ValidateNativeBaseDenom validates denom as a canonical lowercase Ark-native
// base denomination.
func ValidateNativeBaseDenom(denom string) error {
	if len(denom) < 3 || denom[0] != 'a' {
		return fmt.Errorf("denom must be an Ark-native base denom beginning with a: %s", denom)
	}
	if err := sdk.ValidateDenom(denom); err != nil ||
		denom != strings.ToLower(denom) ||
		strings.Contains(denom[1:], "/") {
		return fmt.Errorf(
			"denom must be a canonical lowercase Ark-native base denom without path separators: %s",
			denom,
		)
	}

	return nil
}
