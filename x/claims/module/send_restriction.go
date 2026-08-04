package claims

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	"ark/x/claims/types"
)

var insuranceAddress = string(authtypes.NewModuleAddress(types.InsuranceName))

// ClaimsSendRestriction permits only positive anoah-only transfers into the
// Insurance custody account. Transfers to every other recipient pass through
// unchanged.
//
// Claims owns this rather than Treasury because the account is Claims' own.
// Insurance has no counterpart to Treasury's stability-tax-collector exemption:
// nothing routes non-NOAH residue here, and a claim is always paid in anoah.
func ClaimsSendRestriction(
	_ context.Context,
	_ sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if string(toAddr) != insuranceAddress {
		return toAddr, nil
	}
	if err := chain.ValidateNoahOnlyDeposit("insurance fund", amount); err != nil {
		return nil, err
	}

	return toAddr, nil
}
