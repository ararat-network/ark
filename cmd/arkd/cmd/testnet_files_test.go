package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCalculateIP(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		offset      int
		want        string
		errContains string
	}{
		{name: "zero offset returns the starting address", ip: "192.168.0.1", want: "192.168.0.1"},
		{name: "offset increments the last octet", ip: "192.168.0.1", offset: 3, want: "192.168.0.4"},
		{name: "the last usable octet", ip: "192.168.0.1", offset: 254, want: "192.168.0.255"},
		{name: "a zero starting octet", ip: "10.0.0.0", offset: 255, want: "10.0.0.255"},
		{
			// Incrementing in a loop wrapped here, handing this node the same
			// address as the one three past the start.
			name:        "carry past the last octet is refused",
			ip:          "192.168.0.250",
			offset:      10,
			errContains: "overflows the last octet",
		},
		{
			name:        "one past the broadcast address",
			ip:          "192.168.0.255",
			offset:      1,
			errContains: "overflows the last octet",
		},
		{
			name:        "an offset wider than an octet",
			ip:          "192.168.0.1",
			offset:      256,
			errContains: "overflows the last octet",
		},
		{
			name:        "a negative offset",
			ip:          "192.168.0.10",
			offset:      -1,
			errContains: "overflows the last octet",
		},
		{name: "not an address", ip: "not-an-ip", errContains: "non ipv4 address"},
		{name: "an ipv6 address", ip: "::1", offset: 1, errContains: "non ipv4 address"},
		{name: "an ipv4 address in ipv6 form", ip: "::ffff:192.168.0.1", offset: 1, want: "192.168.0.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateIP(tt.ip, tt.offset)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				require.Empty(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestCalculateIPAssignsDistinctAddresses checks that validator IP assignments remain distinct and
// refuse overflow.
func TestCalculateIPAssignsDistinctAddresses(t *testing.T) {
	const start = "192.168.0.1"

	seen := make(map[string]int, maxOctet)
	for i := range maxOctet {
		ip, err := calculateIP(start, i)
		require.NoError(t, err)
		if first, clash := seen[ip]; clash {
			t.Fatalf("offset %d reuses the address issued to offset %d: %s", i, first, ip)
		}
		seen[ip] = i
	}
	require.Len(t, seen, maxOctet)

	// One past the last usable offset is refused rather than reissued.
	_, err := calculateIP(start, maxOctet)
	require.ErrorContains(t, err, "overflows the last octet")
}

// TestGetIPUsesTheStartingAddress covers the branch that does not consult the
// host's external interface.
func TestGetIPUsesTheStartingAddress(t *testing.T) {
	got, err := getIP(2, "10.1.2.3")
	require.NoError(t, err)
	require.Equal(t, "10.1.2.5", got)

	_, err = getIP(1, "10.1.2.255")
	require.ErrorContains(t, err, "overflows the last octet")
}
