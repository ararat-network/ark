package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/mandate"
	"ark/x/asset/types"
)

// SetEmergencyMandate replaces or disables the committee appointment. Every
// replacement advances the term and clears recorded per-term suspensions;
// because replacement always advances the term, clearing on replacement is
// exactly term-scoping. Mandate mutations never touch asset versions.
func (k Keeper) SetEmergencyMandate(ctx context.Context, committee string, activationHeight uint64, expiryHeight uint64) error {
	current, err := k.EmergencyMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting emergency mandate: %w", err)
	}
	envelope, err := mandate.Next(current.Envelope, committee, activationHeight, expiryHeight)
	if err != nil {
		return err
	}

	// The mandate carries no fields of its own, so the derived envelope is the
	// whole appointment.
	updated := types.EmergencyMandate{Envelope: envelope}
	if err := updated.Validate(); err != nil {
		return err
	}

	if err := k.EmergencyMandate.Set(ctx, updated); err != nil {
		return fmt.Errorf("setting emergency mandate: %w", err)
	}
	if err := k.EmergencySuspensions.Clear(ctx, nil); err != nil {
		return fmt.Errorf("clearing emergency suspension usage: %w", err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventEmergencyMandateSet{
			Term:             updated.Term,
			Committee:        updated.Committee,
			ActivationHeight: updated.ActivationHeight,
			ExpiryHeight:     updated.ExpiryHeight,
		},
	); err != nil {
		return fmt.Errorf("emitting emergency mandate: %w", err)
	}

	return nil
}

// EmergencySuspendAsset applies SuspendAsset semantics under a live mandate,
// checking the committee signer, the mandate window, the exact term, and the
// one-suspension-per-asset-per-term bound. It is a pure status move: the
// reference is a feed read, so an asset sharing the reference denomination
// suspends while the unit keeps its price.
//
// Suspension is the committee's only power. Halting issuance contains nothing a
// crisis cares about — the exit leg keeps converting at the full oracle rate —
// so an emergency halt would pay full suspension latency in NOAH dilution while
// signalling committee-confirmed distress through a door it left open. Halting
// is wind-down policy, and policy runs at governance speed.
func (k Keeper) EmergencySuspendAsset(ctx context.Context, committee string, denom string, expectedTerm uint64) error {
	emergencyMandate, err := k.EmergencyMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting emergency mandate: %w", err)
	}
	if err := emergencyMandate.Authorise(
		committee,
		expectedTerm,
		uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()),
	); err != nil {
		return sdkerrors.Wrap(types.ErrEmergencyMandateInactive, err.Error())
	}

	// Re-suspending an asset governance has already recovered within the same
	// term is griefing, and requires governance: without this bound a committee
	// re-suspends faster than governance can recover, and governance cannot win
	// that race.
	used, err := k.EmergencySuspensions.Has(ctx, denom)
	if err != nil {
		return fmt.Errorf("checking emergency suspension for asset %s: %w", denom, err)
	}
	if used {
		return sdkerrors.Wrapf(
			types.ErrEmergencySuspensionConsumed,
			"asset %s already suspended in term %d",
			denom,
			emergencyMandate.Term,
		)
	}

	// Status preconditions stay with the lifecycle operation itself.
	asset, err := k.GetAsset(ctx, denom)
	if err != nil {
		return err
	}
	if err := k.suspendAsset(ctx, asset); err != nil {
		return err
	}
	// This is the only lifecycle transition that lands inside a block still
	// being read: governance transitions execute in x/gov's EndBlocker, after
	// every transaction, while the committee acts in an ordinary one. That once
	// obliged consumers holding a block-scoped fold of the registry to be told,
	// and Treasury's liability snapshot was such a consumer. None remains —
	// Treasury values liability at the end of the block, from final state, so a
	// suspension landing anywhere inside the block is simply seen.
	if err := k.EmergencySuspensions.Set(ctx, denom); err != nil {
		return fmt.Errorf("recording emergency suspension for asset %s: %w", denom, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventEmergencySuspended{
			Denom:   denom,
			Term:    expectedTerm,
			Version: asset.Version + 1,
		},
	); err != nil {
		return fmt.Errorf("emitting emergency suspension for asset %s: %w", denom, err)
	}

	return nil
}
