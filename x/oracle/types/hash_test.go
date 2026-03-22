package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

var (
	voter1 = sdk.ValAddress([]byte("addr1_______________"))
	voter2 = sdk.ValAddress([]byte("addr2_______________"))
)

func TestGetAggregateVoteHash(t *testing.T) {
	tests := []struct {
		name  string
		salt  string
		rates string
		voter sdk.ValAddress
	}{
		{
			name:  "basic hash",
			salt:  "salt",
			rates: "100ukrw,200uusd",
			voter: voter1,
		},
		{
			name:  "different salt",
			salt:  "pepper",
			rates: "100ukrw,200uusd",
			voter: voter1,
		},
		{
			name:  "different rates",
			salt:  "salt",
			rates: "999ukrw",
			voter: voter1,
		},
		{
			name:  "different voter",
			salt:  "salt",
			rates: "100ukrw,200uusd",
			voter: voter2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hash := types.GetAggregateVoteHash(tc.salt, tc.rates, tc.voter)
			require.Len(t, hash, types.TruncatedHashSize)

			// Deterministic: same inputs produce same hash
			hash2 := types.GetAggregateVoteHash(tc.salt, tc.rates, tc.voter)
			require.Equal(t, hash, hash2)

			// Round-trip: hash → hex string → decode → same hash
			hexStr := hash.String()
			decoded, err := types.AggregateVoteHashFromHexString(hexStr)
			require.NoError(t, err)
			require.Equal(t, hash, decoded)
		})
	}

	// Different inputs produce different hashes
	h1 := types.GetAggregateVoteHash("salt", "100ukrw,200uusd", voter1)
	h2 := types.GetAggregateVoteHash("pepper", "100ukrw,200uusd", voter1)
	h3 := types.GetAggregateVoteHash("salt", "999ukrw", voter1)
	h4 := types.GetAggregateVoteHash("salt", "100ukrw,200uusd", voter2)
	require.NotEqual(t, h1, h2, "different salt should produce different hash")
	require.NotEqual(t, h1, h3, "different rates should produce different hash")
	require.NotEqual(t, h1, h4, "different voter should produce different hash")
}

func TestAggregateVoteHashFromHexString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{
			name:  "valid hex",
			input: "0123456789abcdef0123456789abcdef01234567",
		},
		{
			name:      "invalid hex characters",
			input:     "xyz123",
			expectErr: true,
		},
		{
			name:      "odd length hex",
			input:     "abc",
			expectErr: true,
		},
		{
			name:  "empty string",
			input: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.AggregateVoteHashFromHexString(tc.input)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
