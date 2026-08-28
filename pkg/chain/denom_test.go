package chain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/chain"
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

func TestValidateExternalDenom(t *testing.T) {
	tests := []struct {
		name      string
		denom     string
		wantErr   bool
		errPhrase string
	}{
		{
			name:  "canonical external symbol",
			denom: "ausd-x",
		},
		{
			name:  "tags distinguish external symbols sharing one feed",
			denom: "axau-lbma",
		},
		{
			name:  "digits are permitted in the tag",
			denom: "abtc-cb2",
		},
		{
			name:  "longest permitted external symbol",
			denom: "a" + strings.Repeat("z", 15) + "-" + strings.Repeat("z", 12),
		},
		{
			// The issuable name is the whole point of the partition: a bare
			// denomination cannot be listed even when it is the only external on
			// its series, or the two namespaces meet again.
			name:      "bare priced denom",
			denom:     "ausd",
			wantErr:   true,
			errPhrase: "must name a feed and a tag",
		},
		{
			// An off-chain NOAH holding is deliberately unrecognisable, and the
			// prefix rule refuses it in the numeraire's own words.
			name:      "numeraire prefix",
			denom:     "anoah-x",
			wantErr:   true,
			errPhrase: "is the numeraire and is never priced",
		},
		{
			name:      "empty tag",
			denom:     "ausd-",
			wantErr:   true,
			errPhrase: "must carry one tag",
		},
		{
			name:      "empty feed",
			denom:     "-x",
			wantErr:   true,
			errPhrase: "prices through an invalid feed",
		},
		{
			// The cut takes the first separator, so a second one lands in the
			// tag and is refused there.
			name:      "two separators",
			denom:     "ausd-x-y",
			wantErr:   true,
			errPhrase: "must carry one tag",
		},
		{
			name:      "tag longer than the rule allows",
			denom:     "ausd-" + strings.Repeat("z", chain.MaxExternalTagBytes+1),
			wantErr:   true,
			errPhrase: "must carry one tag",
		},
		{
			name:      "feed longer than the priced rule allows",
			denom:     "a" + strings.Repeat("z", 16) + "-x",
			wantErr:   true,
			errPhrase: "prices through an invalid feed",
		},
		{
			name:      "uppercase tag",
			denom:     "ausd-X",
			wantErr:   true,
			errPhrase: "must carry one tag",
		},
		{
			name:      "punctuated tag",
			denom:     "ausd-x.y",
			wantErr:   true,
			errPhrase: "must carry one tag",
		},
		{
			name:      "feed without the native prefix",
			denom:     "usd-x",
			wantErr:   true,
			errPhrase: "prices through an invalid feed",
		},
		{
			name:      "empty denom",
			wantErr:   true,
			errPhrase: "must name a feed and a tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.ValidateExternalDenom(tt.denom)
			if tt.wantErr {
				require.ErrorContains(t, err, tt.errPhrase)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestExternalFeedDerivesTheSeriesUnconditionally pins the property the partition
// rests on: the feed an external symbol prices through is a function of its text alone,
// and several tags resolve to one series.
func TestExternalFeedDerivesTheSeriesUnconditionally(t *testing.T) {
	for _, denom := range []string{"axau-lbma", "axau-cb", "axau-z9"} {
		feed, ok := chain.ExternalFeed(denom)
		require.Truef(t, ok, "%s is external-shaped", denom)
		require.Equal(t, "axau", feed)
	}

	// A registry member and the numeraire are not external symbols and derive nothing:
	// callers holding either price through the denomination itself.
	for _, denom := range []string{"ausd", "anoah", "anoah-x", "", "-", "ausd-"} {
		_, ok := chain.ExternalFeed(denom)
		require.Falsef(t, ok, "%q must not be external-shaped", denom)
	}
}

func TestMaxExternalDenomBytesMatchesTheRule(t *testing.T) {
	longest := "a" + strings.Repeat("z", chain.MaxPricedDenomBytes-1) +
		chain.ExternalSeparator + strings.Repeat("z", chain.MaxExternalTagBytes)
	require.Len(t, longest, chain.MaxExternalDenomBytes)
	require.NoError(t, chain.ValidateExternalDenom(longest))
	require.Error(t, chain.ValidateExternalDenom(longest+"z"))
}

// TestExternalSeparatorIsNeverAPricedDenom is the partition theorem itself, at the
// only place it can be stated: no external symbol is a priced denomination, so no
// eligibility entry can name a registrable asset and no registration can name an
// external symbol.
func TestExternalSeparatorIsNeverAPricedDenom(t *testing.T) {
	for _, denom := range []string{"ausd-x", "axau-lbma", "abtc-cb2", "anoah-x"} {
		require.Errorf(t, chain.ValidatePricedDenom(denom), "priced rule admitted external symbol %s", denom)
	}
	for _, denom := range []string{"ausd", "axau", "abtc", "anoah"} {
		require.Errorf(t, chain.ValidateExternalDenom(denom), "external rule admitted priced denom %s", denom)
	}
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
