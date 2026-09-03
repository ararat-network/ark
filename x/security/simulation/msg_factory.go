package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/security/types"
)

// MsgSetSecurityMandateFactory appoints the security committee. The appointee
// is drawn from the simulation's own accounts, so the committee is an address
// the run holds a key for. The handler derives the term itself, and refuses an
// expiry at or below the current height, so the window is anchored ahead of it.
func MsgSetSecurityMandateFactory() simsx.SimMsgFactoryFn[*types.MsgSetSecurityMandate] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetSecurityMandate) {
		authority := testData.ModuleAccountAddress(reporter, "gov")
		committee := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		// The handler refuses a committee that is the authority itself, and an
		// account drawn at random could be it.
		if committee.AddressBech32 == authority {
			reporter.Skip("drawn committee is the chain authority")

			return nil, nil
		}

		r := testData.Rand()
		height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
		activation := height + r.Uint64InRange(1, 1_000)

		return nil, &types.MsgSetSecurityMandate{
			Authority:        authority,
			Committee:        committee.AddressBech32,
			ActivationHeight: activation,
			// Validation demands activation strictly precede expiry, and the
			// handler demands expiry sit above the current height.
			ExpiryHeight: activation + r.Uint64InRange(1, 100_000),
		}
	}
}
