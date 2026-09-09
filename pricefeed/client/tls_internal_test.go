package client

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	grpcconntestutil "github.com/ararat-network/ark/pkg/grpcconn/testutil"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
	"github.com/ararat-network/ark/pricefeed/api"
	apitestutil "github.com/ararat-network/ark/pricefeed/api/testutil"
)

type staticPriceFeedServer struct {
	api.UnimplementedPriceFeedServer

	version string
}

func (s *staticPriceFeedServer) Prices(context.Context, *api.PricesRequest) (*api.PricesResponse, error) {
	resp := freshResponse()
	resp.Version = s.version
	return resp, nil
}

// serveSidecar serves service on loopback behind files and returns its
// address. Run dials by address, so these tests take the real network path.
func serveSidecar(t *testing.T, files tlsconfig.Server, service api.PriceFeedServer) string {
	t.Helper()

	return grpcconntestutil.Serve(t, files, func(s *grpc.Server) {
		api.RegisterPriceFeedServer(s, service)
	})
}

func tlsClientConfig(address string, tls tlsconfig.Client) Config {
	cfg := validClientConfig()
	cfg.SidecarAddresses = []string{address}
	cfg.Interval = 20 * time.Millisecond
	cfg.TLS = tls
	return cfg
}

func runClient(t *testing.T, client *Client) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, receiveError(t, errCh), context.Canceled)
	})
}

func pricesServed(client *Client) func() bool {
	return func() bool {
		_, err := client.Prices(context.Background(), &api.PricesRequest{})
		return err == nil
	}
}

func TestCachedPriceClientDialsWithTLS(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "sidecar", "127.0.0.1")
	address := serveSidecar(
		t,
		tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile},
		&staticPriceFeedServer{version: "v1"},
	)

	t.Run("trusting client", func(t *testing.T) {
		client := newTestCachedPriceClient(t, tlsClientConfig(address, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile}))
		runClient(t, client)

		require.Eventually(t, pricesServed(client), 5*time.Second, 10*time.Millisecond)
		require.Equal(t, "v1", client.versions[address])
	})

	t.Run("distrusting client", func(t *testing.T) {
		other := tlstestutil.NewAuthority(t)
		client := newTestCachedPriceClient(t, tlsClientConfig(address, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: other.CAFile}))
		runClient(t, client)

		require.Never(t, pricesServed(client), 300*time.Millisecond, 20*time.Millisecond)
	})
}

func TestCachedPriceClientPresentsClientCertificate(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "sidecar", "127.0.0.1")
	clientCert, clientKey := ca.Issue(t, "node", "node")
	address := serveSidecar(
		t,
		tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile, ClientCAFile: ca.CAFile},
		&staticPriceFeedServer{version: "v1"},
	)

	t.Run("with certificate", func(t *testing.T) {
		client := newTestCachedPriceClient(t, tlsClientConfig(address, tlsconfig.Client{
			Mode:     tlsconfig.TLS,
			CAFile:   ca.CAFile,
			CertFile: clientCert,
			KeyFile:  clientKey,
		}))
		runClient(t, client)

		require.Eventually(t, pricesServed(client), 5*time.Second, 10*time.Millisecond)
	})

	t.Run("without certificate", func(t *testing.T) {
		client := newTestCachedPriceClient(t, tlsClientConfig(address, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile}))
		runClient(t, client)

		require.Never(t, pricesServed(client), 300*time.Millisecond, 20*time.Millisecond)
	})
}

// The files are read at construction, where a bad path fails the start
// command, and only for a client that will dial.
func TestNewCachedPriceClientLoadsTLSFilesWhenEnabled(t *testing.T) {
	cfg := validClientConfig()
	cfg.TLS.Mode = tlsconfig.TLS
	cfg.TLS.CAFile = filepath.Join(t.TempDir(), "absent.pem")

	client, err := NewClient(log.NewNopLogger(), cfg)
	require.Nil(t, client)
	require.ErrorContains(t, err, "sidecar connection")

	cfg.Enabled = false
	client, err = NewClient(log.NewNopLogger(), cfg)
	require.NoError(t, err)
	require.Nil(t, client.dial)
}

// Plaintext to a sidecar off this host is the case the default is not safe
// for, so construction says so; a trust anchor or a loopback peer does not.
func TestNewCachedPriceClientWarnsOnRemotePlaintext(t *testing.T) {
	logs := new(bytes.Buffer)
	logger := log.NewLogger(logs, log.ColorOption(false))
	cfg := validClientConfig()
	cfg.SidecarAddresses = []string{"localhost:8080", "10.0.0.2:8080"}
	cfg.TLS.Mode = tlsconfig.Plaintext

	_, err := NewClient(logger, cfg)
	require.NoError(t, err)
	require.Contains(t, logs.String(), "plaintext off loopback")
	require.Contains(t, logs.String(), "10.0.0.2:8080")
	require.NotContains(t, logs.String(), "localhost:8080")

	logs.Reset()
	cfg.TLS = tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: tlstestutil.NewAuthority(t).CAFile}
	_, err = NewClient(logger, cfg)
	require.NoError(t, err)
	require.NotContains(t, logs.String(), "plaintext")

	logs.Reset()
	cfg.TLS = tlsconfig.Client{}
	cfg.SidecarAddresses = []string{"localhost:8080"}
	_, err = NewClient(logger, cfg)
	require.NoError(t, err)
	require.NotContains(t, logs.String(), "plaintext")
}

func TestCachedPriceClientObservesSidecarVersion(t *testing.T) {
	first := freshResponse()
	first.Version = "v1"
	second := freshResponse()
	second.Version = "v2"
	rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
	gomock.InOrder(
		rpc.EXPECT().Prices(gomock.Any(), gomock.Any(), gomock.Any()).Return(first, nil),
		rpc.EXPECT().Prices(gomock.Any(), gomock.Any(), gomock.Any()).Return(first, nil),
		rpc.EXPECT().Prices(gomock.Any(), gomock.Any(), gomock.Any()).Return(second, nil),
	)
	logs := new(bytes.Buffer)
	client, err := NewClient(log.NewLogger(logs, log.ColorOption(false)), validClientConfig())
	require.NoError(t, err)
	endpoints := testEndpoints(rpc)

	for range 3 {
		client.fetchPrices(context.Background(), endpoints)
	}

	require.Equal(t, "v2", client.versions[endpoints[0].address])
	output := logs.String()
	require.Equal(t, 2, strings.Count(output, "sidecar version"), output)
	require.Contains(t, output, "previous=v1 version=v2")
}

func TestCachedPriceClientReportsIncompatibleSidecar(t *testing.T) {
	rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
	rpc.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.Unimplemented, "unknown service ark.pricefeed.v1.PriceFeed"))
	client := newTestCachedPriceClient(t, validClientConfig())

	_, err := client.fetchFrom(context.Background(), endpoint{address: "sidecar-0", rpc: rpc})

	require.ErrorContains(t, err, "not compatible with this node")
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
