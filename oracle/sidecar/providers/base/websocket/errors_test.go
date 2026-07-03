package websocket_test

import (
	"errors"
	. "noah/oracle/sidecar/providers/base/websocket"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrSelectEndpointWithErrWrapsSentinelAndCause(t *testing.T) {
	cause := errors.New("no endpoint available")

	err := ErrSelectEndpointWithErr(cause)

	require.ErrorIs(t, err, ErrSelectEndpoint)
	require.ErrorIs(t, err, cause)
}
