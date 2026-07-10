package sidecar

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	sidecarinternal "ark/oracle/sidecar/internal"
	"ark/oracle/types"
)

type adminServer struct {
	logger  log.Logger
	address string
	grpcSrv *grpc.Server
}

type adminService struct {
	types.UnimplementedOracleAdminServer

	oracle *Oracle
}

func newAdminServer(oracle *Oracle, logger log.Logger, address string) (*adminServer, error) {
	if oracle == nil {
		return nil, errors.New("oracle admin service is nil")
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}

	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("oracle admin server address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("oracle admin host %q must be a loopback IP address", host)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return nil, fmt.Errorf("oracle admin server address: %w", err)
	}

	s := &adminServer{
		logger:  logger.With("component", "admin_transport"),
		address: net.JoinHostPort(host, port),
	}
	s.grpcSrv = grpc.NewServer(grpc.UnaryInterceptor(s.recoverUnaryPanic))
	types.RegisterOracleAdminServer(s.grpcSrv, &adminService{oracle: oracle})

	return s, nil
}

func (s *adminServer) run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	ln, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen oracle admin server: %w", err)
	}
	return s.serve(ctx, ln)
}

// serve owns ln and serves the process-local administration transport until
// ctx is cancelled or serving fails.
func (s *adminServer) serve(ctx context.Context, ln net.Listener) error {
	stopClose := context.AfterFunc(ctx, func() {
		_ = ln.Close()
	})
	defer func() {
		stopClose()
		_ = ln.Close()
		s.grpcSrv.Stop()
	}()

	s.logger.Info("starting oracle admin server", "address", ln.Addr().String())
	if err := s.grpcSrv.Serve(ln); err != nil && ctx.Err() == nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("serve oracle admin requests: %w", err)
	}

	return nil
}

// ReloadConfig handles the process-local RPC by delegating the serialized file
// reload and runtime replacement to the owning Oracle.
func (s *adminService) ReloadConfig(
	ctx context.Context,
	req *types.OracleReloadConfigRequest,
) (*types.OracleReloadConfigResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}
	if s.oracle == nil {
		return nil, status.Error(codes.FailedPrecondition, "oracle is unavailable")
	}
	s.oracle.logger.Info("reloading oracle config")
	if err := s.oracle.ReloadConfig(ctx); err != nil {
		s.oracle.logger.Error("oracle config reload failed", "error", err)
		return nil, err
	}
	s.oracle.logger.Info("oracle config reloaded")

	return &types.OracleReloadConfigResponse{}, nil
}

// recoverUnaryPanic contains panics at the administration RPC boundary and maps
// ordinary handler errors to explicit gRPC status codes.
func (s *adminServer) recoverUnaryPanic(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (_ any, err error) {
	defer sidecarinternal.HandlePanic("oracle admin rpc", func(panicErr error) {
		method := ""
		if info != nil {
			method = info.FullMethod
		}
		s.logger.Error("oracle admin rpc panicked", "method", method, "error", panicErr)
		err = status.Error(codes.Internal, "oracle admin rpc panicked")
	})

	resp, err := handler(ctx, req)
	return resp, adminRPCStatusError(err)
}

// adminRPCStatusError maps administration-domain errors at the gRPC boundary.
func adminRPCStatusError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}

	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.FailedPrecondition, err.Error())
	}
}
