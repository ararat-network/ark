package sidecar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewOracleRejectsInvalidServerAddress(t *testing.T) {
	oracle, err := NewOracle(Config{
		Runtime: newTestRuntimeConfig(),
		Process: ProcessConfig{ServerAddress: "127.0.0.1:invalid"},
	}, nil)

	require.Nil(t, oracle)
	require.Error(t, err)
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	runtimeStarted := make(chan struct{})
	transportStarted := make(chan struct{})
	adminStarted := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runOracle(
			ctx,
			func(ctx context.Context) error {
				close(runtimeStarted)
				<-ctx.Done()
				return ctx.Err()
			},
			func(ctx context.Context) error {
				close(transportStarted)
				<-ctx.Done()
				return nil
			},
			func(ctx context.Context) error {
				close(adminStarted)
				<-ctx.Done()
				return nil
			},
		)
	}()
	requireSignal(t, runtimeStarted, "runtime did not start")
	requireSignal(t, transportStarted, "transport did not start")
	requireSignal(t, adminStarted, "admin transport did not start")

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop after context cancellation")
	}
}

func TestRunStopsOnContextCancellationCause(t *testing.T) {
	runtimeStarted := make(chan struct{})
	transportStarted := make(chan struct{})
	ctx, cancel := context.WithCancelCause(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runOracle(
			ctx,
			func(ctx context.Context) error {
				close(runtimeStarted)
				<-ctx.Done()
				return context.Cause(ctx)
			},
			func(ctx context.Context) error {
				close(transportStarted)
				<-ctx.Done()
				return nil
			},
			nil,
		)
	}()
	requireSignal(t, runtimeStarted, "runtime did not start")
	requireSignal(t, transportStarted, "transport did not start")

	cancel(errors.New("terminated signal received"))
	requireOracleStopped(t, errCh)
}

func TestRunReturnsRuntimePanic(t *testing.T) {
	transportStopped := make(chan struct{})
	err := runOracle(
		context.Background(),
		func(context.Context) error {
			panic("runtime exploded")
		},
		func(ctx context.Context) error {
			<-ctx.Done()
			close(transportStopped)
			return nil
		},
		nil,
	)

	require.ErrorContains(t, err, "oracle runtime panicked: runtime exploded")
	requireSignal(t, transportStopped, "transport cleanup did not finish")
}

func TestRunWaitsForRuntimeShutdown(t *testing.T) {
	runtimeStarted := make(chan struct{})
	transportStarted := make(chan struct{})
	runtimeStopStarted := make(chan struct{})
	allowRuntimeStop := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runOracle(
			ctx,
			func(ctx context.Context) error {
				close(runtimeStarted)
				<-ctx.Done()
				close(runtimeStopStarted)
				<-allowRuntimeStop
				return ctx.Err()
			},
			func(ctx context.Context) error {
				close(transportStarted)
				<-ctx.Done()
				return nil
			},
			nil,
		)
	}()
	requireSignal(t, runtimeStarted, "runtime did not start")
	requireSignal(t, transportStarted, "transport did not start")

	cancel()
	requireSignal(t, runtimeStopStarted, "runtime cleanup did not start")
	select {
	case err := <-errCh:
		t.Fatalf("oracle Run returned before runtime shutdown completed: %v", err)
	default:
	}

	close(allowRuntimeStop)
	requireOracleStopped(t, errCh)
}

func requireOracleStopped(t *testing.T, errCh <-chan error) {
	t.Helper()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop")
	}
}

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}
