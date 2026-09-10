package cmd

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/sync/errgroup"

	"github.com/cosmos/cosmos-sdk/server"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
	oraclemetrics "github.com/ararat-network/ark/abci/oracle/metrics"
	pkgmetrics "github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/pkg/telemetry"
	"github.com/ararat-network/ark/pkg/telemetry/telemetrytest"
	clientmetrics "github.com/ararat-network/ark/pricefeed/client/metrics"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the exported-series golden file")

func TestNewPrometheusEndpointDisabledByDefault(t *testing.T) {
	// No genesis in the default context either: disabled must not read one.
	endpoint, err := newPrometheusEndpoint(server.NewDefaultContext(), telemetry.DefaultPrometheusConfig(), otelFile{})
	require.NoError(t, err)
	require.Nil(t, endpoint)

	// nil receivers are the disabled path in every hook.
	endpoint.install()
	require.NoError(t, endpoint.startBaseappInstrument())
	endpoint.serve(context.Background(), &errgroup.Group{}, nil)
}

// This package's only provider-installing test controls the process's first OTel binding, verifying
// endpoint ownership survives the SDK's subsequent noop install.
func TestPrometheusEndpointOwnsMetersAcrossSDKInit(t *testing.T) {
	early, err := otel.Meter("ark/test/early").Int64Counter("test.early")
	require.NoError(t, err)

	svrCtx := server.NewDefaultContext()
	svrCtx.Config.Moniker = "node0"
	svrCtx.Viper.Set("chain-id", "ark-test")

	endpoint, err := newPrometheusEndpoint(svrCtx, telemetry.PrometheusConfig{Enabled: true, Address: "127.0.0.1:0"}, otelFile{})
	require.NoError(t, err)
	require.NotNil(t, endpoint)

	endpoint.install()
	// As start does: the instrument starts between the installs.
	require.NoError(t, endpoint.startBaseappInstrument())
	otel.SetMeterProvider(metricnoop.NewMeterProvider()) // the SDK's setNoop
	endpoint.install()

	late, err := otel.Meter("ark/test/late").Int64Counter("test.late")
	require.NoError(t, err)
	early.Add(context.Background(), 1)
	late.Add(context.Background(), 1)

	resp := httptest.NewRecorder()
	telemetry.PrometheusHandler(endpoint.registry).ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, resp.Code)
	body := resp.Body.String()
	require.Contains(t, body, "test_early_total 1\n")
	require.Contains(t, body, "test_late_total 1\n")
	require.Contains(t, body, `service_instance_id="ark-test/node0"`)
	require.Contains(t, body, `ark_chain_id="ark-test"`)
	require.Contains(t, body, "go_goroutines")

	// Every Ark series the node exports, by name, type, and label keys: the
	// contract dashboards and alert rules elsewhere are written to. Checked
	// here because the meters are bound to this test's provider.
	recordEveryNodeSeries()
	telemetrytest.RequireGolden(t, endpoint.registry, telemetrytest.ArkSeries, filepath.Join("testdata", "exported_series.txt"), *updateGolden)

	ctx, cancel := context.WithCancel(context.Background())
	var g errgroup.Group
	endpoint.serve(ctx, &g, nil)
	cancel()
	require.NoError(t, g.Wait())
}

func TestStartChainIDPrefersFlagThenGenesis(t *testing.T) {
	svrCtx := server.NewDefaultContext()
	svrCtx.Config.SetRoot(t.TempDir())

	_, err := startChainID(svrCtx)
	require.ErrorContains(t, err, "resolving chain ID from genesis")

	svrCtx.Viper.Set("chain-id", "from-flag")
	chainID, err := startChainID(svrCtx)
	require.NoError(t, err)
	require.Equal(t, "from-flag", chainID)
}

// recordEveryNodeSeries records once through each of the node's meters, so
// every instrument has a series to export.
func recordEveryNodeSeries() {
	abcimetrics.ObservePool(func() ([3]int, [3]int64) { return [3]int{}, [3]int64{} })
	abcimetrics.RecordAdmission("accepted")
	sink := telemetry.NewGoMetricsSink(context.Background(), otel.Meter("gometrics"))
	sink.AddSample([]string{"/unknown"}, 1)
	abcimetrics.RecordLatencyAndStatus(time.Millisecond, abcimetrics.StatusSuccess, abcimetrics.PrepareProposal)
	oraclemetrics.CountVoteReports(1, 1, 1)
	oraclemetrics.RecordBlockParticipation(1, 1, true)
	oraclemetrics.RecordVoteCoverage(1, 2, []string{"ausd"})
	clientmetrics.RecordSidecarResponse("127.0.0.1:1", time.Millisecond, nil)
	clientmetrics.RecordSnapshotTimestamp("127.0.0.1:1", time.Now())
	pkgmetrics.RecordModuleMethodLatency(context.Background(), "oracle", pkgmetrics.EndBlock)()
}
