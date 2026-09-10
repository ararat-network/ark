package sidecar

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	stdlog "log"
	"net"
	"net/http"
	"strings"
	"time"

	gateway "github.com/cosmos/gogogateway"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	"github.com/ararat-network/ark/pricefeed/api"
)

// DefaultServerReadHeaderTimeout bounds how long the public server waits to
// read each request's headers.
const DefaultServerReadHeaderTimeout = 3 * time.Second

const (
	// gatewayEndpoint names the in-memory hop from the gateway to the gRPC
	// server; the dialer ignores it.
	gatewayEndpoint = "passthrough:///pricefeed-gateway"
	// gatewayBufferSize sizes that hop's connection buffers.
	gatewayBufferSize = 1 << 20
)

type server struct {
	logger  log.Logger
	address string
	// material holds the fixed TLS policy and rotating listener identity.
	material *tlsconfig.Material

	grpcSrv    *grpc.Server
	gatewayMux *runtime.ServeMux
	httpSrv    *http.Server
}

func newServer(
	service api.PriceFeedServer,
	logger log.Logger,
	address string,
	tlsFiles tlsconfig.Server,
) (*server, error) {
	if service == nil {
		return nil, errors.New("oracle service is nil")
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}
	logger = logger.With("component", "transport")
	host, port, err := grpcconn.ListenAddress(address)
	if err != nil {
		return nil, fmt.Errorf("oracle server address: %w", err)
	}
	if err := grpcconn.ValidateTargets(tlsFiles.Mode, address); err != nil {
		return nil, err
	}
	material, err := tlsFiles.Load()
	if err != nil {
		return nil, fmt.Errorf("oracle server tls: %w", err)
	}
	if material.Config == nil && !grpcconn.Loopback(address) {
		logger.Warn(
			"serving plaintext off loopback; explicit plaintext mode selected",
			"address", address,
		)
	}

	s := &server{
		logger:   logger,
		address:  net.JoinHostPort(host, port),
		material: material,
	}
	s.grpcSrv = grpc.NewServer(grpc.ChainUnaryInterceptor(unaryMetrics, s.recoverUnaryPanic))
	api.RegisterPriceFeedServer(s.grpcSrv, service)

	s.gatewayMux = runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &gateway.JSONPb{
			EmitDefaults: true,
			Indent:       "",
			OrigName:     true,
		}),
	)
	router := http.NewServeMux()
	router.HandleFunc("/", s.routeRequest)
	// The sidecar multiplexes gRPC and the gateway on one port: HTTP/2 by
	// ALPN over TLS, and in plaintext the unencrypted HTTP/2 that net/http
	// serves natively through this field.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	if material.Config != nil {
		protocols.SetHTTP2(true)
	} else {
		protocols.SetUnencryptedHTTP2(true)
	}
	s.httpSrv = &http.Server{
		Handler:           router,
		Protocols:         protocols,
		ReadHeaderTimeout: DefaultServerReadHeaderTimeout,
		// net/http's own lines, TLS handshake failures among them, would
		// otherwise go to stderr.
		ErrorLog: stdlog.New(httpErrorLog{s.logger}, "", 0),
	}
	return s, nil
}

// httpErrorLog carries net/http's error lines into the structured log.
type httpErrorLog struct{ logger log.Logger }

func (l httpErrorLog) Write(p []byte) (int, error) {
	l.logger.Warn(strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
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

// serve owns ln, wraps it in the listener's TLS when there is any, and serves
// the public HTTP, gRPC, and gateway transport until ctx is cancelled or
// serving fails. The gateway reaches the gRPC server through an in-memory
// listener: the hop never leaves the process, so the public listener's TLS
// does not cover it and a client-certificate requirement cannot lock the
// gateway out.
func (s *server) serve(ctx context.Context, ln net.Listener) (err error) {
	stopCertificates := s.material.Start(ctx, s.logger)
	defer stopCertificates()
	if s.material.Config != nil {
		cfg := s.material.Config.Clone()
		cfg.NextProtos = []string{"h2", "http/1.1"}
		ln = tls.NewListener(ln, cfg)
	}
	runCtx, cancel := context.WithCancel(ctx)
	stopClose := context.AfterFunc(runCtx, func() {
		_ = ln.Close()
	})
	local := bufconn.Listen(gatewayBufferSize)
	localDone := make(chan struct{})
	go func() {
		defer close(localDone)
		_ = s.grpcSrv.Serve(local)
	}()
	defer func() {
		stopClose()
		cancel()
		_ = ln.Close()
		err = errors.Join(err, s.httpSrv.Close())
		s.grpcSrv.Stop()
		_ = local.Close()
		<-localDone
	}()

	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return local.DialContext(ctx)
		}),
	}
	if err := api.RegisterPriceFeedHandlerFromEndpoint(
		runCtx,
		s.gatewayMux,
		gatewayEndpoint,
		dialOptions,
	); err != nil {
		return fmt.Errorf("register oracle gateway handler: %w", err)
	}

	s.logger.Info("starting oracle server", "address", ln.Addr().String(), "tls", s.material.Config != nil)
	err = s.httpSrv.Serve(ln)
	if err != nil && runCtx.Err() == nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("serve oracle requests: %w", err)
	}

	return nil
}

// routeRequest sends HTTP/2 gRPC to the gRPC server and everything else to
// the gateway, whichever transport carried it.
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
