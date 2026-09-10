package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/asset/types"
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
	envelope, committeeAddress, err := mandate.Next(current.Envelope, committee, activationHeight, expiryHeight)
	if err != nil {
		return err
	}
	// A disabling has no committee to look up.
	if !envelope.IsDisabled() {
		envelope.Observe(
			k.accountKeeper.GetAccount(ctx, committeeAddress),
			k.wasmKeeper.HasContractInfo(ctx, committeeAddress),
		)
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
			CommitteeShape:   updated.CommitteeShape,
		},
	); err != nil {
		return fmt.Errorf("emitting emergency mandate: %w", err)
	}

	return nil
}

// AuthoriseCommittee validates the exact signer, term, and active mandate window. The handler and
// priority-lane eligibility share these checks; callers enforce action-specific constraints.
func (k Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.EmergencyMandate, error) {
	emergencyMandate, err := k.EmergencyMandate.Get(ctx)
	if err != nil {
		return types.EmergencyMandate{}, fmt.Errorf("getting emergency mandate: %w", err)
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if err := emergencyMandate.Authorise(committee, expectedTerm, height); err != nil {
		return types.EmergencyMandate{}, errorsmod.Wrap(types.ErrEmergencyMandateInactive, err.Error())
	}
	return emergencyMandate, nil
}

// EmergencySuspendAsset applies suspension under the live committee mandate, once per asset per
// term. It checks signer, window, and exact term; the feed and reference pricing remain intact. See
// x/asset/README.md, "The emergency mandate".
func (k Keeper) EmergencySuspendAsset(ctx context.Context, committee string, denom string, expectedTerm uint64) error {
	emergencyMandate, err := k.AuthoriseCommittee(ctx, committee, expectedTerm)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Re-suspending an asset governance has already recovered within the same
	// term is griefing, and requires governance: without this bound a committee
	// re-suspends faster than governance can recover, and governance cannot win
	// that race.
	used, err := k.EmergencySuspensions.Has(ctx, denom)
	if err != nil {
		return fmt.Errorf("checking emergency suspension for asset %s: %w", denom, err)
	}
	if used {
		return errorsmod.Wrapf(
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
	// Committee suspension can occur during transaction execution. Treasury's EndBlock liability
	// valuation reads the resulting final asset state.
	if err := k.EmergencySuspensions.Set(ctx, denom); err != nil {
		return fmt.Errorf("recording emergency suspension for asset %s: %w", denom, err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(
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
