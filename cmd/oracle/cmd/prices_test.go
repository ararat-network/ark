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

	transporttypes "ark/oracle/types"
	"ark/pkg/encoding"
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
		cmd.SetArgs([]string{"uusd"})

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
	oracleServer := &pricesOracleServer{response: pricesResponse(t)}
	transporttypes.RegisterOracleServer(server, oracleServer)
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
	require.Less(t, strings.Index(output, "ukrw"), strings.Index(output, "uusd"))
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
			"ukrw": "1234.500000000000000000",
			"uusd": "1.000000000000000000",
		},
		Timestamp: resp.Timestamp,
		Age:       "2s",
		Version:   "v1.2.3",
	}, got)
}

func TestWritePricesRejectsMalformedPrice(t *testing.T) {
	err := writePrices(
		new(bytes.Buffer),
		&transporttypes.OraclePricesResponse{Prices: map[string][]byte{"uusd": []byte("invalid")}},
		pricesOutputTable,
		time.Now(),
	)

	require.ErrorContains(t, err, `decoding oracle price "uusd"`)
}

func TestPriceSnapshotAgeIsUnknownForZeroTimestamp(t *testing.T) {
	require.Equal(t, "unknown", priceSnapshotAge(time.Now(), time.Time{}))
}

func pricesResponse(t *testing.T) *transporttypes.OraclePricesResponse {
	t.Helper()

	uusd, err := encoding.EncodeLegacyDec(sdkmath.LegacyNewDec(1))
	require.NoError(t, err)
	ukrw, err := encoding.EncodeLegacyDec(sdkmath.LegacyMustNewDecFromStr("1234.5"))
	require.NoError(t, err)

	return &transporttypes.OraclePricesResponse{
		Prices: map[string][]byte{
			"uusd": uusd,
			"ukrw": ukrw,
		},
		Timestamp: time.Date(2026, time.July, 10, 10, 11, 12, 0, time.UTC),
		Version:   "v1.2.3",
	}
}

type pricesOracleServer struct {
	transporttypes.UnimplementedOracleServer

	called   bool
	response *transporttypes.OraclePricesResponse
}

func (s *pricesOracleServer) Prices(
	_ context.Context,
	_ *transporttypes.OraclePricesRequest,
) (*transporttypes.OraclePricesResponse, error) {
	s.called = true
	return s.response, nil
}
