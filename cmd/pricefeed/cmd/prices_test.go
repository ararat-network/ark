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

	sdkmath "cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
)

func TestRootCmdExposesPricesCommand(t *testing.T) {
	cmd := NewRootCmd()

	pricesCmd, _, err := cmd.Find([]string{"prices"})
	require.NoError(t, err)
	require.Equal(t, "prices", pricesCmd.Name())
	require.Equal(t, defaultAddress, pricesCmd.Flags().Lookup(flagAddress).DefValue)
	require.Equal(t, defaultPricesOutput, pricesCmd.Flags().Lookup(flagOutput).DefValue)
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

func TestFetchPricesCallsOracleEndpoint(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	oracleServer := &pricesPriceFeedServer{response: pricesResponse(t)}
	api.RegisterPriceFeedServer(server, oracleServer)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	resp, err := fetchPrices(
		context.Background(),
		"passthrough:///oracle-prices",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)

	require.NoError(t, err)
	require.True(t, oracleServer.called)
	require.Equal(t, oracleServer.response, resp)
}

func TestWritePricesTableSortsDenoms(t *testing.T) {
	resp := pricesResponse(t)
	out := new(bytes.Buffer)

	err := writePrices(out, resp, pricesOutputTable, resp.Timestamp.Add(2*time.Second))

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
	out := new(bytes.Buffer)

	err := writePrices(out, resp, pricesOutputJSON, resp.Timestamp.Add(2*time.Second))

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

func TestWritePricesRejectsMalformedPrice(t *testing.T) {
	err := writePrices(
		new(bytes.Buffer),
		&api.PricesResponse{Prices: map[string][]byte{"ausd": {0x00, 0x01}}},
		pricesOutputTable,
		time.Now(),
	)

	require.ErrorContains(t, err, `decoding oracle price "ausd"`)
}

func TestPriceSnapshotAgeIsUnknownForZeroTimestamp(t *testing.T) {
	require.Equal(t, "unknown", priceSnapshotAge(time.Now(), time.Time{}))
}

func pricesResponse(t *testing.T) *api.PricesResponse {
	t.Helper()

	ausd, err := encoding.EncodeCompactLegacyDec(sdkmath.LegacyNewDec(1))
	require.NoError(t, err)
	akrw, err := encoding.EncodeCompactLegacyDec(sdkmath.LegacyMustNewDecFromStr("1234.5"))
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
