package sidecar_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	server "noah/oracle/sidecar"
)

func TestCloserDone(t *testing.T) {
	closer := server.NewCloser()

	select {
	case <-closer.Done():
		t.Fatal("Done channel closed before Close")
	default:
	}

	closer.Close()

	select {
	case <-closer.Done():
	default:
		t.Fatal("Done channel remained open after Close")
	}
}

func TestCloserCallbackRunsOnce(t *testing.T) {
	var calls atomic.Int64
	closer := server.NewCloser().WithCallback(func() {
		calls.Add(1)
	})

	closer.Close()
	closer.Close()

	require.Equal(t, int64(1), calls.Load())
}

func TestCloserConcurrentCloseRunsCallbackOnce(t *testing.T) {
	var calls atomic.Int64
	closer := server.NewCloser().WithCallback(func() {
		calls.Add(1)
	})

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			closer.Close()
		}()
	}
	wg.Wait()

	require.Equal(t, int64(1), calls.Load())
	select {
	case <-closer.Done():
	default:
		t.Fatal("Done channel remained open after concurrent Close calls")
	}
}

func TestCloserWithoutCallback(t *testing.T) {
	closer := server.NewCloser()

	require.NotPanics(t, closer.Close)
	select {
	case <-closer.Done():
	default:
		t.Fatal("Done channel remained open after Close")
	}
}
