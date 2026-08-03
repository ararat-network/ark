package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
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

// SetReferenceDenom re-points the protocol reference. Changing a configured
// reference denom rebases consumer reference-unit state in the same transaction;
// first configuration has nothing to rebase from.
//
// A non-nil outgoingRate is the rate the outgoing unit is converted out of,
// replacing whatever the store holds. Nil reads the stored rate instead.
func (k Keeper) SetReferenceDenom(ctx context.Context, referenceDenom string, outgoingRate math.LegacyDec) error {
	// Clearing a configured reference is rejected for the same reason an empty
	// one cannot be set: consumers hold state denominated in it.
	if referenceDenom == "" {
		return sdkerrors.Wrap(types.ErrInvalidReferenceDenom, "reference denom must be set")
	}
	// The reference denom names a feed, not necessarily a listed asset, so the
	// identity rule is the feed-key rule and the numeraire is excluded for the
	// same reason it has no feed.
	if err := chain.ValidatePricedDenom(referenceDenom); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidReferenceDenom, "reference denom %v", err)
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
		return sdkerrors.Wrapf(
			types.ErrInvalidReferenceDenom,
			"feed %s is not active and cannot serve as the protocol reference",
			denom,
		)
	}

	return nil
}

// rebaseReferenceDenom re-denominates every consumer's reference-unit state. A
// missing executor or executor error fails the whole action atomically, which
// is the correct escalation: a half-rebased consumer prices against a
// reference denom that no longer exists.
//
// The conversion pair is read once here and handed to both executors, so they
// convert through identical rates and never invent freshness policy. Both
// sides must be fresh. A successor the chain is not currently pricing is no
// recovery at all, and a failed outgoing feed still carries its last
// observation — present, wrong, and exactly the number a re-point exists to
// escape — so reading it at any stored age would convert consumer state at a
// dead price without saying so.
//
// A governance-supplied outgoingRate is how a re-point escapes an outgoing
// feed the chain cannot price, and it replaces the store rather than filling
// in behind it. Absent one, the outgoing rate is read fresh like any other: a
// stale, pruned, or never-priced outgoing unit fails the action by name, and
// governance resubmits stating the rate it means, so that conversion is voted
// on rather than inherited.
func (k Keeper) rebaseReferenceDenom(ctx context.Context, from string, to string, outgoingRate math.LegacyDec) error {
	if k.marketReferenceKeeper == nil || k.treasuryReferenceKeeper == nil {
		return sdkerrors.Wrapf(
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
		return sdkerrors.Wrapf(
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
		return sdkerrors.Wrapf(
			types.ErrReferenceDenomRebaseUnavailable,
			"rebasing Market base pool from %s to %s: %v",
			from,
			to,
			err,
		)
	}
	if err := k.treasuryReferenceKeeper.RebaseTaxCap(ctx, from, to, rates); err != nil {
		return sdkerrors.Wrapf(
			types.ErrReferenceDenomRebaseUnavailable,
			"rebasing Treasury tax cap from %s to %s: %v",
			from,
			to,
			err,
		)
	}

	return nil
}
