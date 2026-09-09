package chainstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	grpcconntestutil "github.com/ararat-network/ark/pkg/grpcconn/testutil"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// serveQuery serves query behind files and returns its address: the shape of
// a terminator in front of the node's plaintext gRPC port.
func serveQuery(t *testing.T, query oracletypes.QueryServer, files tlsconfig.Server) string {
	t.Helper()

	return grpcconntestutil.Serve(t, files, func(s *grpc.Server) {
		oracletypes.RegisterQueryServer(s, query)
	})
}

func tlsConfig(address string, interval time.Duration, tls tlsconfig.Client) Config {
	return Config{
		Addresses: []string{address},
		Timeout:   time.Second,
		Interval:  interval,
		TLS:       tls,
	}
}

func TestRunDialsWithConfiguredTLS(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "chain", "127.0.0.1")
	query := newFakeQueryServer(feedResult([]string{"ausd"}))
	address := serveQuery(t, query, tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile})

	client, err := NewClient(tlsConfig(address, time.Hour, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile}))
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ausd"})
}

// A trust anchor the terminator's certificate does not chain to fails every
// query, and a config update that fixes it reconnects on the next tick.
func TestUpdateConfigReconnectsWhenTLSChanges(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	other := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "chain", "127.0.0.1")
	query := newFakeQueryServer(feedResult([]string{"ausd"}))
	address := serveQuery(t, query, tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile})
	logs := &lockedBuffer{}
	interval := 20 * time.Millisecond

	client, err := NewClient(
		tlsConfig(address, interval, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: other.CAFile}),
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), "failed to refresh chain state feeds")
	}, time.Second, time.Millisecond)
	query.requireNoCalls(t, interval)
	_, err = client.Feeds()
	require.EqualError(t, err, "no feeds fetched yet")

	require.NoError(t, client.Update(tlsConfig(address, interval, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile})))

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ausd"})
	require.Contains(t, logs.String(), "reconnecting chain state feed client after tls update")
}

// The files are read at construction and at Update, where a bad path fails
// the start or the reload rather than every poll after it, and a rejected
// update leaves the client on its current config.
func TestClientReadsTLSFilesAtConstructionAndUpdate(t *testing.T) {
	absent := tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: filepath.Join(t.TempDir(), "absent.pem")}

	client, err := NewClient(tlsConfig("passthrough:///chain", time.Second, absent))
	require.Nil(t, client)
	require.ErrorContains(t, err, "oracle query connection")

	cfg := tlsConfig("passthrough:///chain", time.Second, tlsconfig.Client{Mode: tlsconfig.Plaintext})
	client, err = NewClient(cfg)
	require.NoError(t, err)

	err = client.Update(tlsConfig("passthrough:///chain", time.Second, absent))

	require.ErrorContains(t, err, "oracle query connection")
	require.Equal(t, cfg, client.getConfig())
}

// Plaintext to a chain node off this host is the case the default is not
// safe for, so construction and Update say so.
func TestClientWarnsOnRemotePlaintext(t *testing.T) {
	logs := &lockedBuffer{}
	cfg := tlsConfig("10.0.0.2:9090", time.Second, tlsconfig.Client{Mode: tlsconfig.Plaintext})

	_, err := NewClient(cfg, WithLogger(log.NewLogger(logs, log.ColorOption(false))))
	require.NoError(t, err)
	require.Contains(t, logs.String(), "plaintext off loopback")
	require.Contains(t, logs.String(), "10.0.0.2:9090")

	client, err := NewClient(
		tlsConfig("127.0.0.1:9090", time.Second, tlsconfig.Client{Mode: tlsconfig.Plaintext}),
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
	)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(logs.String(), "plaintext off loopback"))

	require.NoError(t, client.Update(cfg))
	require.Equal(t, 2, strings.Count(logs.String(), "plaintext off loopback"))
}

func TestConfigEqualCoversTLS(t *testing.T) {
	base := Config{TLS: tlsconfig.Client{Mode: tlsconfig.Plaintext}, Addresses: []string{"passthrough:///chain"}, Timeout: time.Second, Interval: time.Second}
	withTLS := base
	withTLS.TLS = tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: "ca.pem"}

	sameTLS := withTLS
	require.True(t, base.Equal(Config{TLS: tlsconfig.Client{Mode: tlsconfig.Plaintext}, Addresses: base.Addresses, Timeout: base.Timeout, Interval: base.Interval}))
	require.False(t, base.Equal(withTLS))
	require.True(t, withTLS.Equal(sameTLS))
}

func TestConfigValidateRejectsInvalidTLS(t *testing.T) {
	cfg := Config{
		Addresses: []string{"passthrough:///chain"},
		Timeout:   time.Second,
		Interval:  time.Second,
		TLS:       tlsconfig.Client{Mode: tlsconfig.TLS, CertFile: "chain.pem"},
	}

	require.ErrorContains(t, cfg.Validate(), "feed query tls")
}

func TestTimingUpdateRetainsLoadedTrustRoots(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	cfg := tlsConfig("127.0.0.1:9090", time.Second, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile})
	client, err := NewClient(cfg)
	require.NoError(t, err)
	_, original := client.snapshot()
	require.NoError(t, os.Remove(ca.CAFile))
	cfg.Timeout *= 2
	require.NoError(t, client.Update(cfg))
	_, next := client.snapshot()
	require.Same(t, original, next)
	cfg.Addresses = []string{"127.0.0.1:9091"}
	require.ErrorContains(t, client.Update(cfg), "tls ca file")
	_, next = client.snapshot()
	require.Same(t, original, next)
	require.Equal(t, []string{"127.0.0.1:9090"}, client.getConfig().Addresses)
}

func TestRejectedTLSUpdateKeepsRunningConnection(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	cert, key := ca.Issue(t, "chain", "127.0.0.1")
	query := newFakeQueryServer(feedResult([]string{"ausd"}))
	address := serveQuery(t, query, tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: cert, KeyFile: key})
	cfg := tlsConfig(address, 20*time.Millisecond, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile})
	client, err := NewClient(cfg)
	require.NoError(t, err)
	cancel := startClient(t, client)
	defer stopClient(cancel, client)
	query.waitForCalls(t, 1)
	cfg.TLS.CAFile = filepath.Join(t.TempDir(), "missing.pem")
	require.ErrorContains(t, client.Update(cfg), "tls ca file")
	query.waitForCalls(t, 2)
	requireEventuallyTargets(t, client, []string{"ausd"})
}
