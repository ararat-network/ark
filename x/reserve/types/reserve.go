package types

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
)

// MaxReferenceLength bounds one free-form evidence string. A reference
// carries what the chain cannot verify — an external transaction hash, a
// custodian account identifier — so it is capped rather than structured.
const MaxReferenceLength = 512

// MaxAttestedQuantity bounds one position's attested holding, and exists to
// make the recognition fold safe by inspection rather than by projection.
//
// The quantity is a committee attestation about custody the chain cannot see,
// so nothing else bounds it — and it is read every block by the fold Treasury
// settles against, where an arithmetic failure is a halt rather than a refused
// message. Two steps amplify it: the per-denomination sum over the open set,
// and the credit conversion, which divides by a rate as small as 10^-18. With
// the field under 2^128 the sum needs more than 2^128 positions to leave Int
// range, and the conversion lands under 2^188, leaving sixty-eight bits of
// headroom under the LegacyDec limit. The cap is far past any custody a
// Reserve could attest — 2^128 base units is on the order of 10^20 whole units
// at the eighteen-decimal convention — so it makes an absurd attestation
// impossible rather than a sensible one difficult.
var MaxAttestedQuantity = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))

// ReserveMandateLabel names the shared appointment envelope in Reserve errors.
const ReserveMandateLabel = "Reserve mandate"

// DefaultReserveMandate returns the canonical unconfigured sentinel.
func DefaultReserveMandate() ReserveMandate {
	return NewDisabledReserveMandate(0)
}

// NewDisabledReserveMandate returns the canonical disabled mandate at the
// supplied term.
func NewDisabledReserveMandate(term uint64) ReserveMandate {
	return ReserveMandate{
		Envelope:            mandate.Disabled(term),
		DeploymentAllowance: chain.NoahCoin(math.ZeroInt()),
		MinimumNoahBalance:  chain.NoahCoin(math.ZeroInt()),
	}
}

// Validate validates either the exact unconfigured sentinel or one complete
// governed Reserve mandate.
func (reserveMandate ReserveMandate) Validate() error {
	if err := chain.ValidateNoahCoin("deployment allowance", reserveMandate.DeploymentAllowance); err != nil {
		return err
	}
	if err := chain.ValidateNoahCoin("minimum NOAH balance", reserveMandate.MinimumNoahBalance); err != nil {
		return err
	}
	if err := reserveMandate.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", ReserveMandateLabel, err)
	}
	if reserveMandate.IsDisabled() {
		if !reserveMandate.DeploymentAllowance.Amount.IsZero() ||
			!reserveMandate.MinimumNoahBalance.Amount.IsZero() ||
			len(reserveMandate.Destinations) != 0 {
			return errors.New("unconfigured Reserve mandate must be empty")
		}
		return nil
	}

	if !reserveMandate.DeploymentAllowance.Amount.IsPositive() {
		return errors.New("reserve deployment allowance must be positive")
	}
	if len(reserveMandate.Destinations) == 0 {
		return errors.New("configured Reserve mandate must name at least one destination")
	}
	seen := make(map[string]struct{}, len(reserveMandate.Destinations))
	for _, destination := range reserveMandate.Destinations {
		if _, err := chain.ParseCanonicalAccountAddress("Reserve destination", destination); err != nil {
			return err
		}
		if _, duplicate := seen[destination]; duplicate {
			return fmt.Errorf("duplicate Reserve destination %s", destination)
		}
		seen[destination] = struct{}{}
	}

	return nil
}

// AllowsDestination reports whether the mandate permits paying destination.
func (reserveMandate ReserveMandate) AllowsDestination(destination string) bool {
	for _, allowed := range reserveMandate.Destinations {
		if allowed == destination {
			return true
		}
	}
	return false
}

// Validate validates the immutable shape of one position record. A position's
// quantity is the committee's attestation, so what is validated here is
// coherence, never that the holding exists.
func (position Position) Validate() error {
	if position.PositionId == 0 {
		return errors.New("position ID must be positive")
	}
	if err := position.Quantity.Validate(); err != nil {
		return fmt.Errorf("invalid position quantity: %w", err)
	}
	if position.Quantity.Amount.IsNegative() {
		return errors.New("position quantity must be zero or positive")
	}
	// The one bound on the attestation, and the reason the recognition fold's
	// arithmetic cannot be driven out of range by what a committee writes here.
	if position.Quantity.Amount.GT(MaxAttestedQuantity) {
		return fmt.Errorf(
			"position quantity must not exceed %s: %s",
			MaxAttestedQuantity,
			position.Quantity.Amount,
		)
	}
	// Registry membership is not checked here: a stored record must not be
	// re-judged against live state, so the keeper answers membership where a
	// denomination is set and this answers only for shape.
	if err := validateHeldDenom(position.Quantity.Denom); err != nil {
		return fmt.Errorf("invalid position asset: %w", err)
	}
	if err := chain.ValidateNoahCoin("position deployed", position.Deployed); err != nil {
		return err
	}
	if err := chain.ValidateNoahCoin("position returned", position.Returned); err != nil {
		return err
	}
	if !position.Deployed.Amount.IsPositive() {
		return errors.New("position deployed must be positive")
	}
	if err := ValidateReference("venue reference", position.VenueReference, true); err != nil {
		return err
	}
	if position.OpenedHeight == 0 {
		return errors.New("position opened height must be positive")
	}
	if position.IsClosed() && position.ClosedHeight < position.OpenedHeight {
		return errors.New("closed position cannot close before it opened")
	}

	return nil
}

// IsClosed reports whether the position has been closed. The closing height
// is the only status a bare Position carries.
func (position Position) IsClosed() bool {
	return position.ClosedHeight != 0
}

// Realised returns the position's profit or loss: what came back less what
// went out, both proven coin movements.
func (position Position) Realised() math.Int {
	return position.Returned.Amount.Sub(position.Deployed.Amount)
}

// Validate validates the immutable shape of one ledger entry.
func (entry AccountingEntry) Validate() error {
	if entry.EntryId == 0 {
		return errors.New("entry ID must be positive")
	}
	if entry.PositionId == 0 {
		return errors.New("entry must name a position")
	}
	switch entry.Kind {
	case EntryKind_ENTRY_KIND_CORRECTION, EntryKind_ENTRY_KIND_RETURN_REVERSAL:
		if entry.Corrects == 0 {
			return errors.New("a correction or reversal must name the entry it restates")
		}
		if entry.Corrects >= entry.EntryId {
			return errors.New("a correction or reversal must restate an earlier entry")
		}
	case EntryKind_ENTRY_KIND_DEPLOYMENT,
		EntryKind_ENTRY_KIND_QUANTITY_UPDATE,
		EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
		EntryKind_ENTRY_KIND_IMPAIRMENT,
		EntryKind_ENTRY_KIND_CLOSURE:
		if entry.Corrects != 0 {
			return errors.New("only a correction or reversal may name a restated entry")
		}
	default:
		return errors.New("entry kind is invalid")
	}

	if err := entry.Quantity.Validate(); err != nil {
		return fmt.Errorf("invalid entry quantity: %w", err)
	}
	if entry.Quantity.Amount.IsNegative() {
		return errors.New("entry quantity must be zero or positive")
	}
	if err := chain.ValidateNoahCoin("entry moved noah value", entry.MovedNoahValue); err != nil {
		return err
	}
	if err := entry.MovedCoin.Validate(); err != nil {
		return fmt.Errorf("invalid entry moved coin: %w", err)
	}
	if entry.MovedCoin.Amount.IsNegative() {
		return errors.New("entry moved coin must be zero or positive")
	}

	// The movement kinds each require a coin to have moved; the judgment kinds
	// require that none did. Both fields are governed together, so a judgment
	// cannot smuggle in half a movement.
	switch entry.Kind {
	case EntryKind_ENTRY_KIND_DEPLOYMENT:
		if !entry.MovedCoin.Amount.IsPositive() {
			return errors.New("deployment must move a positive coin")
		}
		// An outflow may wait for a price, so it is booked at a real one.
		if !entry.MovedNoahValue.Amount.IsPositive() {
			return errors.New("deployment must carry a positive booked value")
		}
	case EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
		EntryKind_ENTRY_KIND_RETURN_REVERSAL:
		if !entry.MovedCoin.Amount.IsPositive() {
			return errors.New("a return attribution or its reversal must carry a positive coin")
		}
		// A zero booked value is legal here and only here: a live feed priced
		// the return to dust. An absent feed never lands here — valueMovement
		// refuses it. A reversal carries that movement verbatim, so the shape
		// admitted here is admitted back.
	default:
		if entry.MovedCoin.Amount.IsPositive() || entry.MovedNoahValue.Amount.IsPositive() {
			return errors.New(
				"only a deployment, return attribution, or return reversal may carry a movement",
			)
		}
	}

	// NOAH is its own valuation, so the par case must agree with itself.
	if entry.MovedCoin.Denom == chain.NoahBaseDenom &&
		!entry.MovedCoin.Amount.Equal(entry.MovedNoahValue.Amount) {
		return fmt.Errorf(
			"entry moves %s but books %s: NOAH is valued at par",
			entry.MovedCoin,
			entry.MovedNoahValue,
		)
	}

	// The kinds that assert or retract a coin movement must name their
	// evidence; the judgment kinds stay exempt. A reversal is included: it
	// contradicts a movement the ledger records, as the attribution asserted it.
	carriesMovement := entry.Kind == EntryKind_ENTRY_KIND_DEPLOYMENT ||
		entry.Kind == EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION ||
		entry.Kind == EntryKind_ENTRY_KIND_RETURN_REVERSAL
	if err := ValidateReference("entry reference", entry.Reference, carriesMovement); err != nil {
		return err
	}
	if _, err := chain.ParseCanonicalAccountAddress("entry recorded by", entry.RecordedBy); err != nil {
		return err
	}
	if entry.Height == 0 {
		return errors.New("entry height must be positive")
	}

	// Governance acts carry term zero; a committee act names the term it was
	// authorised under. Impairment, correction, closure, and return reversal
	// may be written by either authority, so they accept both; the
	// committee-only kinds must name a term.
	switch entry.Kind {
	case EntryKind_ENTRY_KIND_IMPAIRMENT,
		EntryKind_ENTRY_KIND_CORRECTION,
		EntryKind_ENTRY_KIND_CLOSURE,
		EntryKind_ENTRY_KIND_RETURN_REVERSAL:
	default:
		if entry.Term == 0 {
			return errors.New("a committee entry must name the term it acted under")
		}
	}

	return nil
}

// validateHeldDenom admits the two denomination classes the Reserve can hold
// a position in — an external symbol naming off-chain custody, or a bare
// priced denomination naming Ark-issued paper bought back — and reports both
// refusals when a denomination is neither.
func validateHeldDenom(denom string) error {
	if _, isExternal := chain.ExternalFeed(denom); isExternal {
		return nil
	}
	if err := chain.ValidatePricedDenom(denom); err != nil {
		return fmt.Errorf(
			"%w; nor is it an external symbol naming external custody",
			err,
		)
	}

	return nil
}

// ValidateReference validates a bounded free-form evidence field.
func ValidateReference(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if len(value) > MaxReferenceLength {
		return fmt.Errorf("%s must not exceed %d bytes", field, MaxReferenceLength)
	}
	return nil
}
