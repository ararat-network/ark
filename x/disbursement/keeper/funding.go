package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// conversion is what a stablecoin award bought: the coins and the NOAH they cost.
type conversion struct {
	offer  math.Int
	output sdk.Coin
}

// fund backs an award before it is reserved. A NOAH award needs nothing; a stablecoin award uses
// uncommitted custody first and buys the shortfall through Market with contributor-pool NOAH, paying
// at most maxOffer. Market delivers at least the shortfall, sometimes a few base units more, which
// stay in custody. The message is atomic, so an offer the pool cannot cover reverts the buy. It
// returns nil when nothing was bought.
func (k *Keeper) fund(ctx context.Context, amount sdk.Coin, maxOffer math.Int) (*conversion, error) {
	if maxOffer.IsNil() {
		maxOffer = math.ZeroInt()
	}
	if err := types.ValidateAmount(maxOffer, false); err != nil {
		return nil, fmt.Errorf("max_offer: %w", err)
	}
	if err := amount.Validate(); err != nil {
		return nil, err
	}
	if amount.Denom == chain.NoahBaseDenom {
		if !maxOffer.IsZero() {
			return nil, errors.New("a NOAH award converts nothing")
		}
		return nil, nil
	}
	custody, err := k.uncommitted(ctx, types.ContributorsName, amount.Denom)
	if err != nil {
		return nil, err
	}
	if !custody.LT(amount.Amount) {
		return nil, nil
	}
	shortfall := sdk.NewCoin(amount.Denom, amount.Amount.Sub(custody))
	if maxOffer.IsZero() {
		return nil, fmt.Errorf("custody holds %s of %s; set max_offer to convert the rest", sdk.NewCoin(amount.Denom, custody), amount)
	}
	pool, err := k.uncommitted(ctx, types.ContributorsName, chain.NoahBaseDenom)
	if err != nil {
		return nil, err
	}
	contributors := k.accountKeeper.GetModuleAddress(types.ContributorsName)
	offer, output, _, err := k.marketKeeper.Buy(
		ctx,
		contributors,
		shortfall,
		chain.NoahBaseDenom,
		chain.NoahCoin(maxOffer),
	)
	if err != nil {
		return nil, fmt.Errorf("funding %s: %w", shortfall, err)
	}
	if offer.Amount.GT(pool) {
		return nil, fmt.Errorf("offer %s exceeds the contributor pool", offer)
	}
	return &conversion{offer: offer.Amount, output: output}, nil
}

// recordConversion journals a conversion under its grant: the stablecoin bought as the amount and the
// NOAH offer it cost; Market's own event carries the fee. Nil records nothing.
func (k *Keeper) recordConversion(ctx context.Context, id uint64, actor string, c *conversion) error {
	if c == nil {
		return nil
	}
	return k.recordGrant(ctx, types.GrantEntry{
		GrantId: id,
		Action:  types.GrantAction_GRANT_ACTION_CONVERTED,
		Actor:   actor,
		Amount:  c.output,
		Offer:   c.offer,
	})
}
