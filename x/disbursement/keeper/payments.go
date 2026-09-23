package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/disbursement/types"
)

func (k *Keeper) validateCompensation(ctx context.Context, denom string, params types.Params) error {
	if !slices.Contains(params.CompensationDenoms, denom) {
		return errors.New("compensation denomination is not approved")
	}
	if denom == chain.NoahBaseDenom {
		return nil
	}
	asset, err := k.assets.Get(ctx, denom)
	if err != nil {
		return fmt.Errorf("compensation asset: %w", err)
	}
	if asset.Status != assettypes.AssetStatus_ASSET_STATUS_ACTIVE {
		return errors.New("new compensation requires an active asset")
	}
	return nil
}

// create reserves a complete commitment; members receive their first payment in the same call.
func (k *Keeper) create(ctx context.Context, actor string, kind types.GrantKind, beneficiary sdk.AccAddress, amount sdk.Coin, schedule []types.Period, reference string) (uint64, error) {
	if k.bank.BlockedAddr(beneficiary) {
		return 0, errors.New("beneficiary cannot receive funds")
	}
	if err := amount.Validate(); err != nil {
		return 0, err
	}
	if _, err := types.Split(amount.Amount, schedule); err != nil {
		return 0, err
	}
	available, err := k.unallocated(ctx, amount.Denom)
	if err != nil {
		return 0, err
	}
	if available.LT(amount.Amount) {
		return 0, errors.New("insufficient unallocated disbursement funds")
	}
	at, err := blockTime(ctx)
	if err != nil {
		return 0, err
	}
	id, err := nextID(ctx, k.NextGrantID)
	if err != nil {
		return 0, err
	}
	g := types.Grant{
		Id: id, Kind: kind, Beneficiary: beneficiary.String(), Payee: beneficiary.String(), Amount: amount,
		Schedule: append([]types.Period(nil), schedule...), StartTime: at, Paid: math.ZeroInt(), Remaining: amount.Amount,
		CancelledAmount: math.ZeroInt(), Reference: reference, CreatedBy: actor, CreatedHeight: uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()),
	}
	if kind != types.GrantKind_GRANT_KIND_MEMBER {
		if err := g.Validate(); err != nil {
			return 0, err
		}
		_, err := k.Beneficiaries.Get(ctx, beneficiary)
		if errors.Is(err, collections.ErrNotFound) {
			err = k.Beneficiaries.Set(ctx, beneficiary, types.Beneficiary{Address: beneficiary.String(), Controller: beneficiary.String(), OwnershipPaid: math.ZeroInt()})
		}
		if err != nil {
			return 0, err
		}
	}
	t, err := k.totals(ctx, amount.Denom)
	if err != nil {
		return 0, err
	}
	t.Reserved, err = t.Reserved.SafeAdd(amount.Amount)
	if err != nil {
		return 0, err
	}
	if err := types.ValidateAmount(t.Reserved, false); err != nil {
		return 0, err
	}
	if err := k.Totals.Set(ctx, amount.Denom, t); err != nil {
		return 0, err
	}
	if kind == types.GrantKind_GRANT_KIND_OWNERSHIP {
		f, err := k.founder(ctx, beneficiary)
		if err != nil {
			return 0, err
		}
		if f != nil {
			remaining, err := k.FounderRemaining.Get(ctx)
			if err != nil {
				return 0, err
			}
			remaining, err = remaining.SafeAdd(amount.Amount)
			if err != nil {
				return 0, err
			}
			if err := k.FounderRemaining.Set(ctx, remaining); err != nil {
				return 0, err
			}
		}
	}
	if err := k.Grants.Set(ctx, id, g); err != nil {
		return 0, err
	}
	if err := k.GrantsByBeneficiary.Set(ctx, collections.Join(beneficiary, id), true); err != nil {
		return 0, err
	}
	if err := k.record(ctx, id, "created", actor, amount, g); err != nil {
		return 0, err
	}
	if kind == types.GrantKind_GRANT_KIND_MEMBER {
		if err := k.Members.Set(ctx, beneficiary, id); err != nil {
			return 0, err
		}
		if _, err := k.pay(ctx, actor, g); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// releasable uses aggregate founder totals, never a scan over historical grants.
func (k *Keeper) releasable(ctx context.Context, g types.Grant) (*types.QueryReleasableResponse, error) {
	at, err := blockTime(ctx)
	if err != nil {
		return nil, err
	}
	earned, next, err := types.Accrued(g, at)
	if err != nil {
		return nil, err
	}
	if earned.LT(g.Paid) {
		return nil, errors.New("grant paid beyond accrued entitlement")
	}
	due := earned.Sub(g.Paid)
	r := &types.QueryReleasableResponse{
		Amount: sdk.NewCoin(g.Amount.Denom, due), Accrued: due, NextAt: next,
		Bonded: math.ZeroInt(), Own: math.ZeroInt(), OwnershipLimit: math.ZeroInt(), BlocAllowance: math.ZeroInt(),
	}
	r.Suspended, err = k.suspended(ctx, g)
	if err != nil {
		return nil, err
	}
	if r.Suspended {
		r.Amount.Amount = math.ZeroInt()
		return r, nil
	}
	if g.Kind != types.GrantKind_GRANT_KIND_OWNERSHIP {
		return r, nil
	}
	address, err := chain.ParseCanonicalAccountAddress("beneficiary", g.Beneficiary)
	if err != nil {
		return nil, err
	}
	p, err := k.Beneficiaries.Get(ctx, address)
	if err != nil {
		return nil, err
	}
	f, err := k.founder(ctx, address)
	if err != nil {
		return nil, err
	}
	seat := math.ZeroInt()
	if f != nil {
		seat = f.SeatAmount
	}
	r.Own, err = p.OwnershipPaid.SafeAdd(seat)
	if err != nil {
		return nil, err
	}
	r.Bonded, err = k.staking.TotalValidatorPower(ctx)
	if err != nil {
		return nil, err
	}
	policy, err := k.OwnershipPolicy.Get(ctx)
	if err != nil {
		return nil, err
	}
	limit, err := types.MulDiv(types.PositiveDifference(r.Bonded, r.Own), math.NewIntFromUint64(policy.Numerator), math.NewIntFromUint64(policy.Denominator))
	if err != nil {
		return nil, err
	}
	ceiling, err := policy.Ceiling.SafeAdd(seat)
	if err != nil {
		return nil, err
	}
	r.OwnershipLimit = math.MinInt(limit, ceiling)
	allowance := types.PositiveDifference(r.OwnershipLimit, r.Own)
	if f != nil {
		founding, err := k.FoundingStake.Get(ctx)
		if err != nil {
			return nil, err
		}
		paid, err := k.FounderPaid.Get(ctx)
		if err != nil {
			return nil, err
		}
		remaining, err := k.FounderRemaining.Get(ctx)
		if err != nil {
			return nil, err
		}
		room := types.PositiveDifference(types.PositiveDifference(r.Bonded.QuoRaw(3), founding), paid)
		if remaining.IsPositive() {
			r.BlocAllowance, err = types.MulDiv(room, g.Remaining, remaining)
			if err != nil {
				return nil, err
			}
		}
		allowance = math.MinInt(allowance, r.BlocAllowance)
	}
	r.Amount.Amount = math.MinInt(due, allowance)
	return r, nil
}

func (k *Keeper) pay(ctx context.Context, actor string, g types.Grant) (bool, error) {
	r, err := k.releasable(ctx, g)
	if err != nil {
		return false, err
	}
	amount := r.Amount.Amount
	if amount.IsZero() {
		return false, nil
	}
	payee, err := chain.ParseCanonicalAccountAddress("payee", g.Payee)
	if err != nil {
		return false, err
	}
	if err := k.bank.SendCoinsFromModuleToAccount(ctx, types.ModuleName, payee, sdk.NewCoins(r.Amount)); err != nil {
		return false, err
	}
	g.Paid = g.Paid.Add(amount) // Payment cannot exceed the validated remaining principal.
	g.Remaining = g.Remaining.Sub(amount)
	t, err := k.totals(ctx, g.Amount.Denom)
	if err != nil {
		return false, err
	}
	t.Reserved = t.Reserved.Sub(amount)
	switch g.Kind {
	case types.GrantKind_GRANT_KIND_MEMBER:
		t.MembersPaid, err = t.MembersPaid.SafeAdd(amount)
	case types.GrantKind_GRANT_KIND_COMPENSATION:
		t.CompensationPaid, err = t.CompensationPaid.SafeAdd(amount)
	case types.GrantKind_GRANT_KIND_OWNERSHIP:
		t.OwnershipPaid, err = t.OwnershipPaid.SafeAdd(amount)
		if err != nil {
			return false, err
		}
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", g.Beneficiary)
		if err != nil {
			return false, err
		}
		p, err := k.Beneficiaries.Get(ctx, address)
		if err != nil {
			return false, err
		}
		p.OwnershipPaid, err = p.OwnershipPaid.SafeAdd(amount)
		if err != nil {
			return false, err
		}
		if err := k.Beneficiaries.Set(ctx, address, p); err != nil {
			return false, err
		}
		f, err := k.founder(ctx, address)
		if err != nil {
			return false, err
		}
		if f != nil {
			paid, err := k.FounderPaid.Get(ctx)
			if err != nil {
				return false, err
			}
			paid, err = paid.SafeAdd(amount)
			if err != nil {
				return false, err
			}
			remaining, err := k.FounderRemaining.Get(ctx)
			if err != nil {
				return false, err
			}
			if err := k.FounderPaid.Set(ctx, paid); err != nil {
				return false, err
			}
			if err := k.FounderRemaining.Set(ctx, remaining.Sub(amount)); err != nil {
				return false, err
			}
		}
	}
	if err != nil {
		return false, err
	}
	if err := g.Validate(); err != nil {
		return false, err
	}
	if err := k.Grants.Set(ctx, g.Id, g); err != nil {
		return false, err
	}
	if err := k.Totals.Set(ctx, g.Amount.Denom, t); err != nil {
		return false, err
	}
	return true, k.record(ctx, g.Id, "paid", actor, r.Amount, map[string]string{"payee": g.Payee})
}

// cancel freezes earned entitlement without attempting a recipient payment.
func (k *Keeper) cancel(ctx context.Context, actor string, g types.Grant) error {
	if g.Cancelled || g.Remaining.IsZero() {
		return nil
	}
	at, err := blockTime(ctx)
	if err != nil {
		return err
	}
	suspended, err := k.suspended(ctx, g)
	if err != nil {
		return err
	}
	if suspended {
		at = g.Suspension.At
	}
	earned, _, err := types.Accrued(g, at)
	if err != nil {
		return err
	}
	if earned.LT(g.Paid) {
		return errors.New("cancellation would claw back earned payments")
	}
	held := earned.Sub(g.Paid)
	returned := g.Remaining.Sub(held)
	g.Cancelled, g.CutoffTime, g.Suspension = true, at, nil
	g.Remaining, g.CancelledAmount = held, returned
	if err := g.Validate(); err != nil {
		return err
	}
	t, err := k.totals(ctx, g.Amount.Denom)
	if err != nil {
		return err
	}
	t.Reserved = t.Reserved.Sub(returned)
	if g.Kind == types.GrantKind_GRANT_KIND_OWNERSHIP {
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", g.Beneficiary)
		if err != nil {
			return err
		}
		f, err := k.founder(ctx, address)
		if err != nil {
			return err
		}
		if f != nil {
			remaining, err := k.FounderRemaining.Get(ctx)
			if err != nil {
				return err
			}
			if err := k.FounderRemaining.Set(ctx, remaining.Sub(returned)); err != nil {
				return err
			}
		}
	}
	coin := sdk.NewCoin(g.Amount.Denom, returned)
	if g.Kind != types.GrantKind_GRANT_KIND_MEMBER && returned.IsPositive() {
		if err := k.distribution.FundCommunityPool(ctx, sdk.NewCoins(coin), k.address); err != nil {
			return err
		}
	}
	if err := k.Grants.Set(ctx, g.Id, g); err != nil {
		return err
	}
	if err := k.Totals.Set(ctx, g.Amount.Denom, t); err != nil {
		return err
	}
	return k.record(ctx, g.Id, "cancelled", actor, coin, map[string]any{"cutoff_time": at, "retained": held.String()})
}
