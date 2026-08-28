package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// SendRestriction permits only positive anoah-only transfers into a Treasury
// custody account, with no sender exemptions. Transfers to every other
// recipient pass through unchanged.
//
// Insurance and the strategic Reserve are absent from the guarded set: each
// belongs to the module whose committee operates it, and each registers its
// own restriction.
//
// It is a keeper method for the same reason the Reserve's is, though the
// dependency is weaker: the guarded addresses come from the constructor's
// FundAccountNames() walk rather than from a second list derived by name in a
// package variable. Deriving them by name cannot be checked — an unregistered
// or misspelled name still yields a well-formed address, one that matches no
// account and silently guards nothing — whereas the account keeper answers nil
// and the constructor panics at startup.
func (k Keeper) SendRestriction(
	_ context.Context,
	_ sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if _, isTreasuryFund := k.fundAddresses[string(toAddr)]; !isTreasuryFund {
		return toAddr, nil
	}
	if err := chain.ValidateNoahOnlyDeposit("treasury fund", amount); err != nil {
		return nil, err
	}

	return toAddr, nil
}
