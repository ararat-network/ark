package simulation

import (
	"context"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/keeper"
	"github.com/ararat-network/ark/x/claims/types"
)

// MsgSetClaimsMandateFactory appoints the Claims committee and states the
// total it may pay out. The appointee is drawn from the simulation's own
// accounts, so the committee is an address the run holds a key for.
//
// The span must cover the cancellation period: a shorter appointment could
// submit no claim at all, because every closing height would land past expiry.
func MsgSetClaimsMandateFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSetClaimsMandate] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetClaimsMandate) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			reporter.Skip("get claims params: " + err.Error())

			return nil, nil
		}
		authority := testData.ModuleAccountAddress(reporter, "gov")
		committee := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		// The handler refuses a committee that is the authority itself, and an
		// account drawn at random could be it.
		if committee.AddressBech32 == authority {
			reporter.Skip("drawn committee is the Claims authority")

			return nil, nil
		}

		r := testData.Rand()
		activation := r.Uint64InRange(1, 1_000)

		return nil, &types.MsgSetClaimsMandate{
			Authority:        authority,
			Committee:        committee.AddressBech32,
			ActivationHeight: activation,
			// Validation demands activation strictly precede expiry, and the
			// handler demands the span cover the cancellation period.
			ExpiryHeight: activation + params.ClaimCancellationPeriodBlocks + r.Uint64InRange(1, 100_000),
			// A configured mandate must allow the committee to pay something.
			CommitteeClaimLimit: chain.NoahCoin(math.NewInt(int64(r.IntInRange(1, 1_000_000_000)))),
		}
	}
}

// MsgSubmitClaimFactory books a governance claim against Insurance. The amount
// stays inside what Insurance holds beyond what is already reserved, which is
// the balance the handler checks the claim against.
func MsgSubmitClaimFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSubmitClaim] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSubmitClaim) {
		reserved, err := k.InsuranceReserved.Get(ctx)
		if err != nil {
			reporter.Skip("get insurance reservation: " + err.Error())

			return nil, nil
		}
		unreserved := k.InsuranceBalance(ctx).Sub(reserved)
		if !unreserved.IsPositive() {
			reporter.Skip("insurance holds nothing beyond what is already reserved")

			return nil, nil
		}

		recipient := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		r := testData.Rand()
		amount, err := r.PositiveSDKIntn(unreserved)
		if err != nil {
			reporter.Skip("draw claim amount: " + err.Error())

			return nil, nil
		}

		return nil, &types.MsgSubmitClaim{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			// A claim must carry a non-empty reference.
			Reference: "sim-claim-" + r.StringN(8),
			Recipient: recipient.AddressBech32,
			Amount:    chain.NoahCoin(amount),
		}
	}
}

// MsgCancelClaimFactory withdraws a claim before it pays. Only a pending claim
// still inside its cancellation window qualifies: once the window closes the
// claim is due, and cancelling it is no longer governance's to do.
func MsgCancelClaimFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCancelClaim] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCancelClaim) {
		height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())

		var candidates []uint64
		if err := k.Claims.Walk(ctx, nil, func(id uint64, claim types.Claim) (bool, error) {
			if claim.Status == types.ClaimStatus_CLAIM_STATUS_PENDING && height < claim.ClosingHeight {
				candidates = append(candidates, id)
			}

			return false, nil
		}); err != nil {
			reporter.Skip("iterating claims: " + err.Error())

			return nil, nil
		}
		if len(candidates) == 0 {
			reporter.Skip("no pending claim to cancel")

			return nil, nil
		}

		return nil, &types.MsgCancelClaim{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			ClaimId:   candidates[testData.Rand().Intn(len(candidates))],
		}
	}
}
