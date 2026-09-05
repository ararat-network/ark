package tlsconfig_test

import (
	"crypto/tls"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/tlsconfig"
	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
)

func TestClientValidate(t *testing.T) {
	tests := []struct {
		name    string
		client  tlsconfig.Client
		wantErr string
	}{
		{name: "plaintext", client: tlsconfig.Client{}},
		{name: "ca only", client: tlsconfig.Client{CAFile: "ca.pem"}},
		{name: "ca with server name", client: tlsconfig.Client{CAFile: "ca.pem", ServerName: "sidecar"}},
		{
			name:   "ca with client certificate",
			client: tlsconfig.Client{CAFile: "ca.pem", CertFile: "c.pem", KeyFile: "c.key"},
		},
		{
			name:    "cert without key",
			client:  tlsconfig.Client{CAFile: "ca.pem", CertFile: "c.pem"},
			wantErr: "set together",
		},
		{
			name:    "key without cert",
			client:  tlsconfig.Client{CAFile: "ca.pem", KeyFile: "c.key"},
			wantErr: "set together",
		},
		{
			name:    "certificate without ca",
			client:  tlsconfig.Client{CertFile: "c.pem", KeyFile: "c.key"},
			wantErr: "requires a ca file",
		},
		{
			name:    "server name without ca",
			client:  tlsconfig.Client{ServerName: "sidecar"},
			wantErr: "requires a ca file",
		},
		{name: "blank ca is plaintext", client: tlsconfig.Client{CAFile: "  "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.client.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestServerValidate(t *testing.T) {
	tests := []struct {
		name    string
		server  tlsconfig.Server
		wantErr string
	}{
		{name: "plaintext", server: tlsconfig.Server{}},
		{name: "cert and key", server: tlsconfig.Server{CertFile: "s.pem", KeyFile: "s.key"}},
		{
			name:   "cert, key, and client ca",
			server: tlsconfig.Server{CertFile: "s.pem", KeyFile: "s.key", ClientCAFile: "ca.pem"},
		},
		{name: "cert without key", server: tlsconfig.Server{CertFile: "s.pem"}, wantErr: "set together"},
		{name: "key without cert", server: tlsconfig.Server{KeyFile: "s.key"}, wantErr: "set together"},
		{
			name:    "client ca without cert",
			server:  tlsconfig.Server{ClientCAFile: "ca.pem"},
			wantErr: "requires a cert file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.server.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestClientLoad(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "client", "client")
	emptyPEM := filepath.Join(t.TempDir(), "empty.pem")
	require.NoError(t, os.WriteFile(emptyPEM, []byte("not a certificate\n"), 0o600))

	t.Run("plaintext loads to nil", func(t *testing.T) {
		cfg, err := tlsconfig.Client{}.Load()
		require.NoError(t, err)
		require.Nil(t, cfg)
	})

	t.Run("ca and server name", func(t *testing.T) {
		cfg, err := tlsconfig.Client{CAFile: ca.CAFile, ServerName: " sidecar "}.Load()
		require.NoError(t, err)
		require.NotNil(t, cfg.RootCAs)
		require.Equal(t, "sidecar", cfg.ServerName)
		require.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
		require.Nil(t, cfg.GetClientCertificate)
	})

	t.Run("client certificate", func(t *testing.T) {
		cfg, err := tlsconfig.Client{CAFile: ca.CAFile, CertFile: certFile, KeyFile: keyFile}.Load()
		require.NoError(t, err)
		require.NotNil(t, cfg.GetClientCertificate)
		cert, err := cfg.GetClientCertificate(nil)
		require.NoError(t, err)
		require.Equal(t, "client", cert.Leaf.Subject.CommonName)
	})

	tests := []struct {
		name    string
		client  tlsconfig.Client
		wantErr string
	}{
		{
			name:    "invalid shape",
			client:  tlsconfig.Client{CertFile: certFile, KeyFile: keyFile},
			wantErr: "requires a ca file",
		},
		{
			name:    "missing ca file",
			client:  tlsconfig.Client{CAFile: filepath.Join(t.TempDir(), "absent.pem")},
			wantErr: "tls ca file",
		},
		{
			name:    "ca file without certificates",
			client:  tlsconfig.Client{CAFile: emptyPEM},
			wantErr: "no certificates in",
		},
		{
			name:    "unreadable certificate",
			client:  tlsconfig.Client{CAFile: ca.CAFile, CertFile: emptyPEM, KeyFile: keyFile},
			wantErr: "tls cert file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := tt.client.Load()
			require.Nil(t, cfg)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestServerLoad(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "server", "localhost")

	t.Run("plaintext loads to nil", func(t *testing.T) {
		cfg, err := tlsconfig.Server{}.Load()
		require.NoError(t, err)
		require.Nil(t, cfg)
	})

	t.Run("certificate", func(t *testing.T) {
		cfg, err := tlsconfig.Server{CertFile: certFile, KeyFile: keyFile}.Load()
		require.NoError(t, err)
		cert, err := cfg.GetCertificate(nil)
		require.NoError(t, err)
		require.Equal(t, "server", cert.Leaf.Subject.CommonName)
		require.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
		require.Equal(t, tls.NoClientCert, cfg.ClientAuth)
	})

	t.Run("client ca requires client certificates", func(t *testing.T) {
		cfg, err := tlsconfig.Server{CertFile: certFile, KeyFile: keyFile, ClientCAFile: ca.CAFile}.Load()
		require.NoError(t, err)
		require.NotNil(t, cfg.ClientCAs)
		require.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	})

	tests := []struct {
		name    string
		server  tlsconfig.Server
		wantErr string
	}{
		{name: "invalid shape", server: tlsconfig.Server{KeyFile: keyFile}, wantErr: "set together"},
		{
			name:    "missing key file",
			server:  tlsconfig.Server{CertFile: certFile, KeyFile: filepath.Join(t.TempDir(), "absent.key")},
			wantErr: "tls cert file",
		},
		{
			name: "missing client ca file",
			server: tlsconfig.Server{
				CertFile:     certFile,
				KeyFile:      keyFile,
				ClientCAFile: filepath.Join(t.TempDir(), "absent.pem"),
			},
			wantErr: "tls client ca file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := tt.server.Load()
			require.Nil(t, cfg)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// A rotated pair is served without a restart, and one written wrong leaves
// the last good pair in service until the next write that loads.
func TestKeyPairFollowsRotation(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "server", "localhost")
	cfg, err := tlsconfig.Server{CertFile: certFile, KeyFile: keyFile}.Load()
	require.NoError(t, err)
	served := func() string {
		cert, err := cfg.GetCertificate(nil)
		require.NoError(t, err)
		return cert.Leaf.Subject.CommonName
	}
	require.Equal(t, "server", served())

	rotatedCert, rotatedKey := ca.Issue(t, "rotated", "localhost")
	at := time.Now().Add(time.Hour)
	copyFileAt(t, certFile, rotatedCert, at)
	copyFileAt(t, keyFile, rotatedKey, at)
	require.Equal(t, "rotated", served())

	at = at.Add(time.Hour)
	writeFileAt(t, certFile, []byte("not a certificate\n"), at)
	require.Equal(t, "rotated", served())

	fixedCert, fixedKey := ca.Issue(t, "fixed", "localhost")
	at = at.Add(time.Hour)
	copyFileAt(t, certFile, fixedCert, at)
	copyFileAt(t, keyFile, fixedKey, at)
	require.Equal(t, "fixed", served())
}

func copyFileAt(t *testing.T, dst, src string, at time.Time) {
	t.Helper()

	bz, err := os.ReadFile(src)
	require.NoError(t, err)
	writeFileAt(t, dst, bz, at)
}

func writeFileAt(t *testing.T, path string, bz []byte, at time.Time) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, bz, 0o600)) //nolint:gosec // test helper; path is under t.TempDir()
	require.NoError(t, os.Chtimes(path, at, at))
}

// The loaded configs must agree with each other end to end, including the
// client-certificate requirement, which only a handshake exercises.
func TestHandshake(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	serverCert, serverKey := ca.Issue(t, "server", "127.0.0.1")
	clientCert, clientKey := ca.Issue(t, "client", "node")
	other := tlstestutil.NewAuthority(t)
	otherCert, otherKey := other.Issue(t, "stranger", "node")

	tests := []struct {
		name    string
		server  tlsconfig.Server
		client  tlsconfig.Client
		wantErr bool
	}{
		{
			name:   "server certificate only",
			server: tlsconfig.Server{CertFile: serverCert, KeyFile: serverKey},
			client: tlsconfig.Client{CAFile: ca.CAFile},
		},
		{
			name:    "client distrusts the server",
			server:  tlsconfig.Server{CertFile: serverCert, KeyFile: serverKey},
			client:  tlsconfig.Client{CAFile: other.CAFile},
			wantErr: true,
		},
		{
			name:   "mutual",
			server: tlsconfig.Server{CertFile: serverCert, KeyFile: serverKey, ClientCAFile: ca.CAFile},
			client: tlsconfig.Client{CAFile: ca.CAFile, CertFile: clientCert, KeyFile: clientKey},
		},
		{
			name:    "server requires a certificate the client lacks",
			server:  tlsconfig.Server{CertFile: serverCert, KeyFile: serverKey, ClientCAFile: ca.CAFile},
			client:  tlsconfig.Client{CAFile: ca.CAFile},
			wantErr: true,
		},
		{
			name:    "server distrusts the client",
			server:  tlsconfig.Server{CertFile: serverCert, KeyFile: serverKey, ClientCAFile: ca.CAFile},
			client:  tlsconfig.Client{CAFile: ca.CAFile, CertFile: otherCert, KeyFile: otherKey},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverCfg, err := tt.server.Load()
			require.NoError(t, err)
			clientCfg, err := tt.client.Load()
			require.NoError(t, err)

			ln, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = ln.Close() })
			serverErr := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					serverErr <- err
					return
				}
				defer conn.Close()
				serverErr <- conn.(*tls.Conn).Handshake()
			}()

			conn, err := tls.Dial("tcp", ln.Addr().String(), clientCfg)
			if err == nil {
				err = conn.Handshake()
				_ = conn.Close()
			}
			if tt.wantErr {
				require.Error(t, errors.Join(err, <-serverErr))
				return
			}
			require.NoError(t, err)
			require.NoError(t, <-serverErr)
		})
	}
}

// A handshake failure the server sees but the client does not, or the other
// way round, is still a failure: both ends are checked above. This guards the
// listener helper itself against a silent accept.
func TestHandshakeListenerRejectsPlaintext(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	serverCert, serverKey := ca.Issue(t, "server", "127.0.0.1")
	serverCfg, err := tlsconfig.Server{CertFile: serverCert, KeyFile: serverKey}.Load()
	require.NoError(t, err)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.(*tls.Conn).Handshake()
			_ = conn.Close()
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
	require.NoError(t, err)
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	require.Error(t, err)
}
