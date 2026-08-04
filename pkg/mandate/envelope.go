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

// Next derives the successor appointment envelope from the current one: the
// canonical disabled envelope when committee is empty, otherwise the complete
// appointment at the advanced term. Every replacement advances the term, so a
// disablement retains its successor term too. The committee is stored in its
// canonical spelling whatever letter case the message carried, so everything
// downstream — Validate, authority-distinctness checks, events — reads one
// spelling.
//
// Next owns derivation only. Judgment of the assembled appointment stays with
// the embedding mandate, whose Validate is the single guardian for every entry
// point — genesis import reaches state without passing here — and which names
// itself when wrapping envelope errors. Callers must validate the mandate they
// assemble around this envelope.
func Next(current Envelope, committee string, activationHeight uint64, expiryHeight uint64) (Envelope, error) {
	term, err := current.NextTerm()
	if err != nil {
		return Envelope{}, err
	}
	if committee == "" {
		return Disabled(term), nil
	}
	canonical, err := chain.CanonicaliseAccountAddress("committee", committee)
	if err != nil {
		return Envelope{}, err
	}

	return Envelope{
		Term:             term,
		Committee:        canonical,
		ActivationHeight: activationHeight,
		ExpiryHeight:     expiryHeight,
	}, nil
}

// RequireTerm checks the exact term a committee message must carry. Term is
// the staleness guard for every committee action.
func (e Envelope) RequireTerm(expected uint64) error {
	if expected != e.Term {
		return fmt.Errorf("term mismatch: expected %d, got %d", e.Term, expected)
	}

	return nil
}

// Authorise checks one committee action against the appointment: the exact
// signer, the exact term, then the active window. Term precedes window so a
// stale committee transaction reports the staleness that produced it rather
// than whichever window it straddles. Everything beyond the appointment —
// usage bounds, policy corridors, status preconditions — stays with the
// embedding mandate, which names itself when wrapping these errors.
//
// The signer is compared in canonical spelling because the stored committee is
// canonical while the ante handler authenticates a signer by decoded bytes, so
// any letter case of the appointed address is the same authenticated account.
func (e Envelope) Authorise(signer string, expectedTerm uint64, height uint64) error {
	if e.IsDisabled() {
		return errors.New("signer is not the exact appointed committee")
	}
	canonical, err := chain.CanonicaliseAccountAddress("committee signer", signer)
	if err != nil {
		return err
	}
	if canonical != e.Committee {
		return errors.New("signer is not the exact appointed committee")
	}
	if err := e.RequireTerm(expectedTerm); err != nil {
		return err
	}
	if !e.IsActive(height) {
		return fmt.Errorf(
			"mandate is not active: window is [%d, %d)",
			e.ActivationHeight,
			e.ExpiryHeight,
		)
	}

	return nil
}
