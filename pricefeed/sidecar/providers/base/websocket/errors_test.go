package websocket_test

import (
	. "ark/pricefeed/sidecar/providers/base/websocket"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrSelectEndpointWithErrWrapsSentinelAndCause(t *testing.T) {
	cause := errors.New("no endpoint available")

	err := ErrSelectEndpointWithErr(cause)

	require.ErrorIs(t, err, ErrSelectEndpoint)
	require.ErrorIs(t, err, cause)
}
