package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// SendRestriction permits positive NOAH or registered-asset deposits to the verified Insurance
// custody address, from any sender: a stablecoin claim's buy lands there, and a deposit in a
// registered asset waits uncommitted for the next claim in it. Other recipients pass unchanged.
// Insurance has no transfer-tax-collector exemption.
func (k *Keeper) SendRestriction(ctx context.Context, _ sdk.AccAddress, toAddr sdk.AccAddress, amount sdk.Coins) (sdk.AccAddress, error) {
	if !toAddr.Equals(k.insuranceAddress) {
		return toAddr, nil
	}
	if err := chain.ValidateRegisteredDeposit(ctx, "insurance fund", amount, k.assetKeeper.HasAsset); err != nil {
		return nil, err
	}

	return toAddr, nil
}
