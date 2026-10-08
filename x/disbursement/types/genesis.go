package types

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// DefaultGenesisState holds no commitments or tranches.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:                 DefaultParams(),
		GrantsMandate:          DefaultGrantsMandate(),
		CompensationUsed:       math.ZeroInt(),
		RegistrarMandate:       DefaultRegistrarMandate(),
		MemberTranche:          math.ZeroInt(),
		NextGrantId:            1,
		NextGrantEntryId:       1,
		NextBeneficiaryEntryId: 1,
	}
}

// Validate checks imported records, aggregate counters, identities and audit continuity. Every list
// arrives in storage-key order, which is the duplicate check too.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.GrantsMandate.Validate(); err != nil {
		return err
	}
	if err := gs.RegistrarMandate.Validate(); err != nil {
		return err
	}
	if err := gs.RegistrarMandate.WithinCommittee(gs.GrantsMandate); err != nil {
		return err
	}
	books, err := validateBooks(gs.Books)
	if err != nil {
		return err
	}
	beneficiaries, err := validateBeneficiaries(gs.Beneficiaries)
	if err != nil {
		return err
	}
	if err := ValidateAmount(gs.CompensationUsed, false); err != nil {
		return fmt.Errorf("term usage: %w", err)
	}
	if err := ValidateAmount(gs.MemberTranche, false); err != nil {
		return fmt.Errorf("member tranche: %w", err)
	}
	totals, err := foldGrants(gs, beneficiaries)
	if err != nil {
		return err
	}
	if err := totals.reconcile(gs, books); err != nil {
		return err
	}
	if err := validateGrantJournal(gs.Grants, gs.GrantJournal, gs.NextGrantEntryId); err != nil {
		return err
	}
	return validateBeneficiaryJournal(gs.Beneficiaries, gs.BeneficiaryJournal, gs.NextBeneficiaryEntryId)
}

// validateBooks checks each book and that the books are sorted by unique account.
func validateBooks(list []AccountBook) (map[string]AccountBook, error) {
	books := make(map[string]AccountBook, len(list))
	for i, b := range list {
		if err := b.Validate(); err != nil {
			return nil, fmt.Errorf("%s book: %w", b.Account, err)
		}
		if i > 0 && b.Account <= list[i-1].Account {
			return nil, errors.New("genesis books must be sorted by unique account")
		}
		books[b.Account] = b
	}
	return books, nil
}

// validateBeneficiaries checks each record and address order, and returns the addresses on record.
func validateBeneficiaries(list []Beneficiary) (map[string]bool, error) {
	beneficiaries := make(map[string]bool, len(list))
	var previous sdk.AccAddress
	for i, b := range list {
		if err := b.Validate(); err != nil {
			return nil, fmt.Errorf("beneficiary %s: %w", b.Address, err)
		}
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", b.Address)
		if err != nil {
			return nil, err
		}
		if i > 0 && bytes.Compare(address, previous) <= 0 {
			return nil, errors.New("genesis beneficiaries must be sorted by unique address")
		}
		previous = address
		beneficiaries[b.Address] = true
	}
	return beneficiaries, nil
}

// grantTotals is what the grant records imply for state stored apart from them.
type grantTotals struct {
	books            map[string]AccountBook
	ownership        map[string]math.Int
	liveCompensation math.Int
}

// foldGrants checks each grant against the records around it and sums what the grants imply.
func foldGrants(gs GenesisState, beneficiaries map[string]bool) (grantTotals, error) {
	if gs.NextGrantId != uint64(len(gs.Grants))+1 {
		return grantTotals{}, fmt.Errorf("next grant ID %d does not follow %d grant records", gs.NextGrantId, len(gs.Grants))
	}
	t := grantTotals{
		books:            make(map[string]AccountBook),
		ownership:        make(map[string]math.Int),
		liveCompensation: math.ZeroInt(),
	}
	for _, account := range PoolAccountNames() {
		t.books[account] = EmptyBook(account)
	}
	members := make(map[string]uint64)
	committee, registrar := gs.GrantsMandate, gs.RegistrarMandate
	for i, g := range gs.Grants {
		if err := g.Validate(); err != nil {
			return grantTotals{}, fmt.Errorf("grant %d: %w", g.Id, err)
		}
		if g.Id != uint64(i)+1 {
			return grantTotals{}, fmt.Errorf("grant %d is record %d; grant IDs must run from one without a gap", g.Id, i+1)
		}
		// A member's term is the registrar's, an award's the committee's; the live committee term
		// sums what its awards charged.
		switch term := g.MandateTerm; {
		case term == 0:
		case g.Kind == GrantKind_GRANT_KIND_MEMBER:
			if term > registrar.Term {
				return grantTotals{}, fmt.Errorf("grant %d carries registrar term %d beyond the live term %d", g.Id, term, registrar.Term)
			}
		default:
			if term > committee.Term {
				return grantTotals{}, fmt.Errorf("grant %d carries committee term %d beyond the live term %d", g.Id, term, committee.Term)
			}
			if term == committee.Term {
				t.liveCompensation = t.liveCompensation.Add(g.AwardValue)
			}
		}
		if g.Kind == GrantKind_GRANT_KIND_MEMBER {
			if first, ok := members[g.Beneficiary]; ok {
				return grantTotals{}, fmt.Errorf("grants %d and %d both register member %s", first, g.Id, g.Beneficiary)
			}
			members[g.Beneficiary] = g.Id
		} else if !beneficiaries[g.Beneficiary] {
			return grantTotals{}, fmt.Errorf("grant %d has no beneficiary record for %s", g.Id, g.Beneficiary)
		}
		account := Account(g.Kind)
		book := t.books[account]
		if err := book.Reserve(sdk.NewCoin(g.Amount.Denom, g.Remaining)); err != nil {
			return grantTotals{}, fmt.Errorf("%s %w", account, err)
		}
		t.books[account] = book
		if g.Kind == GrantKind_GRANT_KIND_OWNERSHIP {
			// A grant commits what it has paid and still owes; a cancel has already dropped the rest.
			committed, err := amountOf(t.ownership, g.Beneficiary).SafeAdd(g.Paid.Add(g.Remaining))
			if err != nil {
				return grantTotals{}, fmt.Errorf("%s ownership: %w", g.Beneficiary, err)
			}
			t.ownership[g.Beneficiary] = committed
		}
	}
	return t, nil
}

// reconcile requires the stored aggregates to agree with what the grants imply.
func (t grantTotals) reconcile(gs GenesisState, listed map[string]AccountBook) error {
	// The usage figure is state an appointment resets and awards grow; the grants must agree.
	if !gs.CompensationUsed.Equal(t.liveCompensation) {
		return fmt.Errorf("term usage %s differs from the live term's awards %s", gs.CompensationUsed, t.liveCompensation)
	}
	if t.liveCompensation.GT(gs.GrantsMandate.CompensationAllowance) {
		return fmt.Errorf("the live term's awards %s exceed its allowance %s", t.liveCompensation, gs.GrantsMandate.CompensationAllowance)
	}
	for _, b := range gs.Beneficiaries {
		if committed := amountOf(t.ownership, b.Address); !committed.Equal(b.OwnershipCommitted) {
			return fmt.Errorf("beneficiary %s ownership committed %s differs from its grants' %s", b.Address, b.OwnershipCommitted, committed)
		}
	}
	// An all-zero book may be listed or omitted. The member tranche is governance state that import
	// checks against Bank rather than derives from grants.
	for _, account := range PoolAccountNames() {
		actual, ok := listed[account]
		if !ok {
			actual = EmptyBook(account)
		}
		if !actual.Reserved.Equal(t.books[account].Reserved) {
			return fmt.Errorf("%s reservations %s differ from the grants' %s", account, actual.Reserved, t.books[account].Reserved)
		}
	}
	return nil
}

// validateGrantJournal checks the grant journal against its grants, which arrive checked with IDs
// running from one. The journal arrives record-major as the store walks it, so each grant's entries
// are one run in entry ID order; the IDs, one sequence across grants, must still run from one without
// a gap. A first pass checks each entry alone; a second walks the grants and their runs together.
func validateGrantJournal(grants []Grant, journal []GrantEntry, next uint64) error {
	entries := len(journal)
	if next != uint64(entries)+1 {
		return fmt.Errorf("next grant entry ID %d does not follow %d entries", next, entries)
	}
	seen, stamps := make([]bool, entries), make([][2]uint64, entries)
	for i, e := range journal {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("grant entry %d: %w", e.Id, err)
		}
		if i > 0 && cmp.Or(cmp.Compare(e.GrantId, journal[i-1].GrantId), cmp.Compare(e.Id, journal[i-1].Id)) <= 0 {
			return errors.New("genesis grant journal must be sorted by unique grant and entry ID")
		}
		if e.Id > uint64(entries) || seen[e.Id-1] {
			return fmt.Errorf("grant entry %d: entry IDs must run from one without a gap", e.Id)
		}
		seen[e.Id-1], stamps[e.Id-1] = true, [2]uint64{e.Height, e.Time}
		if e.GrantId > uint64(len(grants)) {
			return fmt.Errorf("grant entry %d references unknown grant %d", e.Id, e.GrantId)
		}
	}
	// A grant's journal opens with its creation, so no entry predates the grant either.
	if err := blockOrder("grant", stamps); err != nil {
		return err
	}
	i := 0
	for _, g := range grants {
		run, payee, suspended, cancelled := i, g.Beneficiary, false, false
		paid, freed := math.ZeroInt(), math.ZeroInt()
		var suspendedTime uint64
		for ; i < entries && journal[i].GrantId == g.Id; i++ {
			e := journal[i]
			if e.Amount.Denom != g.Amount.Denom {
				return fmt.Errorf("grant entry %d moves %s; grant %d pays %s", e.Id, e.Amount.Denom, g.Id, g.Amount.Denom)
			}
			// Nothing journals a grant before it exists.
			if (e.Action == GrantAction_GRANT_ACTION_CREATED) != (i == run) {
				return fmt.Errorf("grant entry %d: grant %d's journal must open with its creation, once", e.Id, g.Id)
			}
			var err error
			switch e.Action {
			case GrantAction_GRANT_ACTION_CREATED:
				if !e.Amount.Equal(g.Amount) || e.Time != g.CreatedTime || e.Height != g.CreatedHeight || e.Actor != g.CreatedBy {
					return fmt.Errorf("grant entry %d does not record grant %d's creation", e.Id, g.Id)
				}
			case GrantAction_GRANT_ACTION_CONVERTED:
				if g.Amount.Denom == chain.NoahBaseDenom {
					return fmt.Errorf("grant entry %d converts for NOAH grant %d", e.Id, g.Id)
				}
				// A conversion funds its award in the award's own write, the entry after its creation.
				if c := journal[run]; i != run+1 || e.Id != c.Id+1 || e.Height != c.Height || e.Time != c.Time || e.Actor != c.Actor {
					return fmt.Errorf("grant entry %d does not fund grant %d's award", e.Id, g.Id)
				}
			case GrantAction_GRANT_ACTION_PAYEE_CHANGED:
				if e.Subject == payee {
					return fmt.Errorf("grant entry %d changes nothing: grant %d already pays %s", e.Id, g.Id, payee)
				}
				payee = e.Subject
			case GrantAction_GRANT_ACTION_PAID:
				if e.Subject != payee {
					return fmt.Errorf("grant entry %d pays %s; grant %d was paying %s", e.Id, e.Subject, g.Id, payee)
				}
				paid, err = paid.SafeAdd(e.Amount.Amount)
			case GrantAction_GRANT_ACTION_CANCELLED:
				if cancelled {
					return fmt.Errorf("grant entry %d cancels grant %d again", e.Id, g.Id)
				}
				// A cancel freezes entitlement when it happens, or at the suspension it ends.
				cutoff := e.Time
				if suspended {
					cutoff = suspendedTime
				}
				if g.Cancelled() && cutoff != g.CutoffTime {
					return fmt.Errorf("grant entry %d cancels grant %d at %d; the grant was cut off at %d", e.Id, g.Id, cutoff, g.CutoffTime)
				}
				// Cancellation clears a suspension without an entry of its own.
				cancelled, suspended = true, false
				freed, err = freed.SafeAdd(e.Amount.Amount)
			case GrantAction_GRANT_ACTION_SUSPENDED, GrantAction_GRANT_ACTION_REINSTATED:
				// Only a live member is suspended and reinstated, in turn.
				if g.Kind != GrantKind_GRANT_KIND_MEMBER || cancelled || suspended != (e.Action == GrantAction_GRANT_ACTION_REINSTATED) {
					return fmt.Errorf("grant entry %d: %s is out of turn for grant %d", e.Id, e.Action, g.Id)
				}
				suspended = !suspended
				if suspended {
					suspendedTime = e.Time
				}
			}
			if err != nil {
				return fmt.Errorf("grant entry %d: %w", e.Id, err)
			}
		}
		if i == run {
			return fmt.Errorf("grant %d has no journal", g.Id)
		}
		// Cancelled entries must account for exactly the principal the grant no longer holds or paid.
		if !paid.Equal(g.Paid) || !freed.Equal(g.Amount.Amount.Sub(g.Paid).Sub(g.Remaining)) {
			return fmt.Errorf("grant %d differs from its journal: paid %s, cancelled %s", g.Id, paid, freed)
		}
		if payee != g.Payee {
			return fmt.Errorf("grant %d pays %s; its journal leads to %s", g.Id, g.Payee, payee)
		}
		if cancelled != g.Cancelled() || suspended != g.Suspended() {
			return fmt.Errorf("grant %d differs from its journal: cancelled %t, suspended %t", g.Id, cancelled, suspended)
		}
		if suspended && suspendedTime != g.SuspendedTime {
			return fmt.Errorf("grant %d was suspended at %d; its journal suspends it at %d", g.Id, g.SuspendedTime, suspendedTime)
		}
	}
	return nil
}

// validateBeneficiaryJournal checks the beneficiary journal arrives record-major with IDs running from
// one without a gap, and that each record's entries end at its controller.
func validateBeneficiaryJournal(list []Beneficiary, journal []BeneficiaryEntry, next uint64) error {
	entries := len(journal)
	if next != uint64(entries)+1 {
		return fmt.Errorf("next beneficiary entry ID %d does not follow %d entries", next, entries)
	}
	controller := make(map[string]string, len(list))
	for _, p := range list {
		controller[p.Address] = p.Address
	}
	seen, stamps := make([]bool, entries), make([][2]uint64, entries)
	var previous sdk.AccAddress
	for i, e := range journal {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("beneficiary entry %d: %w", e.Id, err)
		}
		address, err := chain.ParseCanonicalAccountAddress("beneficiary entry", e.Beneficiary)
		if err != nil {
			return err
		}
		// The store orders a pair's address by length first, the prefix BytesKey writes.
		if i > 0 && cmp.Or(cmp.Compare(len(address), len(previous)), bytes.Compare(address, previous), cmp.Compare(e.Id, journal[i-1].Id)) <= 0 {
			return errors.New("genesis beneficiary journal must be sorted by unique beneficiary and entry ID")
		}
		previous = address
		if e.Id > uint64(entries) || seen[e.Id-1] {
			return fmt.Errorf("beneficiary entry %d: entry IDs must run from one without a gap", e.Id)
		}
		seen[e.Id-1], stamps[e.Id-1] = true, [2]uint64{e.Height, e.Time}
		current, ok := controller[e.Beneficiary]
		if !ok {
			return fmt.Errorf("beneficiary entry %d references unknown beneficiary %s", e.Id, e.Beneficiary)
		}
		if e.Controller == current {
			return fmt.Errorf("beneficiary entry %d changes nothing: %s already controls %s", e.Id, current, e.Beneficiary)
		}
		controller[e.Beneficiary] = e.Controller
	}
	if err := blockOrder("beneficiary", stamps); err != nil {
		return err
	}
	for _, p := range list {
		if controller[p.Address] != p.Controller {
			return fmt.Errorf("beneficiary %s is controlled by %s; its journal leads to %s", p.Address, p.Controller, controller[p.Address])
		}
	}
	return nil
}

// blockOrder refuses a journal whose heights or times, read by entry ID, fall: IDs are assigned in
// block order.
func blockOrder(journal string, stamps [][2]uint64) error {
	for id := 1; id < len(stamps); id++ {
		if stamps[id][0] < stamps[id-1][0] || stamps[id][1] < stamps[id-1][1] {
			return fmt.Errorf("%s entry %d is recorded before entry %d", journal, id+1, id)
		}
	}
	return nil
}

// amountOf reads a running total, zero before its first addition.
func amountOf(totals map[string]math.Int, key string) math.Int {
	if v, ok := totals[key]; ok {
		return v
	}
	return math.ZeroInt()
}
