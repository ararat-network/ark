package sidecar

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/tlsconfig"
	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
	"github.com/ararat-network/ark/pricefeed/api"
)

// tlsServerName is the name the test certificates carry and clients dial.
const tlsServerName = "pricefeed"

func TestNewServerRejectsIncompleteTLS(t *testing.T) {
	srv, err := newServer(
		&recordingOracleService{},
		log.NewNopLogger(),
		"127.0.0.1:0",
		tlsconfig.Server{Mode: tlsconfig.TLS, KeyFile: "sidecar.key"},
	)

	require.Nil(t, srv)
	require.ErrorContains(t, err, "oracle server tls")
}

// startTLSServer serves service on an in-memory listener with files and
// returns the listener for clients to dial.
func startTLSServer(
	t *testing.T,
	service api.PriceFeedServer,
	files tlsconfig.Server,
	logger log.Logger,
) *bufconn.Listener {
	t.Helper()

	srv, err := newServer(service, logger, "127.0.0.1:0", files)
	require.NoError(t, err)
	listener := bufconn.Listen(1024 * 1024)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.serve(ctx, listener)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Error("server did not stop")
		}
	})

	return listener
}

func dialGRPC(t *testing.T, listener *bufconn.Listener, creds credentials.TransportCredentials) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(
		"passthrough:///"+tlsServerName,
		grpc.WithTransportCredentials(creds),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func gatewayGet(t *testing.T, listener *bufconn.Listener, clientTLS *tls.Config, path string) (int, string) {
	t.Helper()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: clientTLS,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return listener.DialContext(ctx)
			},
		},
		Timeout: time.Second,
	}
	t.Cleanup(client.CloseIdleConnections)

	resp, err := client.Get("https://" + tlsServerName + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(body)
}

func versionRPC(conn *grpc.ClientConn) (*api.VersionResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return api.NewPriceFeedClient(conn).Version(ctx, &api.VersionRequest{})
}

func TestServerServesTLS(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "sidecar", tlsServerName)
	listener := startTLSServer(
		t,
		&recordingOracleService{version: "v9"},
		tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile},
		log.NewNopLogger(),
	)
	clientTLS, err := tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile, ServerName: tlsServerName}.Load()
	require.NoError(t, err)

	t.Run("gateway", func(t *testing.T) {
		status, body := gatewayGet(t, listener, clientTLS.Config, "/ark/pricefeed/v1/version")

		require.Equal(t, http.StatusOK, status)
		require.JSONEq(t, `{"version":"v9"}`, body)
	})

	t.Run("grpc", func(t *testing.T) {
		resp, err := versionRPC(dialGRPC(t, listener, credentials.NewTLS(clientTLS.Config)))

		require.NoError(t, err)
		require.Equal(t, "v9", resp.Version)
	})

	t.Run("plaintext client is refused", func(t *testing.T) {
		_, err := versionRPC(dialGRPC(t, listener, insecure.NewCredentials()))

		require.Error(t, err)
	})

	t.Run("client distrusting the server is refused", func(t *testing.T) {
		other := tlstestutil.NewAuthority(t)
		distrusting, err := tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: other.CAFile, ServerName: tlsServerName}.Load()
		require.NoError(t, err)

		_, err = versionRPC(dialGRPC(t, listener, credentials.NewTLS(distrusting.Config)))

		require.Error(t, err)
	})
}

// With a client CA the public listener admits only clients that present a
// certificate it signed. The gateway keeps working because its hop to the
// gRPC server is in-process and never crosses that listener.
func TestServerRequiresClientCertificate(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "sidecar", tlsServerName)
	clientCert, clientKey := ca.Issue(t, "node", "node")
	listener := startTLSServer(
		t,
		&recordingOracleService{version: "v9"},
		tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile, ClientCAFile: ca.CAFile},
		log.NewNopLogger(),
	)
	withCert, err := tlsconfig.Client{
		Mode:       tlsconfig.TLS,
		CAFile:     ca.CAFile,
		ServerName: tlsServerName,
		CertFile:   clientCert,
		KeyFile:    clientKey,
	}.Load()
	require.NoError(t, err)
	withoutCert, err := tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile, ServerName: tlsServerName}.Load()
	require.NoError(t, err)

	t.Run("gateway with certificate", func(t *testing.T) {
		status, body := gatewayGet(t, listener, withCert.Config, "/ark/pricefeed/v1/version")

		require.Equal(t, http.StatusOK, status)
		require.JSONEq(t, `{"version":"v9"}`, body)
	})

	t.Run("grpc with certificate", func(t *testing.T) {
		resp, err := versionRPC(dialGRPC(t, listener, credentials.NewTLS(withCert.Config)))

		require.NoError(t, err)
		require.Equal(t, "v9", resp.Version)
	})

	t.Run("gateway without certificate", func(t *testing.T) {
		transport := &http.Transport{
			TLSClientConfig: withoutCert.Config,
			DialContext:     func(ctx context.Context, _, _ string) (net.Conn, error) { return listener.DialContext(ctx) },
		}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: time.Second}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+tlsServerName+"/ark/pricefeed/v1/version", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err)
	})

	t.Run("grpc without certificate", func(t *testing.T) {
		_, err := versionRPC(dialGRPC(t, listener, credentials.NewTLS(withoutCert.Config)))

		require.Error(t, err)
	})
}

func TestServerRequiresExplicitRemoteTransport(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		bad        bool
	}{
		{"default", "", true}, {"local", tlsconfig.Local, true}, {"explicit plaintext", tlsconfig.Plaintext, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := newServer(&recordingOracleService{}, log.NewNopLogger(), "0.0.0.0:8080", tlsconfig.Server{Mode: tc.mode})
			if tc.bad {
				require.ErrorContains(t, err, "requires mode tls")
				require.Nil(t, srv)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// A failed handshake is reported through the sidecar's logger, not stderr.
func TestServerLogsHandshakeFailures(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "sidecar", tlsServerName)
	logs := &lockedBuffer{}
	listener := startTLSServer(
		t,
		&recordingOracleService{version: "v9"},
		tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile},
		log.NewLogger(logs, log.ColorOption(false)),
	)
	other := tlstestutil.NewAuthority(t)
	distrusting, err := tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: other.CAFile, ServerName: tlsServerName}.Load()
	require.NoError(t, err)

	_, err = versionRPC(dialGRPC(t, listener, credentials.NewTLS(distrusting.Config)))
	require.Error(t, err)

	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), "TLS handshake error")
	}, time.Second, 10*time.Millisecond)
	require.Contains(t, logs.String(), "component=transport")
}

// lockedBuffer collects log output written from server goroutines.
type lockedBuffer struct {
	mut sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mut.Lock()
	defer b.mut.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mut.Lock()
	defer b.mut.Unlock()

	return b.buf.String()
}
