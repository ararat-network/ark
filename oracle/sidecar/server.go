package sidecar

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	gateway "github.com/cosmos/gogogateway"
	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	sidecarinternal "noah/oracle/sidecar/internal"
	"noah/oracle/sidecar/runtime"
	"noah/oracle/types"
)

var _ types.OracleServer = (*Oracle)(nil)

// DefaultServerShutdownTimeout bounds graceful HTTP shutdown and request-header
// reads during sidecar transport startup/shutdown.
const DefaultServerShutdownTimeout = 3 * time.Second

// Oracle owns the sidecar transport process and implements the generated RPC service.
//
// Price fetching, provider lifecycle, and config updates are delegated to runtime;
// this type owns HTTP/gRPC serving, gateway routing, and process shutdown state.
type Oracle struct {
	types.UnimplementedOracleServer

	// runtime owns providers, resolver state, and cached price snapshots.
	runtime *runtime.Runtime

	// Transport components are installed during StartWithListener and read by
	// request routing or shutdown paths until markDone clears lifecycle state.
	grpcSrv    *grpc.Server
	gatewayMux *gatewayruntime.ServeMux
	httpSrv    *http.Server

	// logger is scoped once at construction and shared by transport/RPC paths.
	logger log.Logger

	// lifecycleMu guards cancel, transport pointers, closing/closed, and closeErr.
	// closeOnce makes Close idempotent; doneOnce owns the Done channel close.
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	doneCh      chan struct{}
	closing     bool
	closed      bool
	closeErr    error
	closeOnce   sync.Once
	doneOnce    sync.Once
}

// NewOracle constructs a sidecar process around a validated runtime.
func NewOracle(cfg runtime.Config, logger log.Logger, opts ...runtime.Option) (*Oracle, error) {
	runtimeOpts := make([]runtime.Option, 0, len(opts)+1)
	runtimeOpts = append(runtimeOpts, runtime.WithLogger(logger))
	runtimeOpts = append(runtimeOpts, opts...)
	r, err := runtime.NewRuntime(cfg, runtimeOpts...)
	if err != nil {
		return nil, err
	}

	o := &Oracle{
		runtime: r,
		logger:  logger.With("server", "oracle"),
		doneCh:  make(chan struct{}),
	}

	return o, nil
}

// routeRequest multiplexes h2c traffic between native gRPC and the HTTP gateway.
func (o *Oracle) routeRequest(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 2 && strings.HasPrefix(
		r.Header.Get("Content-Type"),
		"application/grpc",
	) {
		o.grpcSrv.ServeHTTP(w, r)
	} else {
		o.gatewayMux.ServeHTTP(w, r)
	}
}

// StartWithListener starts the runtime and transport stack on ln.
//
// The derived run context owns the runtime loop, HTTP server, and generated
// gateway client connection. Close and parent cancellation both cancel that
// context, so StartWithListener returns only after the sidecar has stopped or an
// unrecoverable runtime/transport error escapes.
func (o *Oracle) StartWithListener(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	httpSrv := &http.Server{
		ReadHeaderTimeout: DefaultServerShutdownTimeout,
	}
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(o.recoverUnaryPanic))
	types.RegisterOracleServer(grpcSrv, o)

	runCtx, cancel := context.WithCancel(ctx)

	gatewayMux := gatewayruntime.NewServeMux(
		gatewayruntime.WithMarshalerOption(gatewayruntime.MIMEWildcard, &gateway.JSONPb{
			EmitDefaults: true,
			Indent:       "",
			OrigName:     true,
		}),
	)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy()}
	err := types.RegisterOracleHandlerFromEndpoint(runCtx, gatewayMux, ln.Addr().String(), opts)
	if err != nil {
		cancel()
		return err
	}

	router := http.NewServeMux()
	router.HandleFunc("/", o.routeRequest)
	httpSrv.Handler = h2c.NewHandler(router, &http2.Server{})

	if err := o.startLifecycle(cancel, httpSrv, grpcSrv, gatewayMux); err != nil {
		cancel()
		return err
	}
	defer cancel()
	defer o.markDone()

	eg, ctx := errgroup.WithContext(runCtx)

	eg.Go(func() error {
		return sidecarinternal.RunRecovering("oracle runtime", func() error {
			err := o.runtime.Start(ctx)
			if ctx.Err() != nil && errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		})
	})

	eg.Go(func() error {
		return sidecarinternal.RunRecovering("oracle shutdown watcher", func() error {
			<-ctx.Done()
			o.logger.Info("context cancelled, closing oracle transport")
			if err := o.Close(); err != nil {
				o.logger.Error("failed to shutdown oracle transport", "error", err)
			}
			return nil
		})
	})

	eg.Go(func() error {
		return sidecarinternal.RunRecovering("oracle transport", func() error {
			host, port, err := net.SplitHostPort(ln.Addr().String())
			if err != nil {
				return errors.New("[grpc server]: invalid listener address")
			}
			o.logger.Info("starting grpc server", "host", host, "port", port)

			err = httpSrv.Serve(ln)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("[grpc server]: error serving: %w", err)
			}

			return nil
		})
	})

	return eg.Wait()
}

// Start listens on host:port and delegates lifecycle ownership to StartWithListener.
func (o *Oracle) Start(ctx context.Context, host, port string) error {
	addr := fmt.Sprintf("%s:%s", host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return o.startOwnedListener(ctx, ln)
}

// startOwnedListener starts a listener created by Start and closes it if the
// transport fails before the server takes ownership.
func (o *Oracle) startOwnedListener(ctx context.Context, ln net.Listener) error {
	err := o.StartWithListener(ctx, ln)
	if err == nil {
		return nil
	}
	if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
		return errors.Join(err, fmt.Errorf("close oracle listener: %w", closeErr))
	}
	return err
}
