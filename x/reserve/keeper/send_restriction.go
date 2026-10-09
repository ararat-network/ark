package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// SendRestriction admits positive NOAH or registered paper into the verified Reserve custody
// account. Other recipients pass through; no sender has privileged admission.
func (k *Keeper) SendRestriction(
	ctx context.Context,
	_ sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if !toAddr.Equals(k.reserveAddress) {
		return toAddr, nil
	}

	if err := chain.ValidateRegisteredDeposit(ctx, "strategic reserve", amount, k.assetKeeper.HasAsset); err != nil {
		return nil, err
	}

	return toAddr, nil
}
