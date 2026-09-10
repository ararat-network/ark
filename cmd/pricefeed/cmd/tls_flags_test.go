package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	grpcconntestutil "github.com/ararat-network/ark/pkg/grpcconn/testutil"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
	"github.com/ararat-network/ark/pricefeed/api"
)

func TestCommandsOwnTLSFlags(t *testing.T) {
	root := NewRootCmd()
	tests := []struct {
		command string
		flags   []string
	}{
		{
			command: "start",
			flags:   []string{flagTLSCertFile, flagTLSKeyFile, flagTLSClientCAFile},
		},
		{
			command: "prices",
			flags:   []string{flagTLSCAFile, flagTLSCertFile, flagTLSKeyFile, flagTLSServerName},
		},
		{
			command: "check",
			flags: []string{
				flagTLSCAFile,
				flagTLSCertFile,
				flagTLSKeyFile,
				flagTLSServerName,
				chainFlagPrefix + flagTLSCAFile,
				chainFlagPrefix + flagTLSCertFile,
				chainFlagPrefix + flagTLSKeyFile,
				chainFlagPrefix + flagTLSServerName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			cmd, _, err := root.Find([]string{tt.command})
			require.NoError(t, err)
			modeFlag := cmd.Flags().Lookup(flagTLSMode)
			require.NotNil(t, modeFlag)
			require.Equal(t, tlsconfig.Local, modeFlag.DefValue)
			if tt.command == "check" {
				require.Equal(t, tlsconfig.Local, cmd.Flags().Lookup(chainFlagPrefix+flagTLSMode).DefValue)
			}
			for _, name := range tt.flags {
				flag := cmd.Flags().Lookup(name)
				require.NotNil(t, flag, name)
				require.Empty(t, flag.DefValue, name)
			}
		})
	}

	// The admin reload stays loopback and plaintext.
	reload, _, err := root.Find([]string{"config", "reload"})
	require.NoError(t, err)
	require.Nil(t, reload.Flags().Lookup(flagTLSCAFile))
}

// The inspection commands dial with the configured trust anchor, so they
// reach a sidecar that serves over TLS, and refuse files of the wrong shape
// before dialling.
func TestFetchPricesDialsWithTLS(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	certFile, keyFile := ca.Issue(t, "sidecar", "127.0.0.1")
	sidecarServer := &pricesPriceFeedServer{response: pricesResponse(t)}
	address := grpcconntestutil.Serve(
		t,
		tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certFile, KeyFile: keyFile},
		func(s *grpc.Server) { api.RegisterPriceFeedServer(s, sidecarServer) },
	)

	resp, err := fetchPrices(context.Background(), address, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: ca.CAFile})
	require.NoError(t, err)
	require.Equal(t, sidecarServer.response, resp)

	other := tlstestutil.NewAuthority(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = fetchPrices(ctx, address, tlsconfig.Client{Mode: tlsconfig.TLS, CAFile: other.CAFile})
	require.ErrorContains(t, err, "fetching prices")

	_, err = fetchPrices(ctx, address, tlsconfig.Client{Mode: tlsconfig.TLS, CertFile: "node.pem"})
	require.ErrorContains(t, err, "set together")
}
