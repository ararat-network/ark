package internal

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRecoveringReturnsFunctionError(t *testing.T) {
	expected := errors.New("failed")

	err := RunRecovering("worker", func() error {
		return expected
	})

	require.ErrorIs(t, err, expected)
}

func TestRunRecoveringConvertsPanic(t *testing.T) {
	err := RunRecovering("worker", func() error {
		panic("boom")
	})

	require.ErrorContains(t, err, "worker panicked")
	require.ErrorContains(t, err, "boom")
	require.True(t, IsPanic(err))
}

func TestHandlePanicConvertsDeferredPanic(t *testing.T) {
	var got error

	func() {
		defer HandlePanic("worker", func(err error) {
			got = err
		})

		panic("boom")
	}()

	require.ErrorContains(t, got, "worker panicked")
	require.ErrorContains(t, got, "boom")
	require.True(t, IsPanic(got))
}

func TestIsPanicRejectsOrdinaryErrors(t *testing.T) {
	require.False(t, IsPanic(errors.New("failed")))
}
