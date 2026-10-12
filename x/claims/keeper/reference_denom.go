package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// RebaseReference re-expresses every reference figure in the new unit: the committee claim limit,
// each committee claim's charge, and the live term's usage. It implements the oracle module's
// ReferenceRebaser, and MsgSetReferenceDenom is its only caller. Every figure rounds up, as the
// submission valuation does. The usage is re-derived from the rebased charges so the import
// equality holds exactly; the limit is raised to it where the per-claim ceilings outran its own,
// since a bound a few units loose beats wedging the re-point.
func (k *Keeper) RebaseReference(ctx context.Context, from string, to string, rates oracletypes.RateSet) error {
	if from == to {
		return nil
	}
	mandate, err := k.ClaimsMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Claims mandate: %w", err)
	}
	oldUsed, err := k.ClaimsAllowanceUsed.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Claims allowance usage: %w", err)
	}

	// Only committee claims carry a charge, and governance claims are few, so the walk reads every
	// claim. Rebased records are collected under the walk and written after it.
	used := math.ZeroInt()
	var charged []types.Claim
	if err := k.Claims.Walk(ctx, nil, func(_ uint64, claim types.Claim) (bool, error) {
		if claim.Origin != types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE {
			return false, nil
		}
		charge, err := rebaseAmount(rates, claim.Charge, from, to)
		if err != nil {
			return true, fmt.Errorf("rebasing the charge of claim %d: %w", claim.ClaimId, err)
		}
		claim.Charge = charge
		if claim.MandateTerm == mandate.Term {
			if used, err = used.SafeAdd(charge); err != nil {
				return true, fmt.Errorf("rebasing the term's usage: %w", err)
			}
		}
		charged = append(charged, claim)
		return false, nil
	}); err != nil {
		return err
	}
	if err := chain.ValidateAmount(used, false); err != nil {
		return fmt.Errorf("rebasing the term's usage: %w", err)
	}

	limit, err := rebaseAmount(rates, mandate.CommitteeClaimLimit, from, to)
	if err != nil {
		return fmt.Errorf("rebasing the committee claim limit: %w", err)
	}
	if limit.LT(used) {
		limit = used
	}

	oldLimit := mandate.CommitteeClaimLimit
	mandate.CommitteeClaimLimit = limit
	if err := k.ClaimsMandate.Set(ctx, mandate); err != nil {
		return fmt.Errorf("setting the rebased Claims mandate: %w", err)
	}
	for _, claim := range charged {
		if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
			return fmt.Errorf("setting rebased claim %d: %w", claim.ClaimId, err)
		}
	}
	if err := k.ClaimsAllowanceUsed.Set(ctx, used); err != nil {
		return fmt.Errorf("setting the term's rebased Claims allowance usage: %w", err)
	}

	return sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventClaimsRebased{
		OldLimit: sdk.NewCoin(from, oldLimit),
		NewLimit: sdk.NewCoin(to, limit),
		OldUsed:  sdk.NewCoin(from, oldUsed),
		NewUsed:  sdk.NewCoin(to, used),
		Claims:   uint64(len(charged)),
	})
}

// rebaseAmount re-expresses a reference-unit amount in the new unit, rounded up, and holds it to the
// module's bound here, where a proposal can be refused, rather than at the next export.
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
