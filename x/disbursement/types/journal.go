package types

import (
	"errors"
	"fmt"

	"github.com/ararat-network/ark/pkg/chain"
)

// Validate checks an entry carries exactly the facts its action uses. Import also checks it against
// its grant.
func (e GrantEntry) Validate() error {
	if e.Id == 0 {
		return errors.New("grant entry needs an ID")
	}
	if !positiveInt64(e.Height) || !fitsInt64(e.Time) {
		return errors.New("invalid grant entry time or height")
	}
	if _, err := chain.ParseCanonicalAccountAddress("grant entry actor", e.Actor); err != nil {
		return err
	}
	if err := e.Amount.Validate(); err != nil {
		return err
	}
	var wantSubject, wantOffer, zeroAmount bool
	switch e.Action {
	case GrantAction_GRANT_ACTION_CREATED:
	case GrantAction_GRANT_ACTION_CONVERTED:
		wantOffer = true
	case GrantAction_GRANT_ACTION_PAID:
		wantSubject = true
	case GrantAction_GRANT_ACTION_CANCELLED:
	case GrantAction_GRANT_ACTION_PAYEE_CHANGED:
		wantSubject, zeroAmount = true, true
	case GrantAction_GRANT_ACTION_SUSPENDED, GrantAction_GRANT_ACTION_REINSTATED:
		zeroAmount = true
	default:
		return errors.New("unknown grant action")
	}
	if e.GrantId == 0 {
		return errors.New("grant entry names no grant")
	}
	if (e.Subject != "") != wantSubject {
		return errors.New("grant entry subject does not match its action")
	}
	if e.Subject != "" {
		if _, err := chain.ParseCanonicalAccountAddress("grant entry subject", e.Subject); err != nil {
			return err
		}
	}
	if err := ValidateAmount(e.Offer, wantOffer); err != nil {
		return fmt.Errorf("grant entry offer: %w", err)
	}
	if !wantOffer && !e.Offer.IsZero() {
		return errors.New("grant entry offer does not match its action")
	}
	if zeroAmount && !e.Amount.IsZero() {
		return errors.New("nonpayment grant entry moves principal")
	}
	if (e.Action == GrantAction_GRANT_ACTION_CREATED || e.Action == GrantAction_GRANT_ACTION_CONVERTED || e.Action == GrantAction_GRANT_ACTION_PAID) && !e.Amount.IsPositive() {
		return errors.New("grant entry moves nothing")
	}
	return nil
}

// Validate checks a beneficiary entry's shape; import also checks a record's entries end at its controller.
func (e BeneficiaryEntry) Validate() error {
	if e.Id == 0 {
		return errors.New("beneficiary entry needs an ID")
	}
	if !positiveInt64(e.Height) || !fitsInt64(e.Time) {
		return errors.New("invalid beneficiary entry time or height")
	}
	for _, a := range []string{e.Beneficiary, e.Actor, e.Controller} {
		if _, err := chain.ParseCanonicalAccountAddress("beneficiary entry address", a); err != nil {
			return err
		}
	}
	return nil
}
