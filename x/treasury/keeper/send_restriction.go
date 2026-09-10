package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// SendRestriction guards verified Treasury fund addresses with positive NOAH-only deposits and no
// sender exemptions. Other recipients pass unchanged; Claims and Reserve own their separate custody
// guards.
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
