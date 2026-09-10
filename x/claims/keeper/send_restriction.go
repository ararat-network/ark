package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// SendRestriction permits positive NOAH-only deposits to the verified Insurance custody address;
// other recipients pass unchanged. Insurance has no transfer-tax-collector exemption.
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
