package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/chain"
)

// SendRestriction admits only positive coins of NOAH or an Ark-issued asset
// into the strategic Reserve; transfers to every other recipient pass through
// unchanged. It is a keeper method so the guarded address is the one the
// constructor resolved from the account keeper and asserted. The sender is
// deliberately unused: no sender is privileged, and admission is a question
// about the coins alone.
func (k *Keeper) SendRestriction(
	ctx context.Context,
	_ sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if !toAddr.Equals(k.reserveAddress) {
		return toAddr, nil
	}

	if len(amount) == 0 {
		return nil, errorsmod.Wrap(
			errortypes.ErrInvalidCoins,
			"strategic reserve deposits must contain at least one coin",
		)
	}
	for _, coin := range amount {
		if coin.Amount.IsNil() || !coin.Amount.IsPositive() {
			return nil, errorsmod.Wrapf(
				errortypes.ErrInvalidCoins,
				"strategic reserve deposits must be positive, got %s",
				coin,
			)
		}
		if coin.Denom == chain.NoahBaseDenom {
			continue
		}
		member, err := k.assetKeeper.HasAsset(ctx, coin.Denom)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "checking the asset registry for %s", coin.Denom)
		}
		if !member {
			return nil, errorsmod.Wrapf(
				errortypes.ErrInvalidCoins,
				"strategic reserve deposits must be %s or an Ark-issued asset, got %s",
				chain.NoahBaseDenom,
				coin,
			)
		}
	}

	return toAddr, nil
}
