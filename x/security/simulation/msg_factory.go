package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/security/types"
)

// MsgSetSecurityMandateFactory appoints the security committee. The appointee
// is drawn from the simulation's own accounts, so the committee is an address
// the run holds a key for. The handler derives the term itself, and refuses an
// expiry at or below the current block time, so the window is anchored ahead of it.
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
		now := chain.BlockSeconds(ctx)
		activation := now + r.Uint64InRange(1, 1_000)

		return nil, &types.MsgSetSecurityMandate{
			Authority:      authority,
			Committee:      committee.AddressBech32,
			ActivationTime: activation,
			// Validation demands activation strictly precede expiry, and the
			// handler demands expiry sit above the current block time.
			ExpiryTime: activation + r.Uint64InRange(1, 100_000),
		}
	}
}
