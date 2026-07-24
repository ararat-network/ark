package sidecar

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/version"

	sidecarinternal "ark/oracle/sidecar/internal"
	sidecartypes "ark/oracle/sidecar/types"
	"ark/oracle/types"
	"ark/pkg/encoding"
)

// Prices returns the runtime's latest cached denom prices.
//
// This RPC does not fetch providers. It snapshots runtime cache state, projects
// it into the generated transport shape, and leaves ongoing fetch work to the
// runtime loop.
func (o *Oracle) Prices(ctx context.Context, req *types.OraclePricesRequest) (*types.OraclePricesResponse, error) {
	// check that the request is non-nil
	if req == nil {
		return nil, ErrNilRequest
	}

	o.logger.Debug("received request for prices")

	if !o.runtime.IsRunning() {
		o.logger.Error("oracle not running")
		return nil, ErrOracleNotRunning
	}

	// The cache snapshot is local and synchronous; cancellation only gates entry
	// into that read instead of spawning request-scoped work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	snapshot := o.runtime.GetPriceSnapshot()
	prices, err := toReqPrices(snapshot.Prices)
	if err != nil {
		return nil, fmt.Errorf("converting oracle prices: %w", err)
	}

	return &types.OraclePricesResponse{
		Prices:    prices,
		Timestamp: snapshot.Timestamp,
		Version:   version.Version,
	}, nil
}

// toReqPrices encodes public denom prices into the generated transport payload.
// Runtime already projects pair prices to active denoms, so this conversion
// accepts denom-keyed prices and rejects nil values at the RPC boundary.
func toReqPrices(prices sidecartypes.DenomPrices) (map[string][]byte, error) {
	result := make(map[string][]byte, len(prices))

	for ticker, price := range prices {
		if price == nil {
			return nil, fmt.Errorf("nil price for %s", ticker)
		}
		if err := sidecartypes.ValidatePrice(price); err != nil {
			return nil, fmt.Errorf("invalid price for %s: %w", ticker, err)
		}

		rate, err := math.LegacyNewDecFromStr(price.Text('f', math.LegacyPrecision))
		if err != nil {
			return nil, fmt.Errorf("convert price %s: %w", ticker, err)
		}
		rawRate, err := encoding.EncodeLegacyDec(rate)
		if err != nil {
			return nil, fmt.Errorf("encoding rate: %w", err)
		}

		result[ticker] = rawRate
	}

	return result, nil
}

// Version returns the version of the oracle server.
func (o *Oracle) Version(_ context.Context, _ *types.OracleVersionRequest) (*types.OracleVersionResponse, error) {
	return &types.OracleVersionResponse{Version: version.Version}, nil
}

// recoverUnaryPanic is the RPC boundary middleware.
//
// Panics in request handlers are returned as internal gRPC errors so one bad
// request does not terminate the sidecar process. Normal handler errors are
// mapped to explicit gRPC status codes before they leave the sidecar.
func (s *server) recoverUnaryPanic(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (_ any, err error) {
	defer sidecarinternal.HandlePanic("oracle rpc", func(panicErr error) {
		method := ""
		if info != nil {
			method = info.FullMethod
		}
		s.logger.Error("oracle rpc panicked", "method", method, "error", panicErr)
		err = status.Error(codes.Internal, "oracle rpc panicked")
	})

	resp, err := handler(ctx, req)
	return resp, rpcStatusError(err)
}

// rpcStatusError maps sidecar-domain handler errors at the gRPC boundary.
func rpcStatusError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}

	switch {
	case errors.Is(err, ErrNilRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrOracleNotRunning):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
