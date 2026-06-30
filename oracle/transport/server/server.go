package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	gateway "github.com/cosmos/gogogateway"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/version"

	"noah/oracle/transport/types"
	oracletypes "noah/oracle/types"
)

var _ types.OracleServer = (*Server)(nil)

const DefaultServerShutdownTimeout = 3 * time.Second

type oracleProvider interface {
	IsRunning() bool
	GetPrices() oracletypes.Prices
	GetLastSyncTime() time.Time
}

// Server is the base implementation of the service.Server interface, this is meant to
// serve requests from a remote OracleClient.
type Server struct {
	types.UnimplementedOracleServer

	// expected implementation of the oracle
	o oracleProvider

	// underlying grpc-server -- serves all grpc requests
	grpcSrv *grpc.Server

	// grpc-gateway mux -- serves all http grpc proxy requests
	gatewayMux *runtime.ServeMux

	// underlying http server
	httpSrv *http.Server

	// closer to handle graceful closures from multiple go-routines
	*Closer

	// logger to log incoming requests
	logger log.Logger
}

// NewOracleServer returns a new instance of the OracleServer, given an implementation of the Oracle interface.
func NewOracleServer(o oracleProvider, logger log.Logger) *Server {
	os := &Server{
		o:      o,
		logger: logger.With("server", "oracle"),
	}
	os.Closer = NewCloser().WithCallback(func() {
		// if the server has been started, close it
		if os.httpSrv != nil {
			ctx, cf := context.WithTimeout(context.Background(), DefaultServerShutdownTimeout)
			os.httpSrv.Shutdown(ctx) // close HTTP server backing GRPC-gateway
			os.grpcSrv.Stop()        // close GRPC server serving listeners that have been routed to GRPC server
			cf()
		}
	})

	return os
}

// routeRequest determines if the incoming http request is a grpc or http request and routes to the proper handler.
func (os *Server) routeRequest(w http.ResponseWriter, r *http.Request) {
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
func (os *Server) StartServerWithListener(ctx context.Context, ln net.Listener) error {
	os.httpSrv = &http.Server{
		ReadHeaderTimeout: DefaultServerShutdownTimeout,
	}
	// create grpc server
	os.grpcSrv = grpc.NewServer()
	// register oracle server
	types.RegisterOracleServer(os.grpcSrv, os)

	// register the grpc-gateway
	// it handles the http request and dials the server endpoint with the grpc request
	os.gatewayMux = runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &gateway.JSONPb{
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
func (os *Server) StartServer(ctx context.Context, host, port string) error {
	addr := fmt.Sprintf("%s:%s", host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return os.StartServerWithListener(ctx, ln)
}

// Prices calls the underlying oracle's implementation of GetPrices. It defers to the ctx in the request, and errors if the context is cancelled
// for any reason, or if the oracle errors.
func (os *Server) Prices(ctx context.Context, req *types.OraclePricesRequest) (*types.OraclePricesResponse, error) {
	// check that the request is non-nil
	if req == nil {
		return nil, types.ErrNilRequest
	}

	os.logger.Debug("received request for prices")

	// check that oracle is running
	if !os.o.IsRunning() {
		os.logger.Error("oracle not running")
		return nil, types.ErrOracleNotRunning
	}

	type pricesResult struct {
		response *types.OraclePricesResponse
		err      error
	}

	resultCh := make(chan pricesResult, 1)

	// run the request in a goroutine, to unblock server + ctx cancellation
	go func() {
		// get the prices
		prices, err := ToReqPrices(os.o.GetPrices())
		if err != nil {
			resultCh <- pricesResult{
				err: fmt.Errorf("convert oracle prices: %w", err),
			}
			return
		}

		// get the latest timestamp of the latest update from the oracle
		timestamp := os.o.GetLastSyncTime()

		resultCh <- pricesResult{
			response: &types.OraclePricesResponse{
				Prices:    prices,
				Timestamp: timestamp,
				Version:   version.Version,
			},
		}
	}()

	// defer to context closure
	select {
	case <-ctx.Done():
		os.logger.Error("context cancelled")
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		return result.response, nil
	}
}

// Version returns the version of the oracle server.
func (os *Server) Version(_ context.Context, _ *types.OracleVersionRequest) (*types.OracleVersionResponse, error) {
	return &types.OracleVersionResponse{Version: version.Version}, nil
}

// Close closes the underlying oracle server, and blocks until all open requests have been satisfied.
func (os *Server) Close() error {
	// close + close server if necessary
	os.Closer.Close()
	return nil
}

// Done returns a channel that is closed when the oracle server is closed.
func (os *Server) Done() <-chan struct{} {
	return os.Closer.Done()
}
