package treasury

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

// treasuryFundAddresses are the custody accounts Treasury itself operates.
// Insurance and the strategic Reserve are absent: each belongs to the module
// whose committee operates it, and each registers its own restriction.
var treasuryFundAddresses = map[string]struct{}{
	string(authtypes.NewModuleAddress(types.SubsidyPoolName)):      {},
	string(authtypes.NewModuleAddress(types.RedemptionBufferName)): {},
}

// TreasurySendRestriction permits only positive anoah-only transfers into a
// Treasury custody account. Transfers to every other recipient pass through
// unchanged.
//
// There is no exemption here. The stability-tax-collector carve-out that once
// lived in this function guarded the strategic Reserve, and moved to
// x/reserve with the account; Treasury's settlement code still routes
// derecognized tax there, but the rule admitting it now belongs to the module
// that owns the recipient.
func TreasurySendRestriction(
	_ context.Context,
	_ sdk.AccAddress,
	toAddr sdk.AccAddress,
	amount sdk.Coins,
) (sdk.AccAddress, error) {
	if _, isTreasuryFund := treasuryFundAddresses[string(toAddr)]; !isTreasuryFund {
		return toAddr, nil
	}
	if err := chain.ValidateNoahOnlyDeposit("treasury fund", amount); err != nil {
		return nil, err
	}

	return toAddr, nil
}
