package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ararat-network/ark/pkg/chain"
)

// Pair identifies an oracle price pair in canonical BASE/QUOTE form.
type Pair string

// NewPair returns a canonical pair from base and quote components.
func NewPair(base, quote string) (Pair, error) {
	base = strings.ToUpper(strings.TrimSpace(base))
	quote = strings.ToUpper(strings.TrimSpace(quote))

	pair := Pair(base + "/" + quote)
	if err := pair.Validate(); err != nil {
		return "", err
	}

	return pair, nil
}

// FromDenom converts a canonical a-prefixed feed denom into its corresponding
// NOAH/QUOTE pair.
func FromDenom(denom string) (Pair, error) {
	if err := chain.ValidatePricedDenom(denom); err != nil {
		return "", err
	}

	return NewPair("NOAH", strings.ToUpper(denom[1:]))
}

// ParsePair parses raw into a canonical BASE/QUOTE pair.
func ParsePair(raw string) (Pair, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("pair is empty")
	}

	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid pair %q: expected BASE/QUOTE", raw)
	}

	return NewPair(parts[0], parts[1])
}

// Validate checks that p is a canonical BASE/QUOTE pair.
func (p Pair) Validate() error {
	raw := string(p)
	if raw == "" {
		return errors.New("pair is empty")
	}

	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid pair %q: expected BASE/QUOTE", raw)
	}

	base, quote := parts[0], parts[1]
	if base == "" {
		return errors.New("pair base is empty")
	}
	if quote == "" {
		return errors.New("pair quote is empty")
	}
	if strings.TrimSpace(base) != base {
		return fmt.Errorf("pair base contains whitespace: %q", base)
	}
	if strings.TrimSpace(quote) != quote {
		return fmt.Errorf("pair quote contains whitespace: %q", quote)
	}
	if base != strings.ToUpper(base) {
		return fmt.Errorf("pair base must be uppercase: %q", base)
	}
	if quote != strings.ToUpper(quote) {
		return fmt.Errorf("pair quote must be uppercase: %q", quote)
	}

	return nil
}

// Base returns the pair base component. Callers should only use this on
// validated canonical pairs.
func (p Pair) Base() string {
	base, _ := p.components()
	return base
}

// Quote returns the pair quote component. Callers should only use this on
// validated canonical pairs.
func (p Pair) Quote() string {
	_, quote := p.components()
	return quote
}

// Denom returns the public feed denom represented by the pair quote.
func (p Pair) Denom() string {
	return "a" + strings.ToLower(p.Quote())
}

// Inverse returns the reciprocal pair. Callers should only use this on
// validated canonical pairs.
func (p Pair) Inverse() Pair {
	base, quote := p.components()
	return Pair(quote + "/" + base)
}

// String returns the pair as BASE/QUOTE.
func (p Pair) String() string {
	return string(p)
}

func (p Pair) components() (string, string) {
	base, quote, _ := strings.Cut(string(p), "/")
	return base, quote
}
