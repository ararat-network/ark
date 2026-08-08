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

	partition, err := q.k.liabilityPartitionValue(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	// The claimable aggregate is reported whether or not the partition is
	// complete: it is what redemption coverage divides by, so withholding it
	// would hide the figure driving payouts during the only stress that makes
	// it interesting.
	claimable, err := partition.recognizedNoah()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	// Targets are the one consumer that needs the complete figure. An
	// incomplete partition understates outstanding exposure, and sizing a
	// target off it would call for less capital exactly when an asset has just
	// failed, so a zero basis reports real balances beside targets that claim
	// nothing.
	targetBasis := math.LegacyZeroDec()
	if partition.complete {
		targetBasis = claimable
	}
	fundStatus, err := q.k.calculateFundStatus(ctx, targetBasis)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	return &types.QueryFundStatusResponse{
		PricedLiability:          chain.NoahDecCoin(partition.pricedNoah),
		SettlementLiability:      chain.NoahDecCoin(partition.settlementNoah),
		StalePricedLiability:     chain.NoahDecCoin(partition.staleNoah),
		UntrustedSuspendedSupply: partition.untrusted,
		WrittenOffExposure:       partition.writtenOff,
		StaleMemberSupply:        partition.stale,
		NominalLiability:         chain.NoahDecCoin(claimable),
		SubsidyPoolBalance:       chain.NoahCoin(q.k.balance(ctx, types.SubsidyPoolName)),
		RedemptionBufferBalance:  chain.NoahCoin(fundStatus.bufferBalance),
		RedemptionBufferTarget:   chain.NoahCoin(fundStatus.bufferTarget),
		StrategicReserveBalance:  chain.NoahCoin(fundStatus.reserveBalance),
		StrategicReserveTarget:   chain.NoahCoin(fundStatus.reserveTarget),
		InsuranceBalance:         chain.NoahCoin(fundStatus.insuranceBalance),
		InsuranceTarget:          chain.NoahCoin(fundStatus.insuranceTarget),
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
