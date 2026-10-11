package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	"github.com/ararat-network/ark/pricefeed/api"
)

func TestPricesCmdOwnsFlags(t *testing.T) {
	flags := newPricesCmd().Flags()

	require.Equal(t, defaultAddress, flags.Lookup(flagAddress).DefValue)
	require.Equal(t, defaultPricesOutput, flags.Lookup(flagOutput).DefValue)
}

func TestPricesCmdRejectsArgumentsAndUnsupportedOutput(t *testing.T) {
	t.Run("arguments", func(t *testing.T) {
		cmd := newPricesCmd()
		cmd.SetArgs([]string{"ausd"})

		err := cmd.Execute()

		require.ErrorContains(t, err, "unknown command")
	})

	t.Run("output", func(t *testing.T) {
		cmd := newPricesCmd()
		cmd.SetArgs([]string{"--" + flagOutput, "yaml"})

		err := cmd.Execute()

		require.ErrorContains(t, err, `unsupported prices output "yaml"`)
	})
}

func TestFetchPricesCallsSidecar(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	sidecarServer := &pricesPriceFeedServer{response: pricesResponse(t)}
	api.RegisterPriceFeedServer(server, sidecarServer)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	resp, err := fetchPrices(
		context.Background(),
		"passthrough:///localhost:20321",
		tlsconfig.Client{},
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)

	require.NoError(t, err)
	require.True(t, sidecarServer.called)
	require.Equal(t, sidecarServer.response, resp)
}

// The command dials the sidecar named by its flags, decodes the snapshot, and
// renders it in the requested format.
func TestPricesCmdRendersTheSidecarSnapshot(t *testing.T) {
	address := servePrices(t, &pricesPriceFeedServer{response: pricesResponse(t)})

	tests := []struct {
		output string
		check  func(t *testing.T, out string)
	}{
		{
			output: pricesOutputJSON,
			check: func(t *testing.T, out string) {
				t.Helper()
				var got pricesView
				require.NoError(t, json.Unmarshal([]byte(out), &got))
				require.Equal(t, map[string]string{
					"akrw": "1234.500000000000000000",
					"ausd": "1.000000000000000000",
				}, got.Prices)
				require.True(t, got.Timestamp.Equal(pricesResponse(t).Timestamp))
				require.Equal(t, "v1.2.3", got.Version)
				require.NotEqual(t, "unknown", got.Age)
			},
		},
		{
			output: pricesOutputTable,
			check: func(t *testing.T, out string) {
				t.Helper()
				require.Contains(t, out, "TIMESTAMP  2026-07-10T10:11:12Z")
				require.Contains(t, out, "VERSION    v1.2.3")
				require.Contains(t, out, "akrw   1234.500000000000000000")
				require.Contains(t, out, "ausd   1.000000000000000000")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out := new(bytes.Buffer)
			cmd := newPricesCmd()
			cmd.SetOut(out)
			cmd.SetArgs([]string{"--" + flagAddress, address, "--" + flagOutput, tt.output})

			require.NoError(t, cmd.ExecuteContext(ctx))
			tt.check(t, out.String())
		})
	}
}

// servePrices serves server on a loopback port and returns its address.
func servePrices(t *testing.T, server api.PriceFeedServer) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcServer := grpc.NewServer()
	api.RegisterPriceFeedServer(grpcServer, server)
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(grpcServer.Stop)

	return listener.Addr().String()
}

func TestWritePricesTableSortsDenoms(t *testing.T) {
	resp := pricesResponse(t)
	view, err := newPricesView(resp, resp.Timestamp.Add(2*time.Second))
	require.NoError(t, err)
	out := new(bytes.Buffer)

	err = writePricesTable(out, view)

	require.NoError(t, err)
	output := out.String()
	require.Contains(t, output, "TIMESTAMP  2026-07-10T10:11:12Z")
	require.Contains(t, output, "AGE        2s")
	require.Contains(t, output, "VERSION    v1.2.3")
	require.Contains(t, output, "DENOM  PRICE")
	require.Less(t, strings.Index(output, "akrw"), strings.Index(output, "ausd"))
}

func TestWritePricesJSONUsesDecodedPrices(t *testing.T) {
	resp := pricesResponse(t)
	view, err := newPricesView(resp, resp.Timestamp.Add(2*time.Second))
	require.NoError(t, err)
	out := new(bytes.Buffer)

	err = writePricesJSON(out, view)

	require.NoError(t, err)
	var got pricesView
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, pricesView{
		Prices: map[string]string{
			"akrw": "1234.500000000000000000",
			"ausd": "1.000000000000000000",
		},
		Timestamp: resp.Timestamp,
		Age:       "2s",
		Version:   "v1.2.3",
	}, got)
}

func TestNewPricesViewRejectsMalformedPrice(t *testing.T) {
	_, err := newPricesView(&api.PricesResponse{Prices: map[string][]byte{"ausd": {0x00, 0x01}}}, time.Now())

	require.ErrorContains(t, err, `decoding price "ausd"`)
}

func TestPriceSnapshotAgeIsUnknownForZeroTimestamp(t *testing.T) {
	require.Equal(t, "unknown", priceSnapshotAge(time.Now(), time.Time{}))
}

func pricesResponse(t *testing.T) *api.PricesResponse {
	t.Helper()

	ausd, err := encoding.EncodeCompactLegacyDec(math.LegacyNewDec(1))
	require.NoError(t, err)
	akrw, err := encoding.EncodeCompactLegacyDec(math.LegacyMustNewDecFromStr("1234.5"))
	require.NoError(t, err)

	return &api.PricesResponse{
		Prices: map[string][]byte{
			"ausd": ausd,
			"akrw": akrw,
		},
		Timestamp: time.Date(2026, time.July, 10, 10, 11, 12, 0, time.UTC),
		Version:   "v1.2.3",
	}
}

type pricesPriceFeedServer struct {
	api.UnimplementedPriceFeedServer

	called   bool
	response *api.PricesResponse
}

func (s *pricesPriceFeedServer) Prices(
	_ context.Context,
	_ *api.PricesRequest,
) (*api.PricesResponse, error) {
	s.called = true
	return s.response, nil
}
