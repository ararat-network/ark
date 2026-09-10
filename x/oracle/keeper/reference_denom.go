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
	"github.com/ararat-network/ark/x/oracle/types"
)

// GetReferenceDenom returns the denomination whose feed serves as the protocol
// reference denom, empty when governance has never configured one.
func (k Keeper) GetReferenceDenom(ctx context.Context) (string, error) {
	referenceDenom, err := k.ReferenceDenom.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return "", nil
		}

		return "", fmt.Errorf("getting protocol reference: %w", err)
	}

	return referenceDenom, nil
}

// SetReferenceDenom changes the protocol reference and rebases existing consumer state atomically.
// First configuration has no outgoing state. A non-nil outgoingRate replaces the stored rate; nil
// requires a fresh read.
func (k Keeper) SetReferenceDenom(ctx context.Context, referenceDenom string, outgoingRate math.LegacyDec) error {
	// Clearing a configured reference is rejected for the same reason an empty
	// one cannot be set: consumers hold state denominated in it.
	if referenceDenom == "" {
		return errorsmod.Wrap(types.ErrInvalidReferenceDenom, "reference denom must be set")
	}
	// The reference denom names a feed, not necessarily a listed asset, so the
	// identity rule is the feed-key rule and the numeraire is excluded for the
	// same reason it has no feed.
	if err := chain.ValidatePricedDenom(referenceDenom); err != nil {
		return errorsmod.Wrapf(types.ErrInvalidReferenceDenom, "reference denom %v", err)
	}
	if err := k.requireFeedActive(ctx, referenceDenom); err != nil {
		return err
	}

	current, err := k.GetReferenceDenom(ctx)
	if err != nil {
		return err
	}
	if current != "" && current != referenceDenom {
		if err := k.rebaseReferenceDenom(ctx, current, referenceDenom, outgoingRate); err != nil {
			return err
		}
	}

	if err := k.ReferenceDenom.Set(ctx, referenceDenom); err != nil {
		return fmt.Errorf("setting protocol reference: %w", err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventReferenceDenomUpdated{
			ReferenceDenom: referenceDenom,
		},
	); err != nil {
		return fmt.Errorf("emitting reference denom update: %w", err)
	}

	return nil
}

// requireFeedActive enforces the only rule the reference has: its feed
// is Active. Both consumers read a rate, so nothing here consults the asset
// registry — a reference unit need not be a listed asset, and an asset sharing
// the denomination may be in any status.
func (k Keeper) requireFeedActive(ctx context.Context, denom string) error {
	phase, err := k.FeedPhase(ctx, denom)
	if err != nil {
		return fmt.Errorf("getting feed phase for reference denom %s: %w", denom, err)
	}
	if phase != types.FeedPhaseActive {
		return errorsmod.Wrapf(
			types.ErrInvalidReferenceDenom,
			"feed %s is not active and cannot serve as the protocol reference",
			denom,
		)
	}

	return nil
}

// rebaseReferenceDenom supplies one captured rate pair to both required executors. Both rates must
// be fresh unless governance supplies the outgoing rate, which bypasses that feed read. Any
// executor failure aborts the action; see x/oracle/README.md.
func (k Keeper) rebaseReferenceDenom(ctx context.Context, from string, to string, outgoingRate math.LegacyDec) error {
	if k.marketReferenceKeeper == nil || k.treasuryReferenceKeeper == nil {
		return errorsmod.Wrapf(
			types.ErrReferenceDenomRebaseUnavailable,
			"moving reference denom %s with no rebase executors wired",
			from,
		)
	}

	// A supplied rate makes the outgoing feed irrelevant, so it is not priced
	// at all — that is what keeps a never-priced unit convertible.
	priced := []string{to}
	if outgoingRate.IsNil() {
		priced = append(priced, from)
	}
	rates, err := k.GetRateSet(ctx, priced...)
	if err != nil {
		return errorsmod.Wrapf(
			types.ErrReferenceDenomRebaseUnavailable,
			"pricing reference denom move from %s to %s: %v",
			from,
			to,
			err,
		)
	}
	if !outgoingRate.IsNil() {
		rates[from] = outgoingRate
	}

	if err := k.marketReferenceKeeper.RebaseBasePool(ctx, from, to, rates); err != nil {
		return errorsmod.Wrapf(
			types.ErrReferenceDenomRebaseUnavailable,
			"rebasing Market base pool from %s to %s: %v",
			from,
			to,
			err,
		)
	}
	if err := k.treasuryReferenceKeeper.RebaseReferenceState(ctx, from, to, rates); err != nil {
		return errorsmod.Wrapf(
			types.ErrReferenceDenomRebaseUnavailable,
			"rebasing Treasury reference state from %s to %s: %v",
			from,
			to,
			err,
		)
	}

	return nil
}
