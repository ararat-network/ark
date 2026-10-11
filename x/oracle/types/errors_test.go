package types_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	errorsmod "cosmossdk.io/errors"

	"github.com/ararat-network/ark/x/oracle/types"
)

// TestIsRateUnavailable checks each member is recognised through the wraps a consumer sees, and that
// the oracle's other errors are not.
func TestIsRateUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "unknown denom", err: errorsmod.Wrap(types.ErrUnknownDenom, "aeur"), expected: true},
		{name: "stale exchange rate", err: errorsmod.Wrap(types.ErrStaleExchangeRate, "ausd"), expected: true},
		{name: "invalid exchange rate", err: errorsmod.Wrap(types.ErrInvalidExchangeRate, "zero"), expected: true},
		{name: "conversion out of range", err: errorsmod.Wrap(types.ErrConversionOutOfRange, "overflow"), expected: true},
		{
			name:     "member under a further fmt wrap",
			err:      fmt.Errorf("pricing the pair: %w", errorsmod.Wrap(types.ErrStaleExchangeRate, "ausd")),
			expected: true,
		},
		{name: "another oracle error", err: errorsmod.Wrap(types.ErrFeedNotFound, "ausd"), expected: false},
		{name: "a foreign error", err: errors.New("stale exchange rate"), expected: false},
		{name: "nil", err: nil, expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, types.IsRateUnavailable(tc.err))
		})
	}
}
