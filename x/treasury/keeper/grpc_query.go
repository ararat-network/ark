package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

var _ types.QueryServer = (*queryServer)(nil)

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns the Treasury query server.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Params queries the current Treasury parameters.
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury params: %v", err)
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

// MonetaryPolicy queries the current reversible Treasury policy.
func (q queryServer) MonetaryPolicy(ctx context.Context, req *types.QueryMonetaryPolicyRequest) (*types.QueryMonetaryPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	policy, err := q.k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting monetary policy: %v", err)
	}
	return &types.QueryMonetaryPolicyResponse{Policy: policy}, nil
}

// MonetaryMandate queries the governed committee appointment and its
// current effective status.
func (q queryServer) MonetaryMandate(ctx context.Context, req *types.QueryMonetaryMandateRequest) (*types.QueryMonetaryMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	mandate, err := q.k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting monetary mandate: %v", err)
	}
	return &types.QueryMonetaryMandateResponse{
		Mandate: mandate,
		Active:  mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
	}, nil
}

// TaxCap queries the derived tax cap for one denomination.
func (q queryServer) TaxCap(ctx context.Context, req *types.QueryTaxCapRequest) (*types.QueryTaxCapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	taxCap, err := q.k.TaxCaps.Get(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "tax cap not found for denom %s", req.Denom)
		}
		return nil, status.Errorf(codes.Internal, "getting tax cap for denom %s: %v", req.Denom, err)
	}
	return &types.QueryTaxCapResponse{TaxCap: taxCap}, nil
}

// TaxCaps queries the complete derived tax-cap map in denomination order.
func (q queryServer) TaxCaps(ctx context.Context, req *types.QueryTaxCapsRequest) (*types.QueryTaxCapsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	taxCaps := make([]types.TaxCap, 0)
	if err := q.k.TaxCaps.Walk(ctx, nil, func(denom string, taxCap math.Int) (bool, error) {
		taxCaps = append(taxCaps, types.TaxCap{Denom: denom, TaxCap: taxCap})
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing treasury tax caps: %v", err)
	}

	return &types.QueryTaxCapsResponse{TaxCaps: taxCaps}, nil
}

// ComputeTax computes the current stability tax for the supplied SDK messages.
func (q queryServer) ComputeTax(ctx context.Context, req *types.QueryComputeTaxRequest) (*types.QueryComputeTaxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	msgs := make([]sdk.Msg, len(req.Messages))
	for i, anyMsg := range req.Messages {
		if anyMsg == nil {
			return nil, status.Errorf(codes.InvalidArgument, "message %d is nil", i)
		}
		if err := q.k.cdc.UnpackAny(anyMsg, &msgs[i]); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unpacking message %d: %v", i, err)
		}
	}

	tax, err := q.k.ComputeTax(ctx, msgs)
	if err != nil {
		code := codes.Internal
		switch {
		case errors.Is(err, types.ErrInvalidTaxMessage):
			code = codes.InvalidArgument
		case errors.Is(err, types.ErrTaxOutOfRange):
			code = codes.OutOfRange
		}
		return nil, status.Errorf(code, "computing treasury tax: %v", err)
	}
	return &types.QueryComputeTaxResponse{Tax: tax}, nil
}

// FundStatus queries live Treasury balances, the partitioned liability report,
// and fund targets. It always answers, because hiding the report during exactly
// the stress that makes valuation incomplete would blind operators when they
// most need it: an incomplete valuation still reports the claimable aggregate
// beside the partition explaining what it excludes, with every target zero
// rather than a guess. It is also the one caller that builds a partition
// without disclosing a degraded one, since a query reports a partition rather
// than recording one.
func (q queryServer) FundStatus(ctx context.Context, req *types.QueryFundStatusRequest) (*types.QueryFundStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	partition, err := q.k.liabilityPartitionValue(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	// The claimable aggregate is reported whether or not the partition is
	// complete: it is what redemption coverage divides by, so withholding it
	// would hide the figure driving payouts during the only stress that makes
	// it interesting.
	claimable, err := partition.recognised()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	net, err := partition.net()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	// Targets are the one consumer that needs the complete figure. An
	// incomplete partition understates outstanding exposure, and sizing a
	// target off it would call for less capital exactly when an asset has just
	// failed, so a zero basis reports real balances beside targets that claim
	// nothing.
	//
	// Both bases are reported rather than one. The query decides nothing, and
	// the two answer different operational questions: the nominal-sized targets
	// are what bounds a committee burn or transfer right now, while the
	// net-sized ones are where the next expansion will route. Publishing one
	// would leave the other derivable only by a reader who knew the policy
	// ratios and the netting rule.
	targetBasis, gapBasis := math.LegacyZeroDec(), math.LegacyZeroDec()
	if partition.complete {
		targetBasis, gapBasis = claimable, net
	}
	policy, err := q.k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting monetary policy: %v", err)
	}
	// Each committee-operated fund reports its own recognised capital, per the
	// contract on ClaimsKeeper; the Buffer has no operator, so Treasury reads it.
	insuranceBalance, err := q.k.claimsKeeper.RecognisedCapital(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	reserveBalance, err := q.k.reserveKeeper.RecognisedCapital(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	// One policy against both bases: the funds hold what they hold either way,
	// and only the figure they are measured against moves. Both bases carry the
	// exposure multiplier, so the two families stay comparable and the reported
	// multiplier reconciles either of them against the liability beside it.
	scaledTargetBasis, err := q.k.exposureAdjusted(ctx, targetBasis)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	scaledGapBasis, err := q.k.exposureAdjusted(ctx, gapBasis)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	exposureState, err := q.k.getExposureState(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	grossTargets := policy.FundTargets(scaledTargetBasis)
	netTargets := policy.FundTargets(scaledGapBasis)
	return &types.QueryFundStatusResponse{
		PricedLiability:           chain.NoahDecCoin(partition.priced),
		SettlementLiability:       chain.NoahDecCoin(partition.settlement),
		StalePricedLiability:      chain.NoahDecCoin(partition.stale),
		NominalLiability:          chain.NoahDecCoin(claimable),
		SelfHeldSupply:            partition.selfHeldSupply,
		SelfHeldLiability:         chain.NoahDecCoin(partition.selfHeld),
		NetLiability:              chain.NoahDecCoin(net),
		StaleMemberSupply:         partition.staleSupply,
		UntrustedSuspendedSupply:  partition.untrustedSupply,
		WrittenOffExposure:        partition.writtenOff,
		RedemptionBufferBalance:   chain.NoahCoin(q.k.getBalance(ctx, types.RedemptionBufferName)),
		RedemptionBufferTarget:    chain.NoahCoin(grossTargets.Buffer),
		RedemptionBufferNetTarget: chain.NoahCoin(netTargets.Buffer),
		StrategicReserveBalance:   chain.NoahCoin(reserveBalance),
		StrategicReserveTarget:    chain.NoahCoin(grossTargets.Reserve),
		StrategicReserveNetTarget: chain.NoahCoin(netTargets.Reserve),
		InsuranceBalance:          chain.NoahCoin(insuranceBalance),
		InsuranceTarget:           chain.NoahCoin(grossTargets.Insurance),
		InsuranceNetTarget:        chain.NoahCoin(netTargets.Insurance),
		SubsidyPoolBalance:        chain.NoahCoin(q.k.getBalance(ctx, types.SubsidyPoolName)),
		ExposureMultiplier:        exposureState.Multiplier,
	}, nil
}

// RewardFunding queries the active reward-funding accounting.
func (q queryServer) RewardFunding(ctx context.Context, req *types.QueryRewardFundingRequest) (*types.QueryRewardFundingResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	funding, err := q.k.RewardFunding.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting reward funding: %v", err)
	}
	return &types.QueryRewardFundingResponse{RewardFunding: funding}, nil
}

// ExposureStatus queries the risk state behind the fund-target multiplier.
//
// It is served from stored state alone and folds no registry, which is what
// keeps it answerable when FundStatus is expensive or its targets are zeroed by
// an incomplete valuation: the multiplier is a property of the risk series, not
// of whether this block could price every member.
func (q queryServer) ExposureStatus(ctx context.Context, req *types.QueryExposureStatusRequest) (*types.QueryExposureStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	state, err := q.k.getExposureState(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting exposure status: %v", err)
	}
	volatility, err := annualisedVolatility(state.VolatilityVariance)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting exposure status: %v", err)
	}
	pending, err := q.k.ExposureRefreshPending.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "getting exposure status: %v", err)
	}

	return &types.QueryExposureStatusResponse{
		ExposureState:        state,
		AnnualisedVolatility: volatility,
		RefreshPending:       pending,
	}, nil
}
