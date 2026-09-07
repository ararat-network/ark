package sidecar

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	sidecarmetrics "github.com/ararat-network/ark/pricefeed/sidecar/metrics"
)

// unaryMetrics records every unary RPC the sidecar serves. It sits outside
// the panic recovery in the chain, so a recovered panic is counted under the
// Internal status it was mapped to.
func unaryMetrics(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	resp, err := handler(ctx, req)

	method := ""
	if info != nil {
		method = info.FullMethod
	}
	sidecarmetrics.RecordRPC(ctx, method, status.Code(err).String())
	return resp, err
}
