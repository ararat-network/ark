package sidecar

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	gateway "github.com/cosmos/gogogateway"
	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/version"

	"noah/oracle/sidecar/runtime"
	"noah/oracle/types"
)

var _ types.OracleServer = (*Oracle)(nil)

const DefaultServerShutdownTimeout = 3 * time.Second

// Oracle implements the oracle sidecar process and RPC service.
type Oracle struct {
	types.UnimplementedOracleServer

	// expected implementation of the oracle
	runtime *runtime.Runtime

	// underlying grpc-server -- serves all grpc requests
	grpcSrv *grpc.Server

	// grpc-gateway mux -- serves all http grpc proxy requests
	gatewayMux *gatewayruntime.ServeMux

	// underlying http server
	httpSrv *http.Server

	// closer to handle graceful closures from multiple go-routines
	*Closer

	// logger to log incoming requests
	logger log.Logger
}

// NewOracleServer returns a new instance of the OracleServer, given an implementation of the Oracle interface.
func NewOracleServer(runtime *runtime.Runtime, logger log.Logger) *Oracle {
	os := &Oracle{
		runtime: runtime,
		logger:  logger.With("server", "oracle"),
	}
	os.initCloser()

	return os
}

func (os *Oracle) initCloser() {
	os.Closer = NewCloser().WithCallback(func() {
		// if the server has been started, close it
		if os.httpSrv != nil {
			ctx, cf := context.WithTimeout(context.Background(), DefaultServerShutdownTimeout)
			os.httpSrv.Shutdown(ctx) // close HTTP server backing GRPC-gateway
			os.grpcSrv.Stop()        // close GRPC server serving listeners that have been routed to GRPC server
			cf()
		}
	})
}

// routeRequest determines if the incoming http request is a grpc or http request and routes to the proper handler.
func (os *Oracle) routeRequest(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 2 && strings.HasPrefix(
		r.Header.Get("Content-Type"), "application/grpc") {

		os.grpcSrv.ServeHTTP(w, r)
	} else {
		os.gatewayMux.ServeHTTP(w, r)
	}
}

// StartServerWithListener starts the oracle gRPC server with a given listener. The server is killed on any errors from the listener, or if ctx is cancelled.
// This method returns an error via any failure from the listener. This is a blocking call, i.e. until the server is closed or the server errors,
// this method will block.
func (os *Oracle) StartServerWithListener(ctx context.Context, ln net.Listener) error {
	os.httpSrv = &http.Server{
		ReadHeaderTimeout: DefaultServerShutdownTimeout,
	}
	// create grpc server
	os.grpcSrv = grpc.NewServer()
	// register oracle server
	types.RegisterOracleServer(os.grpcSrv, os)

	// register the grpc-gateway
	// it handles the http request and dials the server endpoint with the grpc request
	os.gatewayMux = gatewayruntime.NewServeMux(
		gatewayruntime.WithMarshalerOption(gatewayruntime.MIMEWildcard, &gateway.JSONPb{
			EmitDefaults: true,
			Indent:       "",
			OrigName:     true,
		}),
	)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy()}
	err := types.RegisterOracleHandlerFromEndpoint(ctx, os.gatewayMux, ln.Addr().String(), opts)
	if err != nil {
		return err
	}

	router := http.NewServeMux()
	router.HandleFunc("/", os.routeRequest)
	os.httpSrv.Handler = h2c.NewHandler(router, &http2.Server{})

	eg, ctx := errgroup.WithContext(ctx)

	// listen for ctx cancellation
	eg.Go(func() error {
		// if the context is closed, close the server + oracle
		<-ctx.Done()
		os.logger.Info("context cancelled, closing oracle")

		_ = os.Close()
		return nil
	})

	// start the server
	eg.Go(func() error {
		// serve, and return any errors
		host, port, err := net.SplitHostPort(ln.Addr().String())
		if err != nil {
			return errors.New("[grpc server]: invalid listener address")
		}
		os.logger.Info("starting grpc server", "host", host, "port", port)

		err = os.httpSrv.Serve(ln)
		if err != nil {
			return fmt.Errorf("[grpc server]: error serving: %w", err)
		}

		return nil
	})

	// wait for everything to finish
	return eg.Wait()
}

// StartServer starts the oracle gRPC server on the given host and port. The server is killed on any errors from the listener, or if ctx is cancelled.
// This method returns an error via any failure from the listener. This is a blocking call, i.e. until the server is closed or the server errors,
// this method will block.
func (os *Oracle) StartServer(ctx context.Context, host, port string) error {
	addr := fmt.Sprintf("%s:%s", host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return os.StartServerWithListener(ctx, ln)
}

// Prices returns the runtime's latest cached oracle prices.
func (os *Oracle) Prices(ctx context.Context, req *types.OraclePricesRequest) (*types.OraclePricesResponse, error) {
	// check that the request is non-nil
	if req == nil {
		return nil, types.ErrNilRequest
	}

	os.logger.Debug("received request for prices")

	// check that oracle is running
	if !os.runtime.IsRunning() {
		os.logger.Error("oracle not running")
		return nil, types.ErrOracleNotRunning
	}

	prices, err := ToReqPrices(os.runtime.GetPrices())
	if err != nil {
		return nil, fmt.Errorf("converting oracle prices: %w", err)
	}

	// get the latest timestamp of the latest update from the oracle
	timestamp := os.runtime.GetLastSyncTime()

	return &types.OraclePricesResponse{
		Prices:    prices,
		Timestamp: timestamp,
		Version:   version.Version,
	}, nil
}

// Version returns the version of the oracle server.
func (os *Oracle) Version(_ context.Context, _ *types.OracleVersionRequest) (*types.OracleVersionResponse, error) {
	return &types.OracleVersionResponse{Version: version.Version}, nil
}

// Close closes the underlying oracle server, and blocks until all open requests have been satisfied.
func (os *Oracle) Close() error {
	// close + close server if necessary
	os.Closer.Close()
	return nil
}

// Done returns a channel that is closed when the oracle server is closed.
func (os *Oracle) Done() <-chan struct{} {
	return os.Closer.Done()
}
