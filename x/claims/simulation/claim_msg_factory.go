package simulation

import (
	"context"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/claims/keeper"
	"github.com/ararat-network/ark/x/claims/types"
)

// registry is the run's asset registry, the default one, whose currencies a claim may pay in. The
// handler refuses one the run has since suspended, and the factories skip it.
var registry = func() []string {
	assets := assettypes.DefaultGenesisState().Assets
	denoms := make([]string, len(assets))
	for i, asset := range assets {
		denoms[i] = asset.Denom
	}
	return denoms
}()

// drawClaim draws what a simulated claim pays within ceiling NOAH base units: NOAH half the time,
// otherwise a registry currency the handler buys through Market, offering up to all uncommitted
// NOAH. The run's rates span 0.001 to 1,000 NOAH a unit, so a currency claim draws within a
// thousandth of the ceiling to stay affordable at the dearest.
func drawClaim(r *simsx.XRand, ceiling, uncommitted math.Int) (sdk.Coin, math.Int, error) {
	if stable := ceiling.QuoRaw(1_000); stable.IsPositive() && r.Intn(2) == 0 {
		amount, err := r.PositiveSDKIntn(stable)
		if err != nil {
			return sdk.Coin{}, math.Int{}, err
		}
		return sdk.NewCoin(registry[r.Intn(len(registry))], amount), uncommitted, nil
	}
	amount, err := r.PositiveSDKIntn(ceiling)
	if err != nil {
		return sdk.Coin{}, math.Int{}, err
	}
	return chain.NoahCoin(amount), math.ZeroInt(), nil
}

// MsgSetClaimsMandateFactory appoints a simulation-owned signer and a gross claim limit. Its window
// spans at least the cancellation period so submitted claims can close before expiry.
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
		now := chain.BlockSeconds(ctx)
		activation := now + r.Uint64InRange(1, 1_000)

		return nil, &types.MsgSetClaimsMandate{
			Authority:      authority,
			Committee:      committee.AddressBech32,
			ActivationTime: activation,
			// Validation demands activation strictly precede expiry, and the
			// handler demands expiry sit above the current block time and the span
			// cover the cancellation period.
			ExpiryTime: activation + params.ClaimCancellationPeriod + r.Uint64InRange(1, 100_000),
			// A configured mandate must allow the committee to pay something.
			CommitteeClaimLimit: math.NewInt(int64(r.IntInRange(1, 1_000_000_000))),
		}
	}
}

// MsgSubmitClaimFactory books a governance claim against Insurance, in NOAH or a registry currency
// bought through Market. The amount stays inside what Insurance holds beyond what is already
// reserved, and the factory runs the handler on a discarded cache and skips a claim it refuses; the
// proposal still executes later against whatever state it meets.
func MsgSubmitClaimFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSubmitClaim] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSubmitClaim) {
		unreserved, err := k.Uncommitted(ctx, chain.NoahBaseDenom)
		if err != nil {
			reporter.Skip("get uncommitted insurance: " + err.Error())

			return nil, nil
		}
		if !unreserved.IsPositive() {
			reporter.Skip("insurance holds nothing beyond what is already reserved")

			return nil, nil
		}

		recipient := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		r := testData.Rand()
		amount, maxOffer, err := drawClaim(r, unreserved, unreserved)
		if err != nil {
			reporter.Skip("draw claim amount: " + err.Error())

			return nil, nil
		}

		msg := &types.MsgSubmitClaim{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			// A claim must carry a non-empty reference.
			Reference: "sim-claim-" + r.StringN(8),
			Recipient: recipient.AddressBech32,
			Amount:    amount,
			MaxOffer:  maxOffer,
		}
		cache, _ := sdk.UnwrapSDKContext(ctx).CacheContext()
		if _, err := keeper.NewMsgServerImpl(k).SubmitClaim(cache, msg); err != nil {
			reporter.Skip("claim would be refused: " + err.Error())

			return nil, nil
		}
		return nil, msg
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
		now := chain.BlockSeconds(ctx)

		var candidates []uint64
		if err := k.Claims.Walk(ctx, nil, func(id uint64, claim types.Claim) (bool, error) {
			if claim.Status == types.ClaimStatus_CLAIM_STATUS_PENDING && now < claim.ClosingTime {
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
