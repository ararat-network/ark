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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/api"
)

// DefaultServerReadHeaderTimeout bounds how long the public server waits to
// read each request's headers.
const DefaultServerReadHeaderTimeout = 3 * time.Second

type server struct {
	logger  log.Logger
	address string

	grpcSrv    *grpc.Server
	gatewayMux *gatewayruntime.ServeMux
	httpSrv    *http.Server
}

func newServer(service api.PriceFeedServer, logger log.Logger, address string) (*server, error) {
	if service == nil {
		return nil, errors.New("oracle service is nil")
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("oracle server address: %w", err)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return nil, fmt.Errorf("oracle server address: %w", err)
	}

	s := &server{
		logger:  logger.With("component", "transport"),
		address: net.JoinHostPort(host, port),
	}
	s.grpcSrv = grpc.NewServer(grpc.UnaryInterceptor(s.recoverUnaryPanic))
	api.RegisterPriceFeedServer(s.grpcSrv, service)

	s.gatewayMux = gatewayruntime.NewServeMux(
		gatewayruntime.WithMarshalerOption(gatewayruntime.MIMEWildcard, &gateway.JSONPb{
			EmitDefaults: true,
			Indent:       "",
			OrigName:     true,
		}),
	)
	router := http.NewServeMux()
	router.HandleFunc("/", s.routeRequest)
	s.httpSrv = &http.Server{
		Handler:           h2c.NewHandler(router, &http2.Server{}),
		ReadHeaderTimeout: DefaultServerReadHeaderTimeout,
	}
	return s, nil
}

func (s *server) run(ctx context.Context) (err error) {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	ln, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen oracle server: %w", err)
	}
	return s.serve(ctx, ln)
}

// serve owns ln and serves the public HTTP, gRPC, and gateway transport until
// ctx is cancelled or serving fails. Extra dial options let the gateway
// self-dial through the listener's transport.
func (s *server) serve(
	ctx context.Context,
	ln net.Listener,
	extraDialOptions ...grpc.DialOption,
) (err error) {
	runCtx, cancel := context.WithCancel(ctx)
	stopClose := context.AfterFunc(runCtx, func() {
		_ = ln.Close()
	})
	defer func() {
		stopClose()
		cancel()
		_ = ln.Close()
		err = errors.Join(err, s.httpSrv.Close())
		s.grpcSrv.Stop()
	}()

	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	}
	dialOptions = append(dialOptions, extraDialOptions...)
	if err := api.RegisterPriceFeedHandlerFromEndpoint(
		runCtx,
		s.gatewayMux,
		ln.Addr().String(),
		dialOptions,
	); err != nil {
		return fmt.Errorf("register oracle gateway handler: %w", err)
	}

	s.logger.Info("starting oracle server", "address", ln.Addr().String())
	err = s.httpSrv.Serve(ln)
	if err != nil && runCtx.Err() == nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("serve oracle requests: %w", err)
	}

	return nil
}

// routeRequest multiplexes h2c traffic between native gRPC and the HTTP gateway.
func (s *server) routeRequest(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 2 && strings.HasPrefix(
		r.Header.Get("Content-Type"),
		"application/grpc",
	) {
		s.grpcSrv.ServeHTTP(w, r)
	} else {
		s.gatewayMux.ServeHTTP(w, r)
	}
}
