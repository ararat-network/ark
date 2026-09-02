package telemetry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunHTTPServerWaitsForCancellationClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener := newBlockingListener()
	done := make(chan error, 1)
	go func() {
		done <- runHTTPServerWithListener(
			ctx,
			listener,
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			nil,
			"test",
		)
	}()
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("auxiliary HTTP server did not close")
	}
}

func TestRunHTTPServerReturnsServeFailure(t *testing.T) {
	serveErr := errors.New("accept failed")
	listener := &errorListener{err: serveErr}
	err := runHTTPServerWithListener(
		context.Background(),
		listener,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		nil,
		"test",
	)
	require.ErrorContains(t, err, serveErr.Error())
}

func TestRunHTTPServerRejectsEmptyAddress(t *testing.T) {
	err := RunHTTPServer(
		context.Background(),
		"",
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		nil,
		"test",
	)
	require.ErrorContains(t, err, "test address cannot be empty")
}

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *blockingListener) Addr() net.Addr {
	return testAddress("blocking")
}

type errorListener struct {
	err error
}

func (l *errorListener) Accept() (net.Conn, error) {
	return nil, l.err
}

func (l *errorListener) Close() error {
	return nil
}

func (l *errorListener) Addr() net.Addr {
	return testAddress("error")
}

type testAddress string

func (a testAddress) Network() string {
	return "test"
}

func (a testAddress) String() string {
	return string(a)
}
