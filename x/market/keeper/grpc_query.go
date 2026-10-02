// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/market/keeper/querier.go.
// Modified for Ark: modern query handlers and chain-specific state.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k *Keeper
}

func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Sell quotes a stated offer.
func (q queryServer) Sell(ctx context.Context, req *types.QuerySellRequest) (*types.QuerySellResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	offerCoin, err := sdk.ParseCoinNormalized(req.OfferCoin)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parsing offer coin %q: %v", req.OfferCoin, err)
	}

	priced, err := q.k.priceSell(ctx, offerCoin, req.AskDenom)
	if err != nil {
		return nil, quoteStatus(err, fmt.Sprintf("computing swap from %s to %s", offerCoin, req.AskDenom))
	}

	return &types.QuerySellResponse{
		SwapCoin: priced.output,
		SwapFee:  priced.fee,
	}, nil
}

// Buy quotes a stated ask: the least offer that buys it.
func (q queryServer) Buy(ctx context.Context, req *types.QueryBuyRequest) (*types.QueryBuyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	askCoin, err := sdk.ParseCoinNormalized(req.AskCoin)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parsing ask coin %q: %v", req.AskCoin, err)
	}

	priced, err := q.k.priceBuy(ctx, askCoin, req.OfferDenom)
	if err != nil {
		return nil, quoteStatus(err, fmt.Sprintf("sizing an offer of %s for %s", req.OfferDenom, askCoin))
	}

	return &types.QueryBuyResponse{
		OfferCoin: priced.offer,
		SwapCoin:  priced.output,
		SwapFee:   priced.fee,
	}, nil
}

// Pool queries the live virtual-pool state: the policy depth and the signed
// deviation from it, in one snapshot.
func (q queryServer) Pool(ctx context.Context, _ *types.QueryPoolRequest) (*types.QueryPoolResponse, error) {
	arkPoolDelta, err := q.k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market ark pool delta: %v", err)
	}
	capacity, err := q.k.ConversionPolicy.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market conversion policy: %v", err)
	}
	return &types.QueryPoolResponse{
		BasePool:     capacity.BasePool,
		ArkPoolDelta: arkPoolDelta,
	}, nil
}

// Params queries params of market module
func (q queryServer) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market params: %v", err)
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// TobinTax queries the effective Tobin rate for a denomination.
func (q queryServer) TobinTax(ctx context.Context, req *types.QueryTobinTaxRequest) (*types.QueryTobinTaxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	if err := chain.ValidatePricedDenom(req.Denom); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	taxes, err := q.k.GetTobinTaxes(ctx, req.Denom)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTobinTaxResponse{TobinTax: taxes[req.Denom]}, nil
}

// TobinTaxOverrides queries the sparse per-denomination exceptions.
func (q queryServer) TobinTaxOverrides(ctx context.Context, _ *types.QueryTobinTaxOverridesRequest) (*types.QueryTobinTaxOverridesResponse, error) {
	overrides, err := q.k.GetTobinTaxOverrides(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTobinTaxOverridesResponse{TobinTaxOverrides: overrides}, nil
}

// ConversionPolicy queries the live conversion-capacity pair.
func (q queryServer) ConversionPolicy(ctx context.Context, _ *types.QueryConversionPolicyRequest) (*types.QueryConversionPolicyResponse, error) {
	capacity, err := q.k.ConversionPolicy.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market conversion policy: %v", err)
	}

	return &types.QueryConversionPolicyResponse{ConversionPolicy: capacity}, nil
}

// ConversionMandate queries the governed conversion committee appointment and
// whether it is usable at the current height.
func (q queryServer) ConversionMandate(ctx context.Context, _ *types.QueryConversionMandateRequest) (*types.QueryConversionMandateResponse, error) {
	conversionMandate, err := q.k.ConversionMandate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market conversion mandate: %v", err)
	}
	// The live pool decides half the answer: an appointment stranded by a
	// reference re-point keeps an open window while refusing every candidate,
	// and reporting that as active would promise a fast path that is not there.
	capacity, err := q.k.ConversionPolicy.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market conversion policy: %v", err)
	}

	return &types.QueryConversionMandateResponse{
		Mandate: conversionMandate,
		Active:  conversionMandate.IsActive(capacity, uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
	}, nil
}

// quoteStatus maps a quote failure to a gRPC status. An ineligible asset is a precondition failure,
// not an internal one: the quote is well formed and the chain simply will not convert that
// denomination in that direction right now.
func quoteStatus(err error, computing string) error {
	switch {
	case errors.Is(err, types.ErrNoEffectivePrice),
		errors.Is(err, oracletypes.ErrStaleExchangeRate),
		errors.Is(err, types.ErrIneligibleAsset),
		errors.Is(err, assettypes.ErrAssetNotFound):
		return status.Errorf(codes.FailedPrecondition, "%s: %v", computing, err)
	case errors.Is(err, errortypes.ErrInvalidCoins),
		errors.Is(err, errortypes.ErrInvalidRequest),
		errors.Is(err, types.ErrRecursiveSwap),
		errors.Is(err, types.ErrZeroSwapCoin):
		return status.Errorf(codes.InvalidArgument, "%s: %v", computing, err)
	case errors.Is(err, types.ErrArithmeticOutOfRange), errors.Is(err, oracletypes.ErrConversionOutOfRange):
		return status.Errorf(codes.OutOfRange, "%s: %v", computing, err)
	default:
		return status.Errorf(codes.Internal, "%s: %v", computing, err)
	}
}
