package grpcconn_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
)

func TestLoadClient(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	for _, tc := range []struct {
		name    string
		client  tlsconfig.Client
		target  string
		wantTLS bool
		wantErr string
	}{
		{name: "local plaintext", target: "127.0.0.1:9090"},
		{name: "remote default rejected", target: "sidecar:9090", wantErr: "requires mode tls"},
		{name: "explicit remote plaintext", client: tlsconfig.Client{Mode: tlsconfig.Plaintext}, target: "sidecar:9090"},
		{name: "TLS with custom roots", client: tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile}, target: "sidecar:9090", wantTLS: true},
		{name: "invalid field combination", client: tlsconfig.Client{CAFile: ca.CAFile}, target: "localhost:9090", wantErr: "require mode tls"},
		{name: "unreadable roots", client: tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: filepath.Join(t.TempDir(), "absent.pem")}, target: "sidecar:9090", wantErr: "tls ca file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			material, err := grpcconn.LoadClient(tc.client, tc.target)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, material)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantTLS, material.Config != nil)
		})
	}
}

func TestOpenAndClose(t *testing.T) {
	opts := grpcconn.DialOptions(nil)
	addresses := []string{"127.0.0.1:1", "127.0.0.1:2"}

	conns, err := grpcconn.Open(addresses, opts...)
	require.NoError(t, err)
	require.Len(t, conns, 2)
	require.NoError(t, grpcconn.Close(conns))

	// A second close fails per connection, and Close reports every one.
	err = grpcconn.Close(conns)
	for _, address := range addresses {
		require.ErrorContains(t, err, "close connection to "+address)
	}
}

func TestOpenFailsOnAnyAddress(t *testing.T) {
	opts := grpcconn.DialOptions(nil)

	for _, addresses := range [][]string{{"%"}, {"127.0.0.1:1", "%"}} {
		conns, err := grpcconn.Open(addresses, opts...)
		require.Nil(t, conns)
		require.ErrorContains(t, err, "open connection to %")
		require.ErrorContains(t, err, "invalid URL escape")
	}
}

func TestLoopback(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{target: "127.0.0.1:8080", want: true},
		{target: "localhost:8080", want: true},
		{target: "LOCALHOST", want: true},
		{target: "[::1]:9090", want: true},
		{target: "unix:///run/sidecar.sock", want: true},
		{target: "unix:relative.sock", want: true},
		{target: "unix-abstract:sidecar", want: true},
		{target: "dns:///localhost:9090", want: true},
		{target: "dns://8.8.8.8/localhost:9090", want: false},
		{target: "passthrough:///127.0.0.1:1", want: true},
		{target: "dns:///sidecar:9090", want: false},
		{target: "passthrough:///sidecar", want: false},
		{target: "0.0.0.0:8080", want: false},
		{target: ":8080", want: false},
		{target: "[::]:8080", want: false},
		{target: "10.0.0.2:9090", want: false},
		{target: "sidecar.internal:8080", want: false},
		{target: "https://localhost", want: false},
		{target: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			require.Equal(t, tt.want, grpcconn.Loopback(tt.target))
		})
	}
}

func TestRemotePlaintext(t *testing.T) {
	targets := []string{"localhost:8080", "10.0.0.2:8080", "sidecar:8080"}

	require.Nil(t, grpcconn.RemotePlaintext(tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: "ca.pem"}, targets...))
	require.Nil(t, grpcconn.RemotePlaintext(tlsconfig.Client{}, "localhost:8080"))
	require.Equal(
		t,
		[]string{"10.0.0.2:8080", "sidecar:8080"},
		grpcconn.RemotePlaintext(tlsconfig.Client{}, targets...),
	)
}

func TestValidateTargets(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		targets    []string
		bad        bool
	}{
		{name: "default loopback", targets: []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080", "unix:///tmp/sidecar.sock"}},
		{name: "remote default", targets: []string{"10.0.0.2:8080"}, bad: true},
		{name: "mixed local and remote", mode: tlsconfig.Local, targets: []string{"localhost:8080", "sidecar:8080"}, bad: true},
		{name: "wildcard listener", targets: []string{":8080"}, bad: true},
		{name: "remote DNS resolver", targets: []string{"dns://8.8.8.8/localhost:8080"}, bad: true},
		{name: "unknown resolver", targets: []string{"custom:///localhost:8080"}, bad: true},
		{name: "TLS remote", mode: tlsconfig.TLS, targets: []string{"sidecar:8080"}},
		{name: "explicit remote plaintext", mode: tlsconfig.Plaintext, targets: []string{"sidecar:8080"}},
		{name: "invalid mode", mode: "auto", bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.bad, grpcconn.ValidateTargets(tc.mode, tc.targets...) != nil) })
	}
}

func TestListenAddress(t *testing.T) {
	for _, tc := range []struct {
		name     string
		address  string
		wantHost string
		wantPort string
		wantErr  string
	}{
		{name: "loopback", address: "127.0.0.1:9091", wantHost: "127.0.0.1", wantPort: "9091"},
		{name: "wildcard", address: ":9091", wantPort: "9091"},
		{name: "trimmed bracketed ipv6", address: " [::1]:0 ", wantHost: "::1", wantPort: "0"},
		{name: "no port", address: "127.0.0.1", wantErr: "missing port"},
		{name: "port out of range", address: "127.0.0.1:70000", wantErr: "invalid port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, port, err := grpcconn.ListenAddress(tc.address)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantHost, host)
			require.Equal(t, tc.wantPort, port)
		})
	}
}

func TestLoopbackListenAddress(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address string
		wantErr string
	}{
		{name: "ipv4 loopback", address: "127.0.0.1:6060"},
		{name: "ipv6 loopback", address: "[::1]:6060"},
		{name: "wildcard", address: "0.0.0.0:6060", wantErr: `host "0.0.0.0" must be a loopback IP address`},
		{name: "public address", address: "10.0.0.5:6060", wantErr: "must be a loopback"},
		{name: "host name", address: "localhost:6060", wantErr: "must be a loopback"},
		{name: "empty host", address: ":6060", wantErr: "must be a loopback"},
		{name: "no port", address: "127.0.0.1", wantErr: "missing port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := grpcconn.LoopbackListenAddress(tc.address)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
