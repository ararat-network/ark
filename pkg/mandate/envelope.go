package mandate

import (
	"errors"
	"fmt"
	"math"

	"ark/pkg/chain"
)

// Disabled returns the canonical disabled envelope at term. A disabled
// envelope retains its term so previously prepared committee transactions
// cannot become valid again.
func Disabled(term uint64) Envelope {
	return Envelope{Term: term}
}

// IsDisabled reports whether no committee is appointed.
func (e Envelope) IsDisabled() bool {
	return e.Committee == ""
}

// IsActive reports whether the appointed committee may act at height. The
// window is half-open, so expiry_height is the first inactive height.
func (e Envelope) IsActive(height uint64) bool {
	if e.IsDisabled() {
		return false
	}

	return e.ActivationHeight <= height && height < e.ExpiryHeight
}

// Validate checks either a canonical disabled envelope or one complete
// committee appointment. Allowances, usage, and every committee power remain
// owned by the embedding mandate, which names itself when wrapping these
// errors. Embedders shadow this method with their own Validate, so they must
// call it through the Envelope field.
func (e Envelope) Validate() error {
	if e.IsDisabled() {
		if e.ActivationHeight != 0 || e.ExpiryHeight != 0 {
			return errors.New("disabled envelope must not have an activation or expiry height")
		}

		return nil
	}
	if e.Term == 0 {
		return errors.New("configured term must be positive")
	}
	if _, err := chain.ParseCanonicalAccountAddress("committee", e.Committee); err != nil {
		return err
	}
	if e.ActivationHeight >= e.ExpiryHeight {
		return errors.New("activation height must precede expiry height")
	}

	return nil
}

// NextTerm returns the successor appointment term, failing closed on overflow.
func (e Envelope) NextTerm() (uint64, error) {
	if e.Term == math.MaxUint64 {
		return 0, errors.New("term cannot advance")
	}

	return e.Term + 1, nil
}

// RequireTerm checks the exact term a committee message must carry. Term is
// the staleness guard for every committee action.
func (e Envelope) RequireTerm(expected uint64) error {
	if expected != e.Term {
		return fmt.Errorf("term mismatch: expected %d, got %d", e.Term, expected)
	}

	return nil
}
