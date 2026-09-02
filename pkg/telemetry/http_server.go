package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/log/v2"
)

// DefaultReadHeaderTimeout bounds how long an auxiliary server waits for a
// request header before dropping the connection.
const DefaultReadHeaderTimeout = 3 * time.Second

// RunHTTPServer creates and owns the listener for an auxiliary HTTP endpoint
// and serves until ctx is cancelled. It closes the listener even if
// validation fails before Serve takes ownership.
func RunHTTPServer(
	ctx context.Context,
	address string,
	handler http.Handler,
	logger log.Logger,
	name string,
) (err error) {
	if strings.TrimSpace(address) == "" {
		return fmt.Errorf("%s address cannot be empty", name)
	}

	ln, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("starting %s listener: %w", name, err)
	}
	defer func() {
		err = errors.Join(err, normaliseHTTPServerError(ln.Close()))
	}()

	return runHTTPServerWithListener(ctx, ln, handler, logger, name)
}

// runHTTPServerWithListener serves until cancellation or failure and takes
// responsibility for closing the supplied listener.
func runHTTPServerWithListener(
	ctx context.Context,
	ln net.Listener,
	handler http.Handler,
	logger log.Logger,
	name string,
) error {
	if ctx == nil {
		return fmt.Errorf("%s context cannot be nil", name)
	}
	if ln == nil {
		return fmt.Errorf("%s listener cannot be nil", name)
	}
	if handler == nil {
		return fmt.Errorf("%s handler cannot be nil", name)
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
	}
	logger.Info("starting "+name+" server", "address", ln.Addr().String())

	// Cancellation and Serve may finish concurrently, so both cleanup paths
	// share a single Close call and result.
	var (
		closeOnce sync.Once
		closeErr  error
	)
	closeServer := func() {
		closeErr = normaliseHTTPServerError(server.Close())
	}
	stopClose := context.AfterFunc(ctx, func() {
		closeOnce.Do(closeServer)
	})

	serveErr := normaliseHTTPServerError(server.Serve(ln))
	stopClose()
	closeOnce.Do(closeServer)
	err := errors.Join(closeErr, serveErr)
	if err != nil {
		logger.Error(name+" server failed", "error", err)
	}
	return err
}

func normaliseHTTPServerError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
