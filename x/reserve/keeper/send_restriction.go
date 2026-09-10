package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

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
