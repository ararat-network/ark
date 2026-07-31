package chain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ark/pkg/chain"
)

func TestValidatePricedDenom(t *testing.T) {
	tests := []struct {
		name      string
		denom     string
		wantErr   bool
		errPhrase string
	}{
		{
			name:  "canonical asset denom",
			denom: "ausd",
		},
		{
			// The numeraire is a well-formed denomination but is never priced:
			// NOAH is what every other denomination is quoted against.
			name:      "numeraire",
			denom:     "anoah",
			wantErr:   true,
			errPhrase: "is the numeraire and is never priced",
		},
		{
			name:  "digits are permitted after the prefix",
			denom: "ausd2",
		},
		{
			name:  "longest permitted denom",
			denom: "a" + "bcdefghijklmnop",
		},
		{
			name:    "empty denom",
			wantErr: true,
		},
		{
			// Three characters is the floor because a denomination is also a
			// feed key and travels as an SDK denomination in DecCoins, which
			// requires three.
			name:    "denom shorter than the SDK minimum",
			denom:   "aa",
			wantErr: true,
		},
		{
			name:    "denom longer than the pattern allows",
			denom:   "a" + "bcdefghijklmnopq",
			wantErr: true,
		},
		{
			name:    "denom without the native prefix",
			denom:   "usd",
			wantErr: true,
		},
		{
			name:    "uppercase denom",
			denom:   "aUSD",
			wantErr: true,
		},
		{
			// Punctuation the SDK charset would allow is excluded: it could
			// never key a feed.
			name:    "path denom",
			denom:   "afoo/bar",
			wantErr: true,
		},
		{
			name:    "punctuated denom",
			denom:   "a.usd",
			wantErr: true,
		},
		{
			name:    "invalid SDK denom",
			denom:   "a??",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.ValidatePricedDenom(tt.denom)
			if tt.wantErr {
				phrase := tt.errPhrase
				if phrase == "" {
					phrase = "Ark-native base denom matching"
				}
				require.ErrorContains(t, err, phrase)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestMaxPricedDenomBytesMatchesPattern(t *testing.T) {
	longest := "a" + strings.Repeat("z", chain.MaxPricedDenomBytes-1)
	require.Len(t, longest, chain.MaxPricedDenomBytes)
	require.NoError(t, chain.ValidatePricedDenom(longest))
	require.Error(t, chain.ValidatePricedDenom(longest+"z"))
}

func TestNativeBaseAmount(t *testing.T) {
	require.Equal(
		t,
		"1000000000000000000",
		chain.NativeBaseAmount(1).String(),
	)
	require.Equal(
		t,
		"1000000000000000000000000",
		chain.NativeBaseAmount(1_000_000).String(),
	)
}
