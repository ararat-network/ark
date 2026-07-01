package oracle

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotifyUpdateIntervalHandlesNilChannel(t *testing.T) {
	oracle := &Oracle{}

	require.NotPanics(t, oracle.notifyUpdateInterval)
}

func TestNotifyUpdateIntervalSendsSignal(t *testing.T) {
	oracle := &Oracle{
		updateIntervalCh: make(chan struct{}, 1),
	}

	oracle.notifyUpdateInterval()

	require.Len(t, oracle.updateIntervalCh, 1)
}

func TestNotifyUpdateIntervalCoalescesFullChannel(t *testing.T) {
	oracle := &Oracle{
		updateIntervalCh: make(chan struct{}, 1),
	}
	oracle.updateIntervalCh <- struct{}{}

	oracle.notifyUpdateInterval()

	require.Len(t, oracle.updateIntervalCh, 1)
}
