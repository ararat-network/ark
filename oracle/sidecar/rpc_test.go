package sidecar

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/version"

	oracletypes "ark/oracle/sidecar/types"
	transporttypes "ark/oracle/types"
	"ark/pkg/encoding"
)

func TestVersion(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	oracle := newTestOracle(t, nil)
	response, err := oracle.Version(context.Background(), &transporttypes.OracleVersionRequest{})

	require.NoError(t, err)
	require.Equal(t, version.Version, response.Version)
}

func TestPricesRejectsNilRequest(t *testing.T) {
	oracle := newTestOracle(t, nil)

	response, err := oracle.Prices(context.Background(), nil)

	require.Nil(t, response)
	require.ErrorIs(t, err, ErrNilRequest)
}

func TestPricesRejectsStoppedOracle(t *testing.T) {
	oracle := newTestOracle(t, nil)

	response, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, ErrOracleNotRunning)
}

func TestPrices(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	oracle := newTestOracle(t, oracletypes.Prices{
		"NOAH/USD": mustBigFloat(t, "123.456"),
		"NOAH/KRW": mustBigFloat(t, "42.25"),
	})
	startTestRuntime(t, oracle)

	response := requireOracleTick(t, oracle)

	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)
	require.Equal(t, math.LegacyMustNewDecFromStr("123.456"), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("42.25"), decodePrice(t, response.Prices["ukrw"]))
}

func TestPricesReturnsZeroPricesForMissingVoteTargets(t *testing.T) {
	oracle := newTestOracle(t, oracletypes.Prices{})
	startTestRuntime(t, oracle)

	response := requireOracleTick(t, oracle)

	require.Equal(t, math.LegacyZeroDec(), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyZeroDec(), decodePrice(t, response.Prices["ukrw"]))
	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)
}

func TestPricesReturnsCommittedSnapshotDuringAggregationTick(t *testing.T) {
	cfg := newTestRuntimeConfig()
	cfg.UpdateInterval = time.Millisecond
	client := newBlockingVoteTargetsClient(cfg.FallbackDenoms)
	t.Cleanup(client.release)
	oracle := newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(oracletypes.Prices{
			"NOAH/USD": mustBigFloat(t, "1.25"),
			"NOAH/KRW": mustBigFloat(t, "1300"),
		}),
		client,
		ProcessConfig{ServerAddress: "127.0.0.1:0"},
	)
	startTestRuntime(t, oracle)

	initial := requireOracleTick(t, oracle)
	requireSignal(t, client.blocked, "runtime did not block during the next aggregation tick")

	response, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.Equal(t, initial.Timestamp, response.Timestamp)
	require.Equal(t, math.LegacyMustNewDecFromStr("1.25"), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("1300"), decodePrice(t, response.Prices["ukrw"]))
	client.release()
}

func TestPricesReturnsContextErrorBeforeSnapshotRead(t *testing.T) {
	cfg := newTestRuntimeConfig()
	cfg.UpdateInterval = time.Hour
	oracle := newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(nil),
		newStaticChainStateClient(cfg.FallbackDenoms),
		ProcessConfig{ServerAddress: "127.0.0.1:0"},
	)
	startTestRuntime(t, oracle)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := oracle.Prices(ctx, &transporttypes.OraclePricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, context.Canceled)
}

func TestToReqPrices(t *testing.T) {
	tests := []struct {
		name   string
		prices oracletypes.DenomPrices
		want   map[string]math.LegacyDec
	}{
		{
			name:   "empty prices",
			prices: oracletypes.DenomPrices{},
			want:   map[string]math.LegacyDec{},
		},
		{
			name: "multiple prices",
			prices: oracletypes.DenomPrices{
				"uusd": mustBigFloat(t, "123.456"),
				"ukrw": mustBigFloat(t, "42.25"),
			},
			want: map[string]math.LegacyDec{
				"uusd": math.LegacyMustNewDecFromStr("123.456"),
				"ukrw": math.LegacyMustNewDecFromStr("42.25"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toReqPrices(tt.prices)

			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for ticker, want := range tt.want {
				rate, err := encoding.DecodeLegacyDec(got[ticker])
				require.NoError(t, err)
				require.Equal(t, want, rate)
			}
		})
	}
}

func TestToReqPricesRejectsNilPrice(t *testing.T) {
	got, err := toReqPrices(oracletypes.DenomPrices{
		"uusd": nil,
	})

	require.Nil(t, got)
	require.EqualError(t, err, "nil price for uusd")
}

func mustBigFloat(t *testing.T, value string) *big.Float {
	t.Helper()

	price, _, err := big.ParseFloat(value, 10, oracletypes.PricePrecisionBits, big.ToNearestEven)
	require.NoError(t, err)
	return price
}

func decodePrice(t *testing.T, rawPrice []byte) math.LegacyDec {
	t.Helper()

	price, err := encoding.DecodeLegacyDec(rawPrice)
	require.NoError(t, err)
	return price
}
