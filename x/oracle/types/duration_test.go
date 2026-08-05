package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ark/x/oracle/types"
)

// The codec backs stored staleness windows, so a value that does not survive a
// round trip is a window silently changing across a restart.
func TestDurationValueRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value time.Duration
	}{
		{name: "zero", value: 0},
		{name: "sub-second", value: 250 * time.Millisecond},
		{name: "minute", value: time.Minute},
		{name: "slow feed window", value: 26 * time.Hour},
		{name: "maximum", value: time.Duration(1<<63 - 1)},
		// Negative windows are rejected before they reach the store, but the
		// codec must not corrupt one if a migration ever writes it.
		{name: "negative", value: -time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := types.DurationValue.Encode(tc.value)
			require.NoError(t, err)
			decoded, err := types.DurationValue.Decode(encoded)
			require.NoError(t, err)
			require.Equal(t, tc.value, decoded)

			encodedJSON, err := types.DurationValue.EncodeJSON(tc.value)
			require.NoError(t, err)
			decodedJSON, err := types.DurationValue.DecodeJSON(encodedJSON)
			require.NoError(t, err)
			require.Equal(t, tc.value, decodedJSON)
		})
	}
}

// JSON carries the unit rather than a bare nanosecond count, which is what
// makes an exported schema readable by whoever has to audit it.
func TestDurationValueJSONIsUnitBearing(t *testing.T) {
	encoded, err := types.DurationValue.EncodeJSON(26 * time.Hour)
	require.NoError(t, err)
	require.Equal(t, `"26h0m0s"`, string(encoded))
	require.Equal(t, "26h0m0s", types.DurationValue.Stringify(26*time.Hour))
}

func TestDurationValueRejectsMalformedJSON(t *testing.T) {
	_, err := types.DurationValue.DecodeJSON([]byte(`"not-a-duration"`))
	require.ErrorContains(t, err, "parsing duration")

	_, err = types.DurationValue.DecodeJSON([]byte(`93600000000000`))
	require.Error(t, err)
}
