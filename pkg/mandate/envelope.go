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

// Validate checks canonical disabled or complete enabled envelopes. Embedding mandates own payload
// validation and must call Envelope.Validate explicitly when shadowing this method.
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
	if e.CommitteeShape.MemberCount != 0 && e.CommitteeShape.KeyKind != CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG {
		return errors.New("committee threshold and member count belong to a multisig shape")
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

// Next derives the advanced-term envelope, canonicalises the committee, and returns its decoded
// address or nil when disabled. It does not validate the assembled module mandate; callers must do
// so before storing it.
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

// Observe records provable account backing at appointment without authorising actions. Disabled or
// mismatched accounts retain no shape. Contract shape records only its kind because membership and
// code may change; see README.md.
func (e *Envelope) Observe(account sdk.AccountI, contract bool) {
	if e.IsDisabled() {
		return
	}
	if account != nil && account.GetAddress().String() != e.Committee {
		e.CommitteeShape = CommitteeShape{}
		return
	}
	e.CommitteeShape = Shape(account, contract)
}

// RequireTerm checks the exact term a committee message must carry. Term is
// the staleness guard for every committee action.
func (e Envelope) RequireTerm(expected uint64) error {
	if expected != e.Term {
		return fmt.Errorf("term mismatch: expected %d, got %d", e.Term, expected)
	}

	return nil
}

// Authorise checks canonical signer, exact term, then active window, so stale transactions report
// term mismatch first. Embedded mandates enforce usage, corridors, and status preconditions.
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
