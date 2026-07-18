package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

var _ types.QueryServer = (*queryServer)(nil)

type queryServer struct {
	types.UnimplementedQueryServer

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
		Active:  mandate.IsActive(sdk.UnwrapSDKContext(ctx).BlockHeight()),
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
		case errors.Is(err, types.ErrTaxCapUnavailable):
			code = codes.FailedPrecondition
		case errors.Is(err, types.ErrTaxOutOfRange):
			code = codes.OutOfRange
		}
		return nil, status.Errorf(code, "computing treasury tax: %v", err)
	}
	return &types.QueryComputeTaxResponse{Tax: tax}, nil
}

// FundStatus queries live Treasury balances, liabilities, and targets.
func (q queryServer) FundStatus(ctx context.Context, req *types.QueryFundStatusRequest) (*types.QueryFundStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	tobinTaxes, err := q.k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: getting Tobin taxes: %v", err)
	}
	liabilityNoah, complete, err := q.k.nominalLiabilityValue(ctx, tobinTaxes, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	if !complete {
		return nil, status.Error(codes.Internal, "getting treasury fund status: complete Treasury liability valuation is unavailable")
	}
	fundStatus, err := q.k.calculateFundStatus(ctx, liabilityNoah)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury fund status: %v", err)
	}
	return &types.QueryFundStatusResponse{
		NominalLiabilityNoahEquivalent: sdk.NewDecCoinFromDec(chain.MicroNoahDenom, liabilityNoah),
		SubsidyPoolBalance:             sdk.NewCoin(chain.MicroNoahDenom, q.k.balance(ctx, types.SubsidyPoolName)),
		RedemptionBufferBalance:        sdk.NewCoin(chain.MicroNoahDenom, fundStatus.bufferBalance),
		RedemptionBufferTarget:         sdk.NewCoin(chain.MicroNoahDenom, fundStatus.bufferTarget),
		StrategicReserveBalance:        sdk.NewCoin(chain.MicroNoahDenom, fundStatus.reserveBalance),
		StrategicReserveTarget:         sdk.NewCoin(chain.MicroNoahDenom, fundStatus.reserveTarget),
		InsuranceBalance:               sdk.NewCoin(chain.MicroNoahDenom, fundStatus.insuranceBalance),
		InsuranceReserved:              sdk.NewCoin(chain.MicroNoahDenom, fundStatus.insuranceReserved),
		InsuranceUnencumberedBalance:   sdk.NewCoin(chain.MicroNoahDenom, fundStatus.insuranceUnencumbered),
		InsuranceTarget:                sdk.NewCoin(chain.MicroNoahDenom, fundStatus.insuranceTarget),
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

// ClaimsMandate queries the stored committee mandate, allowance usage, and
// Insurance reservation.
func (q queryServer) ClaimsMandate(ctx context.Context, req *types.QueryClaimsMandateRequest) (*types.QueryClaimsMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	mandate, err := q.k.ClaimsMandate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Claims mandate: %v", err)
	}
	insuranceReserved, err := q.k.InsuranceReserved.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Insurance reservation: %v", err)
	}
	allowanceUsed, err := q.k.ClaimsAllowanceUsed.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Claims allowance usage: %v", err)
	}
	allowanceRemaining, err := mandate.CommitteeClaimLimit.SafeSub(allowanceUsed)
	if err != nil || allowanceRemaining.IsNegative() {
		return nil, status.Errorf(
			codes.Internal,
			"Claims allowance usage %s exceeds mandate limit %s",
			allowanceUsed,
			mandate.CommitteeClaimLimit,
		)
	}

	return &types.QueryClaimsMandateResponse{
		Mandate:            mandate,
		InsuranceReserved:  insuranceReserved,
		Active:             mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
		AllowanceUsed:      allowanceUsed,
		AllowanceRemaining: allowanceRemaining,
	}, nil
}

// Claim queries one permanent claim record.
func (q queryServer) Claim(ctx context.Context, req *types.QueryClaimRequest) (*types.QueryClaimResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.ClaimId == 0 {
		return nil, status.Error(codes.InvalidArgument, "claim ID must be positive")
	}

	claim, err := q.k.Claims.Get(ctx, req.ClaimId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "claim %d not found", req.ClaimId)
		}
		return nil, status.Errorf(codes.Internal, "getting claim %d: %v", req.ClaimId, err)
	}
	return &types.QueryClaimResponse{Claim: claim}, nil
}

// Claims queries the paginated permanent claim record.
func (q queryServer) Claims(ctx context.Context, req *types.QueryClaimsRequest) (*types.QueryClaimsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	claims, pageResponse, err := sdkquery.CollectionPaginate(
		ctx,
		q.k.Claims,
		req.Pagination,
		func(_ uint64, claim types.Claim) (types.Claim, error) {
			return claim, nil
		},
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing claims: %v", err)
	}

	return &types.QueryClaimsResponse{
		Claims:     claims,
		Pagination: pageResponse,
	}, nil
}
