package api_test

import (
	"errors"
	. "noah/oracle/sidecar/providers/base/api"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrSelectEndpointWithErrWrapsSentinelAndCause(t *testing.T) {
	cause := errors.New("no endpoints")

	err := ErrSelectEndpointWithErr(cause)

	require.ErrorIs(t, err, ErrSelectEndpoint)
	require.ErrorIs(t, err, cause)
}
