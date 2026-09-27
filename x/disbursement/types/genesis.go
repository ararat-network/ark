package types

import (
	"encoding/json"
	"errors"
	"fmt"
	stdmath "math"
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// DefaultGenesisState holds no commitments, custody or founder seats.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{Params: DefaultParams(), OwnershipPolicy: DefaultOwnershipPolicy(), GrantsMandate: DefaultGrantsMandate(), NextGrantId: 1, NextJournalId: 1, MemberPool: EmptyPool(), ContributorPool: EmptyPool()}
}

// EmptyTotals supplies explicit zero-valued counters for a denomination.
func EmptyTotals(denom string) DenomTotals {
	return DenomTotals{Denom: denom, Reserved: math.ZeroInt(), MembersPaid: math.ZeroInt(), OwnershipPaid: math.ZeroInt(), CompensationPaid: math.ZeroInt()}
}

// EmptyPool is an unfunded pool with nothing open.
func EmptyPool() PoolBalance {
	return PoolBalance{Unallocated: math.ZeroInt(), Open: math.ZeroInt()}
}

// Validate bounds a pool and keeps its open tranche inside it.
func (p PoolBalance) Validate() error {
	for _, v := range []math.Int{p.Unallocated, p.Open} {
		if err := ValidateAmount(v, false); err != nil {
			return err
		}
	}
	if p.Open.GT(p.Unallocated) {
		return errors.New("open tranche exceeds its pool")
	}
	return nil
}

// ValidateMaxSpread admits a cap strictly between zero and one; a spread of one pays nothing.
func ValidateMaxSpread(spread math.LegacyDec) error {
	if spread.IsNil() || !spread.IsPositive() || !spread.LT(math.LegacyOneDec()) {
		return errors.New("max spread must be above zero and below one")
	}
	return nil
}

// Validate checks a live order; an exhausted order is removed rather than kept at zero.
func (o ConversionOrder) Validate() error {
	if err := sdk.ValidateDenom(o.Denom); err != nil {
		return err
	}
	if o.Denom == chain.NoahBaseDenom {
		return errors.New("conversion orders sell NOAH for another denomination")
	}
	if err := ValidateAmount(o.Remaining, true); err != nil {
		return err
	}
	return ValidateMaxSpread(o.MaxSpread)
}

// Validate checks permanent grant terms and conservation independently of Bank.
func (g Grant) Validate() error {
	if g.Id == 0 || g.Id == stdmath.MaxUint64 {
		return errors.New("invalid grant ID")
	}
	for _, address := range []string{g.Beneficiary, g.Payee, g.CreatedBy} {
		if _, err := chain.ParseCanonicalAccountAddress("grant address", address); err != nil {
			return err
		}
	}
	if err := g.Amount.Validate(); err != nil {
		return err
	}
	if _, err := Split(g.Amount.Amount, g.Schedule); err != nil {
		return err
	}
	if g.Amount.Denom != chain.NoahBaseDenom {
		if err := chain.ValidatePricedDenom(g.Amount.Denom); err != nil {
			return err
		}
	}
	if g.Kind != GrantKind_GRANT_KIND_MEMBER && g.Kind != GrantKind_GRANT_KIND_OWNERSHIP && g.Kind != GrantKind_GRANT_KIND_COMPENSATION {
		return errors.New("unknown grant kind")
	}
	if g.Kind != GrantKind_GRANT_KIND_COMPENSATION && g.Amount.Denom != chain.NoahBaseDenom {
		return errors.New("distribution grants require NOAH")
	}
	if len(g.Reference) > MaxReferenceBytes || (g.Kind != GrantKind_GRANT_KIND_MEMBER && strings.TrimSpace(g.Reference) == "") {
		return errors.New("contributor reference must be nonempty and bounded")
	}
	for _, v := range []math.Int{g.Paid, g.Remaining, g.CancelledAmount} {
		if err := ValidateAmount(v, false); err != nil {
			return err
		}
	}
	if !g.Paid.Add(g.Remaining).Add(g.CancelledAmount).Equal(g.Amount.Amount) {
		return errors.New("grant principal does not balance")
	}
	if g.StartTime > stdmath.MaxInt64 || g.CreatedHeight == 0 || g.CreatedHeight > stdmath.MaxInt64 {
		return errors.New("invalid grant creation time or height")
	}
	if g.Cancelled {
		if g.CutoffTime < g.StartTime || g.CutoffTime > stdmath.MaxInt64 || g.Suspension != nil {
			return errors.New("invalid cancellation cutoff")
		}
	} else if g.CutoffTime != 0 || !g.CancelledAmount.IsZero() {
		return errors.New("active grant carries cancellation accounting")
	}
	earned, _, err := Accrued(g, stdmath.MaxInt64)
	if err != nil {
		return err
	}
	if g.Paid.GT(earned) || (g.Cancelled && !g.Remaining.Equal(earned.Sub(g.Paid))) {
		return errors.New("grant exceeds its earned entitlement")
	}
	if g.Kind == GrantKind_GRANT_KIND_MEMBER {
		shares, _ := Split(g.Amount.Amount, g.Schedule)
		if g.Paid.LT(shares[0]) {
			return errors.New("member first period was not paid")
		}
	}
	if g.Suspension != nil {
		if g.Kind != GrantKind_GRANT_KIND_MEMBER || g.Suspension.At < g.StartTime || g.Suspension.At > stdmath.MaxInt64 || g.Suspension.Term == 0 {
			return errors.New("invalid member suspension")
		}
	}
	if g.MandateTerm != 0 && g.Kind != GrantKind_GRANT_KIND_COMPENSATION {
		return errors.New("only compensation carries a committee term")
	}
	return nil
}

// Validate checks imported records, aggregate counters, identities and audit continuity.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.OwnershipPolicy.Validate(); err != nil {
		return err
	}
	if err := gs.GrantsMandate.Validate(); err != nil {
		return err
	}
	if gs.NextGrantId == 0 || gs.NextJournalId == 0 {
		return errors.New("next IDs must be positive")
	}
	for _, p := range []PoolBalance{gs.MemberPool, gs.ContributorPool} {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	orders := make(map[string]bool, len(gs.ConversionOrders))
	for _, o := range gs.ConversionOrders {
		if orders[o.Denom] {
			return errors.New("duplicate conversion order")
		}
		if err := o.Validate(); err != nil {
			return err
		}
		orders[o.Denom] = true
	}
	people := make(map[string]Beneficiary, len(gs.Beneficiaries))
	foundingStake := math.ZeroInt()
	for _, p := range gs.Beneficiaries {
		if _, ok := people[p.Address]; ok {
			return errors.New("duplicate beneficiary")
		}
		for _, a := range []string{p.Address, p.Controller} {
			if _, err := chain.ParseCanonicalAccountAddress("beneficiary", a); err != nil {
				return err
			}
		}
		for _, v := range []math.Int{p.OwnershipPaid, p.Seat} {
			if err := ValidateAmount(v, false); err != nil {
				return err
			}
		}
		if !p.Seat.IsZero() && !p.Seat.Equal(chain.NativeBaseAmount(chain.SeatGrantNoah)) {
			return errors.New("founding seat must be zero or the genesis seat principal")
		}
		var err error
		foundingStake, err = foundingStake.SafeAdd(p.Seat)
		if err != nil {
			return err
		}
		people[p.Address] = p
	}
	if err := ValidateAmount(foundingStake, false); err != nil {
		return err
	}
	voided := make(map[uint64]bool, len(gs.VoidedTerms))
	for i, term := range gs.VoidedTerms {
		if term == 0 || term >= gs.GrantsMandate.Term || (i > 0 && term <= gs.VoidedTerms[i-1]) {
			return errors.New("voided terms must be increasing committee terms below the current one")
		}
		voided[term] = true
	}
	grants := make(map[uint64]Grant, len(gs.Grants))
	members := make(map[string]bool)
	totals := make(map[string]DenomTotals)
	ownership := make(map[string]math.Int)
	termGrants := make(map[uint64]int)
	live := gs.GrantsMandate
	liveUsed := sdk.NewCoins()
	for _, g := range gs.Grants {
		if err := g.Validate(); err != nil {
			return fmt.Errorf("grant %d: %w", g.Id, err)
		}
		if term := g.MandateTerm; term != 0 {
			if term > live.Term {
				return fmt.Errorf("grant %d has a future committee term", g.Id)
			}
			if p, ok := people[g.Beneficiary]; ok && p.Seat.IsPositive() {
				return fmt.Errorf("grant %d: committee compensation to a seat holder", g.Id)
			}
			if termGrants[term]++; termGrants[term] > MaxTermGrants {
				return fmt.Errorf("committee term %d exceeds %d awards", term, MaxTermGrants)
			}
			if term == live.Term {
				if g.Schedule[0].Length < live.MinFirstPeriod {
					return fmt.Errorf("grant %d: first period is shorter than the mandate's minimum", g.Id)
				}
				liveUsed = liveUsed.Add(g.Amount)
			}
		}
		if g.Id >= gs.NextGrantId {
			return errors.New("grant ID is not below next ID")
		}
		if _, ok := grants[g.Id]; ok {
			return errors.New("duplicate grant ID")
		}
		if g.Kind == GrantKind_GRANT_KIND_MEMBER {
			if members[g.Beneficiary] {
				return errors.New("member registered twice")
			}
			members[g.Beneficiary] = true
		} else if _, ok := people[g.Beneficiary]; !ok {
			return errors.New("grant has no beneficiary record")
		}
		if g.Suspension != nil {
			s := g.Suspension
			if s.Term > gs.GrantsMandate.Term {
				return errors.New("suspension has a future committee term")
			}
			if !voided[s.Term] {
				earned, _, err := Accrued(g, s.At)
				if err != nil || g.Paid.GT(earned) {
					return errors.New("suspended member paid past cutoff")
				}
			}
		}
		t, ok := totals[g.Amount.Denom]
		if !ok {
			t = EmptyTotals(g.Amount.Denom)
		}
		var err error
		t.Reserved, err = t.Reserved.SafeAdd(g.Remaining)
		if err != nil {
			return err
		}
		switch g.Kind {
		case GrantKind_GRANT_KIND_MEMBER:
			t.MembersPaid, err = t.MembersPaid.SafeAdd(g.Paid)
		case GrantKind_GRANT_KIND_OWNERSHIP:
			t.OwnershipPaid, err = t.OwnershipPaid.SafeAdd(g.Paid)
			p, ok := ownership[g.Beneficiary]
			if !ok {
				p = math.ZeroInt()
			}
			p, sumErr := p.SafeAdd(g.Paid)
			if sumErr != nil {
				return sumErr
			}
			ownership[g.Beneficiary] = p
		case GrantKind_GRANT_KIND_COMPENSATION:
			t.CompensationPaid, err = t.CompensationPaid.SafeAdd(g.Paid)
		}
		if err != nil {
			return err
		}
		if err := ValidateAmount(t.Reserved, false); err != nil {
			return err
		}
		totals[g.Amount.Denom] = t
		grants[g.Id] = g
	}
	if !liveUsed.IsAllLTE(live.CompensationAllowance) {
		return errors.New("the live term's committee awards exceed its compensation allowance")
	}
	for address, p := range people {
		paid, ok := ownership[address]
		if !ok {
			paid = math.ZeroInt()
		}
		if !paid.Equal(p.OwnershipPaid) {
			return errors.New("beneficiary ownership total differs from grants")
		}
	}
	seenDenoms := make(map[string]bool)
	for _, actual := range gs.Totals {
		if seenDenoms[actual.Denom] {
			return errors.New("duplicate denomination totals")
		}
		if err := sdk.ValidateDenom(actual.Denom); err != nil {
			return err
		}
		expected, ok := totals[actual.Denom]
		if !ok {
			expected = EmptyTotals(actual.Denom)
		}
		for i, v := range []math.Int{actual.Reserved, actual.MembersPaid, actual.OwnershipPaid, actual.CompensationPaid} {
			if v.IsNil() || v.IsNegative() {
				return errors.New("invalid denomination total")
			}
			want := []math.Int{expected.Reserved, expected.MembersPaid, expected.OwnershipPaid, expected.CompensationPaid}[i]
			if !v.Equal(want) {
				return errors.New("denomination total differs from grants")
			}
		}
		seenDenoms[actual.Denom] = true
	}
	for denom := range totals {
		if !seenDenoms[denom] {
			return errors.New("missing denomination totals")
		}
	}
	if len(gs.Issuance.Entries) > int(MaxIssuanceMembers) {
		return errors.New("issuance log exceeds bound")
	}
	var count, previous uint64
	for i, e := range gs.Issuance.Entries {
		if e.Count == 0 || e.Count > MaxIssuanceMembers || (i > 0 && e.At <= previous) || e.At > stdmath.MaxInt64 {
			return errors.New("invalid issuance log")
		}
		count += e.Count
		previous = e.At
	}
	if count > MaxIssuanceMembers {
		return errors.New("issuance exceeds hard bound")
	}
	created, journalPaid, journalCancelled := map[uint64]bool{}, map[uint64]math.Int{}, map[uint64]math.Int{}
	for i, e := range gs.Journal {
		if e.Id != uint64(i)+1 || e.Id >= gs.NextJournalId {
			return errors.New("journal is not contiguous")
		}
		if _, err := chain.ParseCanonicalAccountAddress("journal actor", e.Actor); err != nil {
			return err
		}
		if err := e.Amount.Validate(); err != nil {
			return err
		}
		if len(e.Details) > MaxJournalDetailsBytes || !json.Valid([]byte(e.Details)) {
			return errors.New("invalid journal details")
		}
		if e.Height == 0 || e.Height > stdmath.MaxInt64 || e.Time > stdmath.MaxInt64 {
			return errors.New("invalid journal time or height")
		}
		if e.GrantId == 0 {
			switch e.Action {
			case "params", "grants_mandate", "controller", "void_suspensions", "return_unallocated",
				"open_tranche", "authorise_conversion", "cancel_conversion", "convert", "cancel_term_grants":
			default:
				return errors.New("invalid administrative journal action")
			}
			continue
		}
		g, ok := grants[e.GrantId]
		if !ok {
			return errors.New("journal references unknown grant")
		}
		if e.Amount.Denom != g.Amount.Denom {
			return errors.New("journal denomination differs from grant")
		}
		switch e.Action {
		case "created":
			if created[g.Id] || !e.Amount.Equal(g.Amount) || e.Time != g.StartTime || e.Height != g.CreatedHeight || e.Actor != g.CreatedBy {
				return errors.New("invalid creation journal entry")
			}
			created[g.Id] = true
		case "paid", "cancelled":
			m := journalPaid
			if e.Action == "cancelled" {
				m = journalCancelled
			}
			amount, ok := m[g.Id]
			if !ok {
				amount = math.ZeroInt()
			}
			amount, err := amount.SafeAdd(e.Amount.Amount)
			if err != nil {
				return err
			}
			m[g.Id] = amount
		case "payee", "suspended", "reinstated":
			if !e.Amount.IsZero() {
				return errors.New("nonpayment journal moves principal")
			}
		default:
			return errors.New("unknown grant journal action")
		}
	}
	if gs.NextJournalId != uint64(len(gs.Journal))+1 {
		return errors.New("next journal ID does not follow history")
	}
	if gs.NextGrantId != uint64(len(gs.Grants))+1 {
		return errors.New("grant records are not complete")
	}
	for id, g := range grants {
		paid, ok := journalPaid[id]
		if !ok {
			paid = math.ZeroInt()
		}
		cancelled, ok := journalCancelled[id]
		if !ok {
			cancelled = math.ZeroInt()
		}
		if !created[id] || !paid.Equal(g.Paid) || !cancelled.Equal(g.CancelledAmount) {
			return errors.New("grant differs from its journal")
		}
	}
	return nil
}
