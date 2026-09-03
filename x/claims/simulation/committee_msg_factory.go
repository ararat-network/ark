package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/keeper"
	"github.com/ararat-network/ark/x/claims/types"
)

// activeClaimsCommittee resolves the appointed committee when the mandate is
// active at this height and the run holds its key. A skip is the norm rather
// than a fault: an appointment may have expired, or been restored from an
// export naming an address this run never held.
func activeClaimsCommittee(
	ctx context.Context,
	k *keeper.Keeper,
	testData *simsx.ChainDataSource,
	reporter simsx.SimulationReporter,
) (types.ClaimsMandate, simsx.SimAccount, bool) {
	mandate, err := k.ClaimsMandate.Get(ctx)
	if err != nil {
		reporter.Skip("get claims mandate: " + err.Error())

		return types.ClaimsMandate{}, simsx.SimAccount{}, false
	}
	if !mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())) {
		reporter.Skip("claims mandate is not active at this height")

		return types.ClaimsMandate{}, simsx.SimAccount{}, false
	}
	committee := testData.GetAccount(reporter, mandate.Committee)
	if reporter.IsSkipped() {
		return types.ClaimsMandate{}, simsx.SimAccount{}, false
	}

	return mandate, committee, true
}

// MsgCommitteeSubmitClaimFactory books a committee claim against Insurance.
// Two ceilings bind rather than one: what Insurance holds beyond its standing
// reservations, and what the mandate still permits the committee to spend. The
// claim must also close before the appointment expires, which the seeded
// window leaves ample room for.
func MsgCommitteeSubmitClaimFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeSubmitClaim] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeSubmitClaim) {
		mandate, committee, ok := activeClaimsCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			reporter.Skip("get claims params: " + err.Error())

			return nil, nil
		}
		height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
		if height+params.ClaimCancellationPeriodBlocks > mandate.ExpiryHeight {
			reporter.Skip("claim would close after the appointment expires")

			return nil, nil
		}

		reserved, err := k.InsuranceReserved.Get(ctx)
		if err != nil {
			reporter.Skip("get insurance reservation: " + err.Error())

			return nil, nil
		}
		used, err := k.ClaimsAllowanceUsed.Get(ctx)
		if err != nil {
			reporter.Skip("get claims allowance usage: " + err.Error())

			return nil, nil
		}

		ceiling := k.InsuranceBalance(ctx).Sub(reserved)
		if remaining := mandate.CommitteeClaimLimit.Amount.Sub(used); remaining.LT(ceiling) {
			ceiling = remaining
		}
		if !ceiling.IsPositive() {
			reporter.Skip("no headroom under the Insurance balance and the mandate limit")

			return nil, nil
		}

		recipient := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		r := testData.Rand()
		amount, err := r.PositiveSDKIntn(ceiling)
		if err != nil {
			reporter.Skip("draw claim amount: " + err.Error())

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeSubmitClaim{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			// A claim must carry a non-empty reference.
			Reference: "sim-committee-claim-" + r.StringN(8),
			Recipient: recipient.AddressBech32,
			Amount:    chain.NoahCoin(amount),
		}
	}
}

// MsgCommitteeCancelClaimFactory withdraws a claim the committee itself
// submitted. It may not cancel a governance claim, and only a pending claim
// still inside its cancellation window qualifies.
func MsgCommitteeCancelClaimFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeCancelClaim] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeCancelClaim) {
		mandate, committee, ok := activeClaimsCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}

		height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
		var candidates []uint64
		if err := k.Claims.Walk(ctx, nil, func(id uint64, claim types.Claim) (bool, error) {
			if claim.Status == types.ClaimStatus_CLAIM_STATUS_PENDING &&
				claim.Origin == types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE &&
				height < claim.ClosingHeight {
				candidates = append(candidates, id)
			}

			return false, nil
		}); err != nil {
			reporter.Skip("iterating claims: " + err.Error())

			return nil, nil
		}
		if len(candidates) == 0 {
			reporter.Skip("no pending committee claim to cancel")

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeCancelClaim{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			ClaimId:      candidates[testData.Rand().Intn(len(candidates))],
		}
	}
}
