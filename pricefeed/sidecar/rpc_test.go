package sidecar

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/version"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func TestVersion(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	oracle := newTestOracle(t, nil)
	response, err := oracle.Version(context.Background(), &api.VersionRequest{})

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

	response, err := oracle.Prices(context.Background(), &api.PricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, ErrOracleNotRunning)
}

func TestPrices(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	oracle := newTestOracle(t, sidecartypes.Prices{
		"NOAH/USD": mustBigFloat(t, "8"),
		"NOAH/KRW": mustBigFloat(t, "0.5"),
	})
	startTestRuntime(t, oracle)

	response := requireOracleTick(t, oracle)

	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)
	// The venue quotes the unit per one NOAH; each UNIT/NOAH leg normalises
	// its sample, and the feed publishes NOAH per unit.
	require.Equal(t, math.LegacyMustNewDecFromStr("0.125"), decodePrice(t, response.Prices["ausd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("2"), decodePrice(t, response.Prices["akrw"]))
}

func TestPricesOmitsMissingFeeds(t *testing.T) {
	oracle := newTestOracle(t, sidecartypes.Prices{})
	startTestRuntime(t, oracle)

	response := requireOracleTick(t, oracle)

	require.Empty(t, response.Prices)
	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, Version(), response.Version)
}

func TestPricesReturnsCommittedSnapshotDuringAggregationTick(t *testing.T) {
	cfg := newTestRuntimeConfig()
	cfg.UpdateInterval = time.Millisecond
	client := newBlockingFeedsClient(cfg.FallbackFeeds)
	t.Cleanup(client.release)
	oracle := newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(sidecartypes.Prices{
			"NOAH/USD": mustBigFloat(t, "0.25"),
			"NOAH/KRW": mustBigFloat(t, "0.0625"),
		}),
		client,
		ProcessConfig{ServerAddress: "127.0.0.1:0"},
	)
	startTestRuntime(t, oracle)

	initial := requireOracleTick(t, oracle)
	requireSignal(t, client.blocked, "runtime did not block during the next aggregation tick")

	response, err := oracle.Prices(context.Background(), &api.PricesRequest{})

	require.NoError(t, err)
	require.Equal(t, initial.Timestamp, response.Timestamp)
	require.Equal(t, math.LegacyMustNewDecFromStr("4"), decodePrice(t, response.Prices["ausd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("16"), decodePrice(t, response.Prices["akrw"]))
	client.release()
}

func TestPricesReturnsContextErrorBeforeSnapshotRead(t *testing.T) {
	cfg := newTestRuntimeConfig()
	cfg.UpdateInterval = time.Hour
	oracle := newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(nil),
		newStaticChainStateClient(cfg.FallbackFeeds),
		ProcessConfig{ServerAddress: "127.0.0.1:0"},
	)
	startTestRuntime(t, oracle)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := oracle.Prices(ctx, &api.PricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, context.Canceled)
}

func TestToReqPrices(t *testing.T) {
	tests := []struct {
		name   string
		prices sidecartypes.FeedPrices
		want   map[string]math.LegacyDec
	}{
		{
			name:   "empty prices",
			prices: sidecartypes.FeedPrices{},
			want:   map[string]math.LegacyDec{},
		},
		{
			name: "multiple prices",
			prices: sidecartypes.FeedPrices{
				"ausd": mustBigFloat(t, "123.456"),
				"akrw": mustBigFloat(t, "42.25"),
			},
			want: map[string]math.LegacyDec{
				"ausd": math.LegacyMustNewDecFromStr("123.456"),
				"akrw": math.LegacyMustNewDecFromStr("42.25"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toReqPrices(tt.prices)

			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for ticker, want := range tt.want {
				rate, err := encoding.DecodeCompactLegacyDec(got[ticker])
				require.NoError(t, err)
				require.Equal(t, want, rate)
			}
		})
	}
}

func TestToReqPricesRejectsNilPrice(t *testing.T) {
	got, err := toReqPrices(sidecartypes.FeedPrices{
		"ausd": nil,
	})

	require.Nil(t, got)
	require.EqualError(t, err, "nil price for ausd")
}

func TestToReqPricesRejectsOutOfRangePriceBeforeFormatting(t *testing.T) {
	tooLarge := new(big.Float).SetPrec(sidecartypes.PricePrecisionBits)
	tooLarge.SetInt(new(big.Int).Lsh(big.NewInt(1), 256))

	got, err := toReqPrices(sidecartypes.FeedPrices{"ausd": tooLarge})

	require.Nil(t, got)
	require.ErrorContains(t, err, "magnitude exceeds LegacyDec range")
}

func mustBigFloat(t *testing.T, value string) *big.Float {
	t.Helper()

	price, _, err := big.ParseFloat(value, 10, sidecartypes.PricePrecisionBits, big.ToNearestEven)
	require.NoError(t, err)
	return price
}

func decodePrice(t *testing.T, rawPrice []byte) math.LegacyDec {
	t.Helper()

	price, err := encoding.DecodeCompactLegacyDec(rawPrice)
	require.NoError(t, err)
	return price
}
