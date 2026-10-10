package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// getGrant reads a grant, reporting absence as the module's not-found.
func (k *Keeper) getGrant(ctx context.Context, id uint64) (types.Grant, error) {
	g, err := k.Grants.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Grant{}, errorsmod.Wrapf(types.ErrGrantNotFound, "%d", id)
		}
		return types.Grant{}, fmt.Errorf("getting grant %d: %w", id, err)
	}
	return g, nil
}

// getBeneficiary reads a beneficiary record, reporting absence as the module's not-found.
func (k *Keeper) getBeneficiary(ctx context.Context, address sdk.AccAddress) (types.Beneficiary, error) {
	p, err := k.Beneficiaries.Get(ctx, address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Beneficiary{}, errorsmod.Wrap(types.ErrBeneficiaryNotFound, address.String())
		}
		return types.Beneficiary{}, fmt.Errorf("getting beneficiary %s: %w", address, err)
	}
	return p, nil
}

// ownershipSchedule is ownership's fixed shape from params. The clock never starts before its award,
// since the cliff is the point and the amount credits the past (D116).
func (k *Keeper) ownershipSchedule(ctx context.Context, startTime uint64) (types.Schedule, error) {
	now := chain.BlockSeconds(ctx)
	if err := types.ValidateStartTime(startTime, now); err != nil {
		return types.Schedule{}, err
	}
	if startTime < now {
		return types.Schedule{}, errors.New("an ownership clock starts no earlier than its award")
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Schedule{}, err
	}
	return params.OwnershipSchedule, nil
}

// ownershipRemaining is what the cap still admits a record: the cap less its committed ownership,
// zero once a lowered cap falls below it.
func (k *Keeper) ownershipRemaining(ctx context.Context, committed math.Int) (math.Int, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.Int{}, err
	}
	return math.MaxInt(params.OwnershipCap.Sub(committed), math.ZeroInt()), nil
}

// create reserves a grant and queues it for its first boundary; members receive their first payment
// in the same call. The caller fills the kind, amount, schedule, start time, reference, grantor,
// term, and award value; create stamps the rest. Every check runs before a stablecoin award buys
// what custody lacks within maxOffer, so a refused award never trades, and the conversion is
// journaled straight after the creation it funds.
func (k *Keeper) create(ctx context.Context, beneficiary sdk.AccAddress, g types.Grant, maxOffer math.Int) (uint64, error) {
	// The beneficiary is the first payee. A disbursement account is not Bank-blocked, since it takes
	// deposits, but paying one only returns custody to the module.
	if _, isPool := k.poolAddresses[string(beneficiary)]; isPool || k.bankKeeper.BlockedAddr(beneficiary) {
		return 0, errors.New("beneficiary cannot receive funds")
	}
	var p types.Beneficiary
	if g.Kind != types.GrantKind_GRANT_KIND_MEMBER {
		var err error
		p, err = k.Beneficiaries.Get(ctx, beneficiary)
		if errors.Is(err, collections.ErrNotFound) {
			p = types.Beneficiary{
				Address:            beneficiary.String(),
				Controller:         beneficiary.String(),
				OwnershipCommitted: math.ZeroInt(),
				Seat:               math.ZeroInt(),
			}
		} else if err != nil {
			return 0, err
		}
	}
	at := chain.BlockSeconds(ctx)
	switch g.Kind {
	case types.GrantKind_GRANT_KIND_COMPENSATION:
		if err := k.checkCompensationCap(ctx, beneficiary, g.AwardValue, g.Schedule, g.StartTime, at); err != nil {
			return 0, err
		}
	case types.GrantKind_GRANT_KIND_OWNERSHIP:
		// Committed ownership stays within the cap, so every ownership grant pays in full (D121).
		remaining, err := k.ownershipRemaining(ctx, p.OwnershipCommitted)
		if err != nil {
			return 0, err
		}
		if g.Amount.Amount.GT(remaining) {
			return 0, fmt.Errorf("award of %s exceeds the record's remaining ownership %s", g.Amount.Amount, remaining)
		}
	}
	id, err := nextID(ctx, k.NextGrantID)
	if err != nil {
		return 0, err
	}
	g.Id = id
	g.Beneficiary = beneficiary.String()
	g.Payee = beneficiary.String()
	g.Paid = math.ZeroInt()
	g.Remaining = g.Amount.Amount
	g.CreatedHeight = uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	g.CreatedTime = at
	if g.Kind != types.GrantKind_GRANT_KIND_MEMBER {
		// A member's first payment below queues it; a contributor waits for its first boundary or one
		// period after the award, whichever is later, so next is never zero here.
		_, next, err := g.Accrued(at)
		if err != nil {
			return 0, err
		}
		if err := k.setDueTime(ctx, &g, next); err != nil {
			return 0, err
		}
		if err := g.Validate(); err != nil {
			return 0, err
		}
	}
	// Only a stablecoin award converts, and only compensation can be one.
	converted, err := k.fund(ctx, g.Amount, maxOffer)
	if err != nil {
		return 0, err
	}
	if err := k.draw(ctx, types.Account(g.Kind), g.Amount); err != nil {
		return 0, err
	}
	if g.Kind != types.GrantKind_GRANT_KIND_MEMBER {
		if g.Kind == types.GrantKind_GRANT_KIND_OWNERSHIP {
			p.OwnershipCommitted = p.OwnershipCommitted.Add(g.Amount.Amount) // Within the cap checked above.
		}
		if err := k.Beneficiaries.Set(ctx, beneficiary, p); err != nil {
			return 0, err
		}
	}
	if err := k.Grants.Set(ctx, id, g); err != nil {
		return 0, err
	}
	if err := k.index(ctx, beneficiary, g); err != nil {
		return 0, err
	}
	if err := k.recordGrant(ctx, types.GrantEntry{
		GrantId: id,
		Action:  types.GrantAction_GRANT_ACTION_CREATED,
		Actor:   g.CreatedBy,
		Amount:  g.Amount,
	}); err != nil {
		return 0, err
	}
	if err := k.recordConversion(ctx, id, g.CreatedBy, converted); err != nil {
		return 0, err
	}
	if g.Kind == types.GrantKind_GRANT_KIND_MEMBER {
		// A member's first period pays at registration.
		q, err := k.payable(ctx, g)
		if err != nil {
			return 0, err
		}
		if err := k.settle(ctx, g.CreatedBy, g, q); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// cancel freezes earned entitlement and queues the retained debt for the block to pay; it sends
// nothing itself.
func (k *Keeper) cancel(ctx context.Context, actor string, g types.Grant) error {
	if g.Cancelled() || g.Remaining.IsZero() {
		return nil
	}
	now := chain.BlockSeconds(ctx)
	cutoff := now
	if g.Suspended() {
		cutoff = g.SuspendedTime
	}
	earned, _, err := g.Accrued(cutoff)
	if err != nil {
		return err
	}
	if earned.LT(g.Paid) {
		return errors.New("cancellation would claw back earned payments")
	}
	held := earned.Sub(g.Paid)
	returned := g.Remaining.Sub(held)
	g.CutoffTime = cutoff
	g.SuspendedTime = 0
	g.Remaining = held
	due := uint64(0)
	if held.IsPositive() {
		due = now
	}
	if err := k.setDueTime(ctx, &g, due); err != nil {
		return err
	}
	account := types.Account(g.Kind)
	b, err := k.getBook(ctx, account)
	if err != nil {
		return err
	}
	coin := sdk.NewCoin(g.Amount.Denom, returned)
	if err := b.Release(coin); err != nil {
		return err
	}
	if err := k.Books.Set(ctx, account, b); err != nil {
		return err
	}
	// A member's unearned NOAH reopens in the tranche; contributor principal returns to the pool or
	// custody, and unearned ownership to the record's room under the cap.
	switch g.Kind {
	case types.GrantKind_GRANT_KIND_MEMBER:
		tranche, err := k.MemberTranche.Get(ctx)
		if err != nil {
			return err
		}
		if tranche, err = tranche.SafeAdd(returned); err != nil {
			return err
		}
		if err := k.MemberTranche.Set(ctx, tranche); err != nil {
			return err
		}
	case types.GrantKind_GRANT_KIND_OWNERSHIP:
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", g.Beneficiary)
		if err != nil {
			return err
		}
		p, err := k.getBeneficiary(ctx, address)
		if err != nil {
			return err
		}
		p.OwnershipCommitted = p.OwnershipCommitted.Sub(returned)
		if err := k.Beneficiaries.Set(ctx, address, p); err != nil {
			return err
		}
	}
	return k.writeGrant(ctx, g, types.GrantEntry{
		GrantId: g.Id,
		Action:  types.GrantAction_GRANT_ACTION_CANCELLED,
		Actor:   actor,
		Amount:  coin,
	})
}

// writeGrant validates and stores a changed grant and journals the change; the caller fills the
// entry as for recordGrant.
func (k *Keeper) writeGrant(ctx context.Context, g types.Grant, entry types.GrantEntry) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if err := k.Grants.Set(ctx, g.Id, g); err != nil {
		return err
	}
	return k.recordGrant(ctx, entry)
}

// setDueTime sets a grant's due time and moves its queue entry with it; zero removes it. It is the
// index's only writer outside import and leaves the grant for the caller to store.
func (k *Keeper) setDueTime(ctx context.Context, g *types.Grant, time uint64) error {
	if g.DueTime != 0 {
		if err := k.Due.Remove(ctx, collections.Join(g.DueTime, g.Id)); err != nil {
			return err
		}
	}
	g.DueTime = time
	if time == 0 {
		return nil
	}
	return k.Due.Set(ctx, collections.Join(time, g.Id))
}

// index enters a stored grant in the beneficiary, member, and term indexes; setDueTime owns Due.
func (k *Keeper) index(ctx context.Context, beneficiary sdk.AccAddress, g types.Grant) error {
	if err := k.GrantsByBeneficiary.Set(ctx, collections.Join(beneficiary, g.Id), true); err != nil {
		return err
	}
	if err := k.GrantsByKind.Set(ctx, collections.Join(int32(g.Kind), g.Id)); err != nil {
		return err
	}
	if g.Kind == types.GrantKind_GRANT_KIND_MEMBER {
		if err := k.Members.Set(ctx, beneficiary, g.Id); err != nil {
			return err
		}
	}
	if g.MandateTerm == 0 {
		return nil
	}
	// Members sit under the registrar's term, awards under the committee's.
	terms := k.CommitteeTermGrants
	if g.Kind == types.GrantKind_GRANT_KIND_MEMBER {
		terms = k.RegistrarTermGrants
	}
	return terms.Set(ctx, collections.Join(g.MandateTerm, g.Id))
}
