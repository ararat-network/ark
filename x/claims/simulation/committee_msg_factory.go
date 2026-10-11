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
// active at this block time and the run holds its key. A skip is the norm rather
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
	if !mandate.IsActive(chain.BlockSeconds(ctx)) {
		reporter.Skip("claims mandate is not active at this block time")

		return types.ClaimsMandate{}, simsx.SimAccount{}, false
	}
	committee := testData.GetAccount(reporter, mandate.Committee)
	if reporter.IsSkipped() {
		return types.ClaimsMandate{}, simsx.SimAccount{}, false
	}

	return mandate, committee, true
}

// MsgCommitteeSubmitClaimFactory books claims, in NOAH or a registry currency bought through Market,
// from at most a tenth of uncommitted Insurance. Closing must precede mandate expiry. The charge reads oracle rates the keeper does not expose, and
// a run's genesis rates go stale, so the factory runs the handler on a discarded cache and skips if
// it refuses.
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
		now := chain.BlockSeconds(ctx)
		if now+params.ClaimCancellationPeriod > mandate.ExpiryTime {
			reporter.Skip("claim would close after the appointment expires")

			return nil, nil
		}

		unreserved, err := k.Uncommitted(ctx, chain.NoahBaseDenom)
		if err != nil {
			reporter.Skip("get uncommitted insurance: " + err.Error())

			return nil, nil
		}
		ceiling := unreserved.QuoRaw(10)
		if !ceiling.IsPositive() {
			reporter.Skip("no uncommitted Insurance NOAH to claim")

			return nil, nil
		}

		recipient := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		r := testData.Rand()
		amount, maxOffer, err := drawClaim(r, ceiling, unreserved)
		if err != nil {
			reporter.Skip("draw claim amount: " + err.Error())

			return nil, nil
		}

		msg := &types.MsgCommitteeSubmitClaim{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			// A claim must carry a non-empty reference.
			Reference: "sim-committee-claim-" + r.StringN(8),
			Recipient: recipient.AddressBech32,
			Amount:    amount,
			MaxOffer:  maxOffer,
		}
		cache, _ := sdk.UnwrapSDKContext(ctx).CacheContext()
		if _, err := keeper.NewMsgServerImpl(k).CommitteeSubmitClaim(cache, msg); err != nil {
			reporter.Skip("claim would be refused: " + err.Error())

			return nil, nil
		}
		return []simsx.SimAccount{committee}, msg
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

		now := chain.BlockSeconds(ctx)
		var candidates []uint64
		if err := k.Claims.Walk(ctx, nil, func(id uint64, claim types.Claim) (bool, error) {
			if claim.Status == types.ClaimStatus_CLAIM_STATUS_PENDING &&
				claim.Origin == types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE &&
				now < claim.ClosingTime {
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
