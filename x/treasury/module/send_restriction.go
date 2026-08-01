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

var (
	stabilityTaxCollectorAddress = string(authtypes.NewModuleAddress(types.StabilityTaxCollectorName))
	strategicReserveAddress      = string(authtypes.NewModuleAddress(types.StrategicReserveName))
)

// TreasurySendRestriction permits only positive anoah-only transfers into a
// Treasury custody account. Transfers to every other recipient pass through
// unchanged.
//
// The one exempt pair is stability_tax_collector to treasury_strategic_reserve:
// settlement routes derecognized (written-off or retired) stability tax there,
// and that residue is by definition not anoah. The exemption is safe because
// the collector is a blocked account whose outflows are Treasury settlement
// code alone, and it changes nothing the Reserve's consensus paths can see:
// targets and the Reserve-to-Buffer commitment read only the anoah balance, so
// routed residue is inert custody awaiting a separate governed disposal.
func TreasurySendRestriction(
	_ context.Context,
	fromAddr sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if _, isTreasuryFund := treasuryFundAddresses[string(toAddr)]; !isTreasuryFund {
		return toAddr, nil
	}
	if string(fromAddr) == stabilityTaxCollectorAddress && string(toAddr) == strategicReserveAddress {
		return toAddr, nil
	}

	if len(amount) != 1 ||
		amount[0].Denom != chain.NoahBaseDenom ||
		amount[0].Amount.IsNil() ||
		!amount[0].Amount.IsPositive() {
		return nil, errorsmod.Wrapf(
			errortypes.ErrInvalidCoins,
			"treasury fund deposits must contain exactly one positive %s coin",
			chain.NoahBaseDenom,
		)
	}

	return toAddr, nil
}
