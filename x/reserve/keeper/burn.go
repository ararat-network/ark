package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
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

// BurnableSurplus bounds NOAH disposal by both recognised capital above requirement and custody
// above the mandate floor. An unavailable requirement is an error, never permission to burn.
func (k Keeper) BurnableSurplus(ctx context.Context) (math.Int, error) {
	if k.treasuryReader == nil {
		return math.Int{}, errors.New("reserve capital requirement reader is not wired")
	}
	required, err := k.treasuryReader.RequiredReserveCapital(ctx)
	if err != nil {
		return math.Int{}, fmt.Errorf("getting Reserve capital requirement: %w", err)
	}
	recognised, err := k.RecognisedCapital(ctx)
	if err != nil {
		return math.Int{}, err
	}
	surplus := recognised.Sub(required)
	if !surplus.IsPositive() {
		return math.ZeroInt(), nil
	}

	reserveMandate, err := k.Mandate.Get(ctx)
	if err != nil {
		return math.Int{}, fmt.Errorf("getting Reserve mandate: %w", err)
	}
	spendable := k.balance(ctx).Sub(reserveMandate.MinimumNoahBalance.Amount)
	if !spendable.IsPositive() {
		return math.ZeroInt(), nil
	}
	return math.MinInt(surplus, spendable), nil
}
