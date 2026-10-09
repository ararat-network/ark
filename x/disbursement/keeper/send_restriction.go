package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// SendRestriction admits positive NOAH into the member account and positive NOAH or a registered
// asset into the contributor account, on every Bank path; other recipients pass unchanged. Grants
// admit nothing else, so nothing else may wait in custody.
func (k *Keeper) SendRestriction(ctx context.Context, _, toAddr sdk.AccAddress, amount sdk.Coins) (sdk.AccAddress, error) {
	account, isPool := k.poolAddresses[string(toAddr)]
	if !isPool {
		return toAddr, nil
	}
	if account == types.MembersName {
		if err := chain.ValidateNoahOnlyDeposit("member pool", amount); err != nil {
			return nil, err
		}
		return toAddr, nil
	}
	if err := chain.ValidateRegisteredDeposit(ctx, "contributor", amount, k.assetKeeper.HasAsset); err != nil {
		return nil, err
	}
	return toAddr, nil
}
