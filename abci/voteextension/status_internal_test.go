package voteextension

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
	abcitypes "github.com/ararat-network/ark/abci/types"
)

func TestVoteExtensionStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want abcimetrics.Status
	}{
		{name: "nil is success", err: nil, want: abcimetrics.StatusSuccess},
		{name: "nil request", err: fmt.Errorf("%w for %s", abcitypes.ErrNilRequest, abcimetrics.ExtendVote), want: abcimetrics.StatusNilRequest},
		{name: "panic", err: fmt.Errorf("%w: %w", errPanic, errors.New("boom")), want: abcimetrics.StatusPanic},
		{name: "price feed client", err: fmt.Errorf("%w: %w", errPriceFeedClient, errors.New("boom")), want: abcimetrics.StatusPriceFeedClient},
		{name: "invalid prices", err: fmt.Errorf("%w: %w", errInvalidPrices, errors.New("boom")), want: abcimetrics.StatusInvalidPrices},
		{name: "vote extension validation", err: fmt.Errorf("%w: %w", errVoteExtensionValidation, errors.New("boom")), want: abcimetrics.StatusVoteExtensionValidation},
		{name: "oracle keeper", err: fmt.Errorf("%w: get feeds for height %d: %w", abcitypes.ErrOracleKeeper, 7, errors.New("boom")), want: abcimetrics.StatusOracleKeeper},
		{name: "codec", err: fmt.Errorf("%w: %w", abcitypes.ErrCodec, errors.New("boom")), want: abcimetrics.StatusCodec},
		{name: "unknown error is failure", err: errors.New("boom"), want: abcimetrics.StatusFailure},
		// A recovered panic wraps its cause, and the panic is what the
		// operator needs to see whatever the cause was.
		{name: "panic outranks codec", err: fmt.Errorf("%w: %w", errPanic, abcitypes.ErrCodec), want: abcimetrics.StatusPanic},
		{name: "panic outranks oracle keeper", err: fmt.Errorf("%w: %w", errPanic, abcitypes.ErrOracleKeeper), want: abcimetrics.StatusPanic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, voteExtensionStatus(tt.err))
		})
	}
}
