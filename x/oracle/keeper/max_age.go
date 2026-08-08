package keeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

// GetMaxAge returns the staleness window governing denom: the window its feed
// carries when governance stated one, the chain default otherwise.
//
// A window resolves for any denomination, including one with no feed at all.
// The question a caller asks here is how old a rate may be, not whether the
// feed is a member — and a denomination with no feed has no rate to age.
func (k Keeper) GetMaxAge(ctx context.Context, denom string) (time.Duration, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting params: %w", err)
	}

	return k.getMaxAge(ctx, params, denom)
}

// getMaxAge resolves denom's window against params the caller already holds.
// Rate-set reads walk many denominations under one policy snapshot, and this
// keeps that walk at one params read rather than one per denomination.
func (k Keeper) getMaxAge(ctx context.Context, params types.Params, denom string) (time.Duration, error) {
	override, err := k.MaxExchangeRateAgeOverrides.Get(ctx, denom)
	if err == nil {
		return override, nil
	}
	if !errors.Is(err, collections.ErrNotFound) {
		return 0, fmt.Errorf("getting max exchange rate age override for %s: %w", denom, err)
	}

	return params.MaxExchangeRateAge, nil
}

// GetMaxExchangeRateAgeOverrides returns the sparse windows in key order, which
// is the order genesis export and the query both need.
func (k Keeper) GetMaxExchangeRateAgeOverrides(ctx context.Context) ([]types.ExchangeRateAgeOverride, error) {
	overrides := []types.ExchangeRateAgeOverride{}
	if err := k.MaxExchangeRateAgeOverrides.Walk(
		ctx,
		nil,
		func(denom string, maxAge time.Duration) (bool, error) {
			overrides = append(overrides, types.ExchangeRateAgeOverride{
				Denom:  denom,
				MaxAge: maxAge,
			})

			return false, nil
		},
	); err != nil {
		return nil, fmt.Errorf("iterating max exchange rate age overrides: %w", err)
	}

	return overrides, nil
}

// SetMaxAge states denom's staleness window, where zero means the chain
// default. It is declarative, not incremental: a zero clears whatever the feed
// carried, so the window after this call depends only on the value passed and
// never on what happened to be stored.
//
// A window matching what is already there writes nothing and announces
// nothing. Restating a feed is the ordinary way governance re-approves one, and
// an event per restatement would report changes that did not happen.
func (k Keeper) SetMaxAge(ctx context.Context, denom string, maxAge time.Duration) error {
	if maxAge < 0 {
		return sdkerrors.Wrapf(
			types.ErrInvalidMaxExchangeRateAge,
			"%s max age %s must not be negative",
			denom,
			maxAge,
		)
	}
	// Clearing is deliberately not gated on membership below: returning a
	// denomination to the default must stay possible whatever its feed's state,
	// which is what lets removal prune a departing feed's window.
	if maxAge == 0 {
		return k.clearMaxAgeOverride(ctx, denom)
	}

	// A window on a denomination with no feed is one nothing would ever read
	// and no removal could ever prune: it would sit in state until an export
	// produced a genesis failing its own validation. AddFeed settles membership
	// before reaching here, so the phase is Adding or Active by now — a feed
	// still scheduled to join counts, which is what lets one message state
	// membership and window together.
	phase, err := k.FeedPhase(ctx, denom)
	if err != nil {
		return err
	}
	if phase == types.FeedPhaseOff {
		return sdkerrors.Wrapf(types.ErrFeedNotFound, "cannot set max age for %s", denom)
	}

	stored, err := k.MaxExchangeRateAgeOverrides.Get(ctx, denom)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("getting max exchange rate age override for %s: %w", denom, err)
	}
	if err == nil && stored == maxAge {
		return nil
	}
	if err := k.MaxExchangeRateAgeOverrides.Set(ctx, denom, maxAge); err != nil {
		return fmt.Errorf("setting max exchange rate age override for %s: %w", denom, err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventMaxExchangeRateAgeOverrideSet{Denom: denom, MaxAge: maxAge},
	); err != nil {
		return fmt.Errorf("emitting max exchange rate age override for %s: %w", denom, err)
	}

	return nil
}

// clearMaxAgeOverride returns denom to the chain default. A denomination that
// carried no window is already there, so the absent case is success and stays
// silent: feed removal calls this for every departing feed, and most of them
// never had a window to lose.
func (k Keeper) clearMaxAgeOverride(ctx context.Context, denom string) error {
	found, err := k.MaxExchangeRateAgeOverrides.Has(ctx, denom)
	if err != nil {
		return fmt.Errorf("checking max exchange rate age override for %s: %w", denom, err)
	}
	if !found {
		return nil
	}
	if err := k.MaxExchangeRateAgeOverrides.Remove(ctx, denom); err != nil {
		return fmt.Errorf("removing max exchange rate age override for %s: %w", denom, err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventMaxExchangeRateAgeOverrideRemoved{Denom: denom},
	); err != nil {
		return fmt.Errorf("emitting removed max exchange rate age override for %s: %w", denom, err)
	}

	return nil
}
