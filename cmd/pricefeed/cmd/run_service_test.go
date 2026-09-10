package cmd

import (
	"context"
	"flag"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/telemetry/telemetrytest"
	chainstatemetrics "github.com/ararat-network/ark/pricefeed/sidecar/chainstate/metrics"
	sidecarmetrics "github.com/ararat-network/ark/pricefeed/sidecar/metrics"
	apimetrics "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api/metrics"
	providermetrics "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/metrics"
	wsmetrics "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket/metrics"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the exported-series golden file")

// freeLoopbackAddress returns a loopback address nothing is listening on.
// runService binds its own listeners, so the addresses have to be released
// again before it starts.
func freeLoopbackAddress(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := ln.Addr().String()
	require.NoError(t, ln.Close())

	return address
}

func requireHTTPOK(t *testing.T, url string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:noctx // the deadline below bounds the loop.
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			require.NoError(t, readErr)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.NotEmpty(t, body)

			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never answered: %v", url, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// quietStartOptions serves on ephemeral ports, so concurrent tests never
// contend for the command's fixed defaults. The admin transport stays off:
// its own run wrapper is covered where it lives.
func quietStartOptions(t *testing.T) startOptions {
	t.Helper()

	return startOptions{address: freeLoopbackAddress(t)}
}

// quietLogger logs only failures.
func quietLogger(t *testing.T) log.Logger {
	t.Helper()

	logger, err := newLogger("error", logFormatPlain)
	require.NoError(t, err)

	return logger
}

// The refusals land before the config is read or anything listens: the config
// path does not exist.
func TestRunServiceRefusesUnsafeProcessAddresses(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*startOptions)
		wantErr string
	}{
		{
			name:    "pprof off loopback",
			mutate:  func(o *startOptions) { o.pprof, o.pprofAddress = true, "0.0.0.0:0" },
			wantErr: `pprof address: host "0.0.0.0" must be a loopback`,
		},
		{
			name:    "metrics without a port",
			mutate:  func(o *startOptions) { o.metrics, o.metricsAddress = true, "127.0.0.1" },
			wantErr: "metrics address: address 127.0.0.1: missing port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := quietStartOptions(t)
			tt.mutate(&options)

			err := runService(context.Background(), quietLogger(t), filepath.Join(t.TempDir(), "absent.toml"), options)

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRunServiceReportsAnUnreadableConfig(t *testing.T) {
	err := runService(context.Background(), quietLogger(t), filepath.Join(t.TempDir(), "absent.toml"), quietStartOptions(t))

	require.Error(t, err)
}

func TestRunServiceReportsAnInvalidConfig(t *testing.T) {
	err := runService(context.Background(), quietLogger(t), writeConfig(t, `update_interval = "0s"`), quietStartOptions(t))

	require.Error(t, err)
}

// TestRunServiceServesItsProcessEndpointsUntilCancelled exercises production endpoint startup. It
// is this package's only enabled newPrometheusEndpoint call because the default registry cannot
// register its collector twice.
func TestRunServiceServesItsProcessEndpointsUntilCancelled(t *testing.T) {
	metricsAddress := freeLoopbackAddress(t)
	pprofAddress := freeLoopbackAddress(t)

	options := quietStartOptions(t)
	options.metrics = true
	options.metricsAddress = metricsAddress
	options.pprof = true
	options.pprofAddress = pprofAddress

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runService(ctx, quietLogger(t), writeConfig(t, offlineConfigTOML()), options)
	}()

	requireHTTPOK(t, "http://"+metricsAddress+"/metrics")
	requireHTTPOK(t, "http://"+pprofAddress+"/debug/pprof/")

	// Every Ark series the sidecar exports, by name, type, and label keys: the
	// contract dashboards and alert rules elsewhere are written to. Checked
	// here because the meters are bound to this process's one provider.
	recordEverySidecarSeries(ctx)
	telemetrytest.RequireGolden(t, prometheus.DefaultGatherer, telemetrytest.ArkSeries, filepath.Join("testdata", "exported_series.txt"), *updateGolden)

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("runService did not return after cancellation")
	}
}

// recordEverySidecarSeries records once through each of the sidecar's meters,
// so every instrument has a series to export.
func recordEverySidecarSeries(ctx context.Context) {
	sidecarmetrics.RecordTick(ctx)
	sidecarmetrics.PublishAggregationSnapshot(sidecarmetrics.AggregationSnapshot{
		Prices:       map[string]float64{"NOAH/USD": 1},
		SampleCounts: map[string]int64{"NOAH/USD": 1},
	})
	sidecarmetrics.RecordBootstrapPriceUse(ctx, "NOAH/USD")
	sidecarmetrics.RecordMissingPrice(ctx, "ausd")
	sidecarmetrics.RecordSkippedSample(ctx, "provider", "NOAH/USD", sidecarmetrics.SkipReasonStale)
	sidecarmetrics.RecordRPC(ctx, "/ark.pricefeed.v1.PriceFeed/Prices", "OK")
	chainstatemetrics.RecordRefresh(ctx, "127.0.0.1:1", "success")
	providermetrics.RecordResponse(ctx, "provider", providertypes.Ticker("NOAHUSD"), providertypes.OK)
	wsmetrics.RecordConnectionEvent(ctx, "provider", wsmetrics.ConnectionEventHealthy)
	wsmetrics.RecordConnectionEvent(ctx, "provider", wsmetrics.ConnectionEventReconnect)
	wsmetrics.RecordParseError(ctx, "provider")
	wsmetrics.RecordWriteError(ctx, "provider", wsmetrics.WriteOperationSubscribe)
	apimetrics.RecordRequest(ctx, "provider", time.Millisecond, &http.Response{StatusCode: http.StatusOK}, nil)
}
