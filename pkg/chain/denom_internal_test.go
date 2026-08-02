package chain

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// pricedDenomPattern is the rule isPricedDenom replaced, kept here as the
// specification the scan is checked against. pricedDenomShape is the source of
// both, so a change to the rule that forgets this test cannot compile past it.
var pricedDenomPattern = regexp.MustCompile(pricedDenomShape)

// differentialAlphabet spans every equivalence class the pattern distinguishes:
// the prefix byte, other lowercase letters, digits at both ends of the range,
// an uppercase letter, the punctuation the SDK charset would allow, a space,
// and a byte that cannot begin a rune.
var differentialAlphabet = []byte{'a', 'z', 'b', '0', '9', 'A', '/', '.', '-', ' ', 0x80, 0xff}

// TestIsPricedDenomMatchesPatternExhaustively checks the scan against the
// pattern over every string up to four bytes drawn from differentialAlphabet.
// Four is past both edges the rule has below the length bound — the prefix and
// the two-character minimum — so every disagreement about shape is reachable
// here, and TestIsPricedDenomMatchesPatternAtLengthBounds covers the other end.
func TestIsPricedDenomMatchesPatternExhaustively(t *testing.T) {
	var checked int
	var candidate []byte

	var walk func(depth int)
	walk = func(depth int) {
		denom := string(candidate)
		require.Equalf(
			t,
			pricedDenomPattern.MatchString(denom),
			isPricedDenom(denom),
			"scan and pattern disagree on %q",
			denom,
		)
		checked++
		if depth == 0 {
			return
		}
		for _, b := range differentialAlphabet {
			candidate = append(candidate, b)
			walk(depth - 1)
			candidate = candidate[:len(candidate)-1]
		}
	}
	walk(4)

	require.Equal(t, 1+12+12*12+12*12*12+12*12*12*12, checked)
}

// TestIsPricedDenomMatchesPatternAtLengthBounds checks the scan against the
// pattern either side of the length bound, which the exhaustive walk stops
// short of.
func TestIsPricedDenomMatchesPatternAtLengthBounds(t *testing.T) {
	for length := 0; length <= MaxPricedDenomBytes+2; length++ {
		for _, body := range []string{"z", "0", "A", "/"} {
			denom := strings.Repeat(body, length)
			require.Equalf(
				t,
				pricedDenomPattern.MatchString(denom),
				isPricedDenom(denom),
				"scan and pattern disagree on %q",
				denom,
			)

			prefixed := "a" + strings.Repeat(body, length)
			require.Equalf(
				t,
				pricedDenomPattern.MatchString(prefixed),
				isPricedDenom(prefixed),
				"scan and pattern disagree on %q",
				prefixed,
			)
		}
	}
}

// TestIsPricedDenomRejectsEmbeddedNewline pins the one place a hand-rolled scan
// could plausibly diverge from a pattern: Go anchors `$` at end of text rather
// than before a trailing newline, so a denomination carrying one is rejected by
// both.
func TestIsPricedDenomRejectsEmbeddedNewline(t *testing.T) {
	for _, denom := range []string{"ausd\n", "\nausd", "aus\nd", "ausd\n\n"} {
		require.False(t, pricedDenomPattern.MatchString(denom), "pattern admitted %q", denom)
		require.False(t, isPricedDenom(denom), "scan admitted %q", denom)
	}
}

func FuzzIsPricedDenomMatchesPattern(f *testing.F) {
	for _, seed := range []string{
		"", "a", "aa", "ausd", "anoah", "aUSD", "afoo/bar", "a.usd", "a??",
		"abcdefghijklmnop", "abcdefghijklmnopq", "ausd\n", "a\xff\xff", "a😀b",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, denom string) {
		require.Equalf(
			t,
			pricedDenomPattern.MatchString(denom),
			isPricedDenom(denom),
			"scan and pattern disagree on %q",
			denom,
		)
	})
}
