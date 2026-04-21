package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

func TestGetVoteHash(t *testing.T) {
	voter1 := sdk.ValAddress([]byte("addr1_______________"))
	voter2 := sdk.ValAddress([]byte("addr2_______________"))
	baseHash := types.GetVoteHash("salt", "100ukrw,200uusd", voter1)

	tests := []struct {
		name             string
		salt             string
		rates            string
		voter            sdk.ValAddress
		expectBaselineEq bool
	}{
		{
			name:             "same inputs",
			salt:             "salt",
			rates:            "100ukrw,200uusd",
			voter:            voter1,
			expectBaselineEq: true,
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
			hash := types.GetVoteHash(tc.salt, tc.rates, tc.voter)
			require.Len(t, hash, types.TruncatedHashSize)

			if tc.expectBaselineEq {
				require.Equal(t, baseHash, hash)
				require.Equal(t, hash, types.GetVoteHash(tc.salt, tc.rates, tc.voter))

				decoded, err := types.VoteHashFromHexString(hash.String())
				require.NoError(t, err)
				require.Equal(t, hash, decoded)
			} else {
				require.NotEqual(t, baseHash, hash)
			}
		})
	}
}

func TestVoteHashFromHexString(t *testing.T) {
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
			_, err := types.VoteHashFromHexString(tc.input)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
