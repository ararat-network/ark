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

// externalDenomPattern is the specification the external rule's two entry points are
// checked against, sourced from externalDenomShape exactly as the priced pattern
// is sourced from pricedDenomShape.
var externalDenomPattern = regexp.MustCompile(externalDenomShape)

// externalShaped reports the pattern's verdict less the numeraire exclusion, which
// no pattern over the symbol alone can express.
func externalShaped(denom string) bool {
	if !externalDenomPattern.MatchString(denom) {
		return false
	}
	feed, _, _ := strings.Cut(denom, ExternalSeparator)

	return feed != NoahBaseDenom
}

// TestExternalRuleMatchesPatternExhaustively walks every string up to five bytes
// over an alphabet including the separator, which is one byte past the shortest
// external symbol the rule admits (`aXX-X`), so every disagreement about shape is
// reachable. Both entry points are checked against the one pattern, which is
// what keeps the scanner ExternalFeed uses and the errors ValidateExternalDenom
// reports from ever disagreeing about what an external symbol is.
func TestExternalRuleMatchesPatternExhaustively(t *testing.T) {
	alphabet := []byte{'a', 'z', '0', '-', 'A', '.', 'n'}

	var candidate []byte
	var walk func(depth int)
	walk = func(depth int) {
		denom := string(candidate)
		want := externalShaped(denom)

		_, ok := ExternalFeed(denom)
		require.Equalf(t, want, ok, "ExternalFeed and pattern disagree on %q", denom)
		require.Equalf(
			t,
			want,
			ValidateExternalDenom(denom) == nil,
			"ValidateExternalDenom and pattern disagree on %q",
			denom,
		)

		if depth == 0 {
			return
		}
		for _, b := range alphabet {
			candidate = append(candidate, b)
			walk(depth - 1)
			candidate = candidate[:len(candidate)-1]
		}
	}
	walk(5)
}

// TestExternalRuleAndPricedRuleAreDisjoint is the partition stated over the two
// scanners rather than over examples: no string satisfies both rules, which is
// what makes "policy ∩ registry = ∅" a theorem instead of a check.
func TestExternalRuleAndPricedRuleAreDisjoint(t *testing.T) {
	alphabet := []byte{'a', 'z', '0', '-', 'A', 'n'}

	var candidate []byte
	var walk func(depth int)
	walk = func(depth int) {
		denom := string(candidate)
		_, external := ExternalFeed(denom)
		require.Falsef(
			t,
			external && isPricedDenom(denom),
			"%q satisfies both denomination rules",
			denom,
		)

		if depth == 0 {
			return
		}
		for _, b := range alphabet {
			candidate = append(candidate, b)
			walk(depth - 1)
			candidate = candidate[:len(candidate)-1]
		}
	}
	walk(5)
}

func FuzzExternalRuleMatchesPattern(f *testing.F) {
	for _, seed := range []string{
		"", "-", "a-", "-a", "ausd", "ausd-x", "axau-lbma", "anoah-x", "ausd-x-y",
		"ausd-X", "ausd-" + strings.Repeat("z", MaxExternalTagBytes+1),
		"a" + strings.Repeat("z", 16) + "-x", "ausd\n-x", "a😀-x",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, denom string) {
		want := externalShaped(denom)

		feed, ok := ExternalFeed(denom)
		require.Equalf(t, want, ok, "ExternalFeed and pattern disagree on %q", denom)
		require.Equalf(
			t,
			want,
			ValidateExternalDenom(denom) == nil,
			"ValidateExternalDenom and pattern disagree on %q",
			denom,
		)
		if ok {
			require.Equalf(t, denom, feed+ExternalSeparator+denom[len(feed)+1:], "%q does not rebuild from its feed", denom)
			require.NoErrorf(t, ValidatePricedDenom(feed), "%q derived an invalid feed key", denom)
		}
	})
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
