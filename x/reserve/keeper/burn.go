package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

// requirePaperBurnable permits only uncredited registry paper. Eligibility symbols and registry
// denominations are disjoint, making the credit check unreachable; retain it as a backstop for this
// otherwise unbounded burn.
func (k *Keeper) requirePaperBurnable(ctx context.Context, amounts sdk.Coins) error {
	for _, coin := range amounts {
		// Subsumed by the membership test, and kept for the boundary it names:
		// NOAH leaves through CommitteeBurnSurplus alone.
		if coin.Denom == chain.NoahBaseDenom {
			return errors.New("committee paper burns must not include " + chain.NoahBaseDenom)
		}
		registered, err := k.assetKeeper.HasAsset(ctx, coin.Denom)
		if err != nil {
			return fmt.Errorf("checking asset registry for %s: %w", coin.Denom, err)
		}
		if !registered {
			return fmt.Errorf(
				"%s is not an Ark-issued asset and may only be burned by governance: "+
					"destroying external custody extinguishes no liability",
				coin.Denom,
			)
		}
		entry, err := k.RecognitionPolicy.Get(ctx, coin.Denom)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return fmt.Errorf("getting eligibility entry for %s: %w", coin.Denom, err)
		}
		if entry.GrantsCredit() {
			return fmt.Errorf(
				"%s carries recognition credit and may only be burned by governance",
				coin.Denom,
			)
		}
	}
	return nil
}

// BurnableSurplus is the largest NOAH burn that leaves recognised capital at the requirement and
// custody at the mandate floor. An unavailable requirement is an error, never permission to burn.
func (k Keeper) BurnableSurplus(ctx context.Context) (math.Int, error) {
	required, stakes, floor, err := k.burnInputs(ctx)
	if err != nil {
		return math.Int{}, err
	}
	return burnableSurplus(k.balance(ctx), floor, required, stakes)
}

// CheckSurplusBurn admits one committee NOAH burn: within the bound, and certified by recognition at
// the balance it leaves, so the requirement holds after the burn by construction rather than by
// the search's monotonicity. It returns the bound for the caller's report.
func (k Keeper) CheckSurplusBurn(ctx context.Context, amount math.Int) (math.Int, error) {
	required, stakes, floor, err := k.burnInputs(ctx)
	if err != nil {
		return math.Int{}, err
	}
	balance := k.balance(ctx)
	surplus, err := burnableSurplus(balance, floor, required, stakes)
	if err != nil {
		return math.Int{}, err
	}
	if amount.GT(surplus) {
		return math.Int{}, fmt.Errorf(
			"burn %s exceeds the burnable Reserve surplus %s",
			chain.NoahCoin(amount),
			chain.NoahCoin(surplus),
		)
	}
	remaining, err := recognisedAt(balance.Sub(amount), stakes)
	if err != nil {
		return math.Int{}, err
	}
	if remaining.LT(required) {
		return math.Int{}, fmt.Errorf(
			"burn %s leaves recognised capital %s below the requirement %s",
			chain.NoahCoin(amount),
			chain.NoahCoin(remaining),
			chain.NoahCoin(required),
		)
	}
	return surplus, nil
}

// burnableSurplus sizes the bound. A clipped stake's credit is its ratio of the balance, so a unit
// burned lowers recognised capital by 1 + Σ clipped ratios and the pre-burn surplus overstates the
// power; the bound is searched on recognition at the post-burn balance instead, whose verdict is
// monotone in the balance.
func burnableSurplus(balance, floor, required math.Int, stakes []types.RecognitionStake) (math.Int, error) {
	recognised, err := recognisedAt(balance, stakes)
	if err != nil {
		return math.Int{}, err
	}
	surplus := recognised.Sub(required)
	if !surplus.IsPositive() {
		return math.ZeroInt(), nil
	}
	spendable := balance.Sub(floor)
	if !spendable.IsPositive() {
		return math.ZeroInt(), nil
	}

	// A unit burned lowers recognised capital by at least a unit, so the pre-burn surplus bounds
	// the search from above and the floor independently.
	low, high := math.ZeroInt(), math.MinInt(surplus, spendable)
	for low.LT(high) {
		mid := low.Add(high).AddRaw(1).QuoRaw(2)
		remaining, err := recognisedAt(balance.Sub(mid), stakes)
		if err != nil {
			return math.Int{}, err
		}
		if remaining.GTE(required) {
			low = mid
		} else {
			high = mid.SubRaw(1)
		}
	}
	return low, nil
}

// burnInputs reads what a burn is sized against: the requirement, the recognition stakes, and the
// mandate's NOAH floor.
func (k Keeper) burnInputs(ctx context.Context) (math.Int, []types.RecognitionStake, math.Int, error) {
	if k.treasuryReader == nil {
		return math.Int{}, nil, math.Int{}, errors.New("reserve capital requirement reader is not wired")
	}
	required, err := k.treasuryReader.RequiredReserveCapital(ctx)
	if err != nil {
		return math.Int{}, nil, math.Int{}, fmt.Errorf("getting Reserve capital requirement: %w", err)
	}
	_, stakes, err := k.recognitionStakes(ctx)
	if err != nil {
		return math.Int{}, nil, math.Int{}, err
	}
	reserveMandate, err := k.Mandate.Get(ctx)
	if err != nil {
		return math.Int{}, nil, math.Int{}, fmt.Errorf("getting Reserve mandate: %w", err)
	}
	return required, stakes, reserveMandate.MinimumNoahBalance.Amount, nil
}
