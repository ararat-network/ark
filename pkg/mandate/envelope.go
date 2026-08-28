package mandate

import (
	"errors"
	"fmt"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
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
		if !e.CommitteeShape.IsZero() {
			return errors.New("disabled envelope must not carry a committee shape")
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
	// The shape is observation, never authorisation, so an enabled envelope is
	// not required to carry one: an unclassified committee reads as unknown,
	// which is what a hand-written genesis that omits it means. Only the
	// internal coherence of a shape that is present is checked.
	if (e.CommitteeShape.Threshold == 0) != (e.CommitteeShape.MemberCount == 0) {
		return errors.New("committee threshold and member count must be zero together")
	}
	if e.CommitteeShape.Threshold > e.CommitteeShape.MemberCount {
		return errors.New("committee threshold cannot exceed the member count")
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
// spelling. The decoded committee is handed back with it — nil for a
// disablement — because canonicalising already decoded it, and a caller that
// needs the account would otherwise re-parse a spelling this function just
// produced, carrying an error branch that cannot be reached.
//
// Next owns derivation only. Judgment of the assembled appointment stays with
// the embedding mandate, whose Validate is the single guardian for every entry
// point — genesis import reaches state without passing here — and which names
// itself when wrapping envelope errors. Callers must validate the mandate they
// assemble around this envelope.
func Next(
	current Envelope,
	committee string,
	activationHeight uint64,
	expiryHeight uint64,
) (Envelope, sdk.AccAddress, error) {
	term, err := current.NextTerm()
	if err != nil {
		return Envelope{}, nil, err
	}
	if committee == "" {
		return Disabled(term), nil, nil
	}
	// Decoded here rather than through chain.CanonicaliseAccountAddress because
	// both halves of that decode are wanted: the bytes for the caller and the
	// canonical spelling for state.
	address, err := sdk.AccAddressFromBech32(committee)
	if err != nil {
		return Envelope{}, nil, fmt.Errorf("committee is invalid: %w", err)
	}

	return Envelope{
		Term:             term,
		Committee:        address.String(),
		ActivationHeight: activationHeight,
		ExpiryHeight:     expiryHeight,
	}, address, nil
}

// Observe records what the chain can prove about this appointment's committee,
// from the account the module resolved for it. A disabled envelope observes
// nothing, keeping the canonical disabled shape zero. [Shape] does the
// classifying, so this package still reads no module state.
//
// This runs at appointment and never again, which is sound because the
// committee address is the hash of the key it commits to: the account named by
// an appointment cannot come to require fewer signatures than it did when
// governance appointed it. Nothing here gates an action — the shape is the
// record of who holds delegated power, for the operators and indexers reading
// it, not a second authorization.
//
// Embedders promote this method rather than shadowing it, so calling it on the
// assembled mandate records the shape on the envelope it wraps.
//
// An account that is not the appointed committee records nothing. Resolving the
// account is the module's half, so this is the one thing that can be mispaired
// — and a shape describing some other address, in the field whose whole purpose
// is naming what backs this committee, is worse than no shape at all.
func (e *Envelope) Observe(account sdk.AccountI) {
	if e.IsDisabled() {
		return
	}
	if account != nil && account.GetAddress().String() != e.Committee {
		e.CommitteeShape = CommitteeShape{}
		return
	}
	e.CommitteeShape = Shape(account)
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
