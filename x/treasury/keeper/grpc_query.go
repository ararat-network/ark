// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/treasury/keeper/querier.go.
// Modified for Ark: modern query handlers and chain-specific state.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
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

// EconomicPolicy queries the current reversible Treasury policy.
func (q queryServer) EconomicPolicy(ctx context.Context, req *types.QueryEconomicPolicyRequest) (*types.QueryEconomicPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	policy, err := q.k.EconomicPolicy.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting economic policy: %v", err)
	}
	return &types.QueryEconomicPolicyResponse{Policy: policy}, nil
}

// EconomicMandate queries the governed committee appointment and its
// current effective status.
func (q queryServer) EconomicMandate(ctx context.Context, req *types.QueryEconomicMandateRequest) (*types.QueryEconomicMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	mandate, err := q.k.EconomicMandate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting economic mandate: %v", err)
	}
	return &types.QueryEconomicMandateResponse{
		Mandate: mandate,
		Active:  mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
	}, nil
}

// TaxCap queries the derived tax cap for one denomination.
func (q queryServer) TaxCap(ctx context.Context, req *types.QueryTaxCapRequest) (*types.QueryTaxCapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	taxCap, err := q.k.GetTaxCap(ctx, req.Denom)
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

	// The response derives each cap over one params read rather than through
	// GetTaxCap, which would re-read params per entry.
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting params: %v", err)
	}
	taxCaps := make([]types.TaxCap, 0)
	if err := q.k.ConversionFactors.Walk(ctx, nil, func(denom string, factor types.ConversionFactor) (bool, error) {
		// GetTaxCap's exclusion: NOAH's entry prices fees, never tax.
		if denom == chain.NoahBaseDenom {
			return false, nil
		}
		taxCaps = append(taxCaps, types.TaxCap{
			Denom:  denom,
			TaxCap: deriveTaxCap(params, factor),
		})
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing treasury tax caps: %v", err)
	}

	return &types.QueryTaxCapsResponse{TaxCaps: taxCaps}, nil
}

// ConversionFactor queries one denomination's stored conversion factor.
func (q queryServer) ConversionFactor(ctx context.Context, req *types.QueryConversionFactorRequest) (*types.QueryConversionFactorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	factor, err := q.k.ConversionFactors.Get(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "conversion factor not found for denom %s", req.Denom)
		}
		return nil, status.Errorf(codes.Internal, "getting conversion factor for denom %s: %v", req.Denom, err)
	}
	return &types.QueryConversionFactorResponse{ConversionFactor: factor}, nil
}

// ConversionFactors queries the complete factor table in denomination order.
func (q queryServer) ConversionFactors(ctx context.Context, req *types.QueryConversionFactorsRequest) (*types.QueryConversionFactorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	factors := make([]types.ConversionFactor, 0)
	if err := q.k.ConversionFactors.Walk(ctx, nil, func(_ string, factor types.ConversionFactor) (bool, error) {
		factors = append(factors, factor)
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing treasury conversion factors: %v", err)
	}
	return &types.QueryConversionFactorsResponse{ConversionFactors: factors}, nil
}

// GasPrice reports one accepted fee denomination's gas price — the base
// price carried through its conversion factor.
func (q queryServer) GasPrice(ctx context.Context, req *types.QueryGasPriceRequest) (*types.QueryGasPriceResponse, error) {
	if req == nil || req.Denom == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury params: %v", err)
	}
	row, err := q.k.gasPrice(ctx, params.ReferenceDenom, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "%s is not an accepted fee denomination", req.Denom)
		}
		return nil, status.Errorf(codes.Internal, "pricing gas in %s: %v", req.Denom, err)
	}
	return &types.QueryGasPriceResponse{GasPrice: row}, nil
}

// GasPrices returns reference identity separately and other accepted denominations, including NOAH,
// in key order. Unrepresentable prices are omitted; presentation order does not set fee-payment
// priority.
func (q queryServer) GasPrices(ctx context.Context, req *types.QueryGasPricesRequest) (*types.QueryGasPricesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury params: %v", err)
	}
	reference := params.ReferenceDenom

	referenceRow, err := q.k.gasPrice(ctx, reference, reference)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "pricing gas in %s: %v", reference, err)
	}

	rows := make([]types.GasPrice, 0)
	if err := q.k.ConversionFactors.Walk(ctx, nil, func(denom string, _ types.ConversionFactor) (bool, error) {
		if denom == reference {
			return false, nil
		}
		row, err := q.k.gasPrice(ctx, reference, denom)
		if err != nil {
			return false, nil
		}
		rows = append(rows, row)
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing gas prices: %v", err)
	}

	return &types.QueryGasPricesResponse{GasPrices: rows, ReferenceGasPrice: referenceRow}, nil
}

// ComputeTax computes the current transfer tax for the supplied SDK messages.
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

	tax, base, err := q.k.ComputeTax(ctx, msgs)
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
	return &types.QueryComputeTaxResponse{Tax: tax, TaxBase: base}, nil
}

// FundStatus reports balances, liability buckets, exclusions, and both target bases. Incomplete
// valuation returns a report with zero targets instead of an error. Queries emit no
// degraded-valuation event; store and arithmetic errors still fail.
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
	// Report nominal targets for committee bounds and net targets for expansion routing. Incomplete
	// valuation zeroes both reported target families while preserving balances and liability
	// disclosures.
	targetBasis, gapBasis := math.LegacyZeroDec(), math.LegacyZeroDec()
	if partition.complete {
		targetBasis, gapBasis = claimable, net
	}
	policy, err := q.k.EconomicPolicy.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting economic policy: %v", err)
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

// ExposureStatus reads stored risk state without a registry fold, remaining independent of
// FundStatus valuation completeness.
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
