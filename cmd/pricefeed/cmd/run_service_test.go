package cmd

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// offlineOracleConfigJSON is validOracleConfigJSON with every endpoint pointed
// somewhere unroutable and every interval pushed past the test's lifetime, so
// a sidecar started from it reaches its serving state without leaving the
// machine.
func offlineOracleConfigJSON() string {
	return `{
		"updateInterval": "1h",
		"providers": {
			"frankfurter_api": {
				"name": "frankfurter_api",
				"transportType": "api",
				"maxPriceAge": "90s",
				"markets": [
					{"pair": "NOAH/USD", "symbol": "NOAHUSD"}
				],
				"api": {
					"name": "frankfurter_api",
					"timeout": "1s",
					"interval": "1h",
					"endpoints": [{"url": "https://localhost.invalid/rates"}],
					"batchSize": 1
				}
			}
		},
		"resolver": {},
		"client": {
			"address": "passthrough:///feeds",
			"timeout": "1s",
			"interval": "1h"
		},
		"fallbackFeeds": ["ausd"]
	}`
}

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

// quietStartOptions serves on ephemeral ports and logs only failures, so
// concurrent tests never contend for the command's fixed defaults. The admin
// transport stays off: its own run wrapper is covered where it lives.
func quietStartOptions(t *testing.T) startOptions {
	t.Helper()

	return startOptions{
		address:  freeLoopbackAddress(t),
		logLevel: "error",
	}
}

func TestRunServiceRejectsInvalidLogLevel(t *testing.T) {
	err := runService(context.Background(), writeOracleConfig(t, offlineOracleConfigJSON()), startOptions{
		logLevel: "chatty",
	})

	require.Error(t, err)
}

func TestRunServiceReportsAnUnreadableConfig(t *testing.T) {
	err := runService(context.Background(), filepath.Join(t.TempDir(), "absent.json"), quietStartOptions(t))

	require.Error(t, err)
}

func TestRunServiceReportsAnInvalidConfig(t *testing.T) {
	err := runService(context.Background(), writeOracleConfig(t, `{"updateInterval": "0s"}`), quietStartOptions(t))

	require.Error(t, err)
}

// TestRunServiceServesItsProcessEndpointsUntilCancelled is the one place the
// process-owned HTTP endpoints are stood up the way `pricefeed start` does it.
// It is also the only call to initPrometheus in this package: the exporter
// registers a collector on the default registry, which a second call could not
// repeat.
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
		errCh <- runService(ctx, writeOracleConfig(t, offlineOracleConfigJSON()), options)
	}()

	requireHTTPOK(t, "http://"+metricsAddress+"/metrics")
	requireHTTPOK(t, "http://"+pprofAddress+"/debug/pprof/")

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("runService did not return after cancellation")
	}
}

func TestPrometheusTelemetryIsInertWhenAbsent(t *testing.T) {
	var absent *prometheusTelemetry

	require.Nil(t, absent.Handler())
	require.NoError(t, absent.Shutdown(context.Background()))
}
