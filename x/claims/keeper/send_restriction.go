package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// SendRestriction permits only positive anoah-only transfers into the Insurance
// custody account. Transfers to every other recipient pass through unchanged.
//
// Claims owns this rather than Treasury because the account is Claims' own.
// Insurance has no counterpart to Treasury's transfer-tax-collector exemption:
// nothing routes non-NOAH residue here, and a claim is always paid in anoah.
//
// It is a keeper method so the guarded address is the one the constructor
// resolved and asserted, rather than a second derivation by name that no
// startup check can reach.
func (k Keeper) SendRestriction(
	_ context.Context,
	_ sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if !toAddr.Equals(k.insuranceAddress) {
		return toAddr, nil
	}
	if err := chain.ValidateNoahOnlyDeposit("insurance fund", amount); err != nil {
		return nil, err
	}

	return toAddr, nil
}
