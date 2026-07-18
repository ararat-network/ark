package treasury

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

var treasuryFundAddresses = map[string]struct{}{
	string(authtypes.NewModuleAddress(types.SubsidyPoolName)):      {},
	string(authtypes.NewModuleAddress(types.RedemptionBufferName)): {},
	string(authtypes.NewModuleAddress(types.StrategicReserveName)): {},
	string(authtypes.NewModuleAddress(types.InsuranceName)):        {},
}

// TreasurySendRestriction permits only positive unoah-only transfers into a
// Treasury custody account. Transfers to every other recipient pass through
// unchanged.
func TreasurySendRestriction(
	_ context.Context,
	_ sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if _, isTreasuryFund := treasuryFundAddresses[string(toAddr)]; !isTreasuryFund {
		return toAddr, nil
	}

	if len(amount) != 1 ||
		amount[0].Denom != chain.MicroNoahDenom ||
		amount[0].Amount.IsNil() ||
		!amount[0].Amount.IsPositive() {
		return nil, errorsmod.Wrapf(
			errortypes.ErrInvalidCoins,
			"treasury fund deposits must contain exactly one positive %s coin",
			chain.MicroNoahDenom,
		)
	}

	return toAddr, nil
}
