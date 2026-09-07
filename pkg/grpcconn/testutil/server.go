// Package testutil serves gRPC behind TLS for transport tests.
package testutil

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/ararat-network/ark/pkg/tlsconfig"
)

// Serve serves the services register adds, on a loopback TCP listener behind
// tls or in plaintext when tls is not enabled, and returns the address to
// dial. The server stops when the test ends.
func Serve(t *testing.T, tls tlsconfig.Server, register func(*grpc.Server)) string {
	t.Helper()

	cfg, err := tls.Load()
	require.NoError(t, err)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var opts []grpc.ServerOption
	if cfg.Config != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(cfg.Config)))
	}
	server := grpc.NewServer(opts...)
	register(server)
	go func() {
		_ = server.Serve(ln)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = ln.Close()
	})

	return ln.Addr().String()
}
