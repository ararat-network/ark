package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// RebaseReference re-expresses every compensation figure in the new reference unit: the cap, each
// compensation award's value and charge, the live term's usage, and its allowance. It implements the
// oracle module's ReferenceRebaser, and MsgSetReferenceDenom is its only caller. Every figure is a
// quantity, rounded up as the award-time valuation is, so no award counts for less after a
// move. The usage is re-derived from the rebased charges rather than converted, so the import
// equality holds exactly; the allowance is raised to it where the per-award ceilings outran its own,
// by under one base unit an award, since a bound a few units loose beats wedging the re-point.
func (k *Keeper) RebaseReference(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	if from == to {
		return nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting disbursement params: %w", err)
	}
	mandate, err := k.GrantsMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting the grants mandate: %w", err)
	}
	oldUsed, err := k.CompensationUsed.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting the term's compensation usage: %w", err)
	}

	cap, err := rebaseAmount(rates, params.CompensationCap, from, to)
	if err != nil {
		return fmt.Errorf("rebasing the compensation cap: %w", err)
	}

	// The kind index keeps the walk to compensation awards, so its cost scales with awards rather
	// than registrations. They are collected under the walk and written after it, the live term's
	// usage summed on the way: a governance award carries no term and charges none.
	used := math.ZeroInt()
	var awards []types.Grant
	compensation := collections.NewPrefixedPairRange[int32, uint64](int32(types.GrantKind_GRANT_KIND_COMPENSATION))
	if err := k.GrantsByKind.Walk(ctx, compensation, func(key collections.Pair[int32, uint64]) (bool, error) {
		g, err := k.Grants.Get(ctx, key.K2())
		if err != nil {
			return true, fmt.Errorf("getting compensation grant %d: %w", key.K2(), err)
		}
		value, err := rebaseAmount(rates, g.AwardValue, from, to)
		if err != nil {
			return true, fmt.Errorf("rebasing the value of grant %d: %w", g.Id, err)
		}
		g.AwardValue = value
		charge, err := rebaseAmount(rates, g.Charge, from, to)
		if err != nil {
			return true, fmt.Errorf("rebasing the charge of grant %d: %w", g.Id, err)
		}
		g.Charge = charge
		if g.MandateTerm != 0 && g.MandateTerm == mandate.Term {
			if used, err = used.SafeAdd(charge); err != nil {
				return true, fmt.Errorf("rebasing the term's usage: %w", err)
			}
		}
		awards = append(awards, g)
		return false, nil
	}); err != nil {
		return err
	}
	if err := chain.ValidateAmount(used, false); err != nil {
		return fmt.Errorf("rebasing the term's usage: %w", err)
	}

	allowance, err := rebaseAmount(rates, mandate.CompensationAllowance, from, to)
	if err != nil {
		return fmt.Errorf("rebasing the compensation allowance: %w", err)
	}
	if allowance.LT(used) {
		allowance = used
	}

	oldCap, oldAllowance := params.CompensationCap, mandate.CompensationAllowance
	params.CompensationCap = cap
	if err := k.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("setting rebased disbursement params: %w", err)
	}
	for _, g := range awards {
		if err := k.Grants.Set(ctx, g.Id, g); err != nil {
			return fmt.Errorf("setting rebased grant %d: %w", g.Id, err)
		}
	}
	if err := k.CompensationUsed.Set(ctx, used); err != nil {
		return fmt.Errorf("setting the term's rebased compensation usage: %w", err)
	}
	mandate.CompensationAllowance = allowance
	if err := k.GrantsMandate.Set(ctx, mandate); err != nil {
		return fmt.Errorf("setting the rebased grants mandate: %w", err)
	}

	return sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventCompensationRebased{
		OldCap:       sdk.NewCoin(from, oldCap),
		NewCap:       sdk.NewCoin(to, cap),
		OldAllowance: sdk.NewCoin(from, oldAllowance),
		NewAllowance: sdk.NewCoin(to, allowance),
		OldUsed:      sdk.NewCoin(from, oldUsed),
		NewUsed:      sdk.NewCoin(to, used),
		Grants:       uint64(len(awards)),
	})
}

// rebaseAmount re-expresses a reference-unit amount in the new unit, rounded up. The result is held
// to the module's amount bound here, where a proposal can be refused, rather than at the next
// export, which would refuse the figure instead.
func rebaseAmount(rates oracletypes.RateSet, amount math.Int, from, to string) (math.Int, error) {
	value, err := rates.ConvertCeil(sdk.Coin{Denom: from, Amount: amount}, to)
	if err != nil {
		return math.Int{}, err
	}
	// A positive figure must stay positive: one rounding to zero would leave a record its own
	// validation refuses, so the re-point is refused instead.
	if err := chain.ValidateAmount(value, amount.IsPositive()); err != nil {
		return math.Int{}, err
	}
	return value, nil
}
