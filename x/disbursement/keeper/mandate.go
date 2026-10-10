package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// SetGrantsMandate replaces or disables the committee appointment. Every replacement advances the
// term, zeroes the term's usage, and clears the registrar the outgoing committee appointed.
// Suspensions stand until reinstated.
func (k *Keeper) SetGrantsMandate(ctx context.Context, msg *types.MsgSetGrantsMandate) error {
	current, err := k.GrantsMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting grants mandate: %w", err)
	}
	envelope, address, err := mandate.Next(
		current.Envelope,
		msg.Committee,
		msg.ActivationTime,
		msg.ExpiryTime,
	)
	if err != nil {
		return err
	}
	// A disabling has no committee to look up.
	if !envelope.IsDisabled() {
		envelope.Observe(k.accountKeeper.GetAccount(ctx, address), k.wasmKeeper.HasContractInfo(ctx, address))
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	updated := types.NewDisabledGrantsMandate(envelope.Term)
	updated.Envelope = envelope
	if msg.Committee != "" {
		// An allowance left out of the message is none, as max_offer is.
		updated.CompensationAllowance = msg.CompensationAllowance
		if updated.CompensationAllowance.IsNil() {
			updated.CompensationAllowance = math.ZeroInt()
		}
		if err := updated.Validate(); err != nil {
			return err
		}
		// Governance already holds the unbounded path, so appointing itself delegates nothing.
		if updated.Committee == k.authority || updated.Committee == msg.Authority {
			return errors.New("grants committee must be distinct from the disbursement authority")
		}
		// An expired window authorises nothing and would only displace the incumbent and advance the
		// term. Genesis skips this check, so an export taken after expiry round-trips.
		if now := chain.BlockSeconds(ctx); updated.ExpiryTime <= now {
			return fmt.Errorf(
				"%s expires at time %d, which is not after the current block time %d",
				types.GrantsMandateLabel,
				updated.ExpiryTime,
				now,
			)
		}
	}
	if err := k.GrantsMandate.Set(ctx, updated); err != nil {
		return fmt.Errorf("setting grants mandate: %w", err)
	}
	if err := k.CompensationUsed.Set(ctx, math.ZeroInt()); err != nil {
		return fmt.Errorf("resetting compensation usage: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventGrantsMandateSet{
		Term:           updated.Term,
		Committee:      updated.Committee,
		ActivationTime: updated.ActivationTime,
		ExpiryTime:     updated.ExpiryTime,
		CommitteeShape: updated.CommitteeShape,
	}); err != nil {
		return fmt.Errorf("emitting grants mandate: %w", err)
	}
	// The registrar is the committee's delegate: a committee appointment clears it, so no key
	// registers under a replaced or disabled committee.
	registrar, err := k.RegistrarMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting registrar mandate: %w", err)
	}
	if registrar.IsDisabled() {
		return nil
	}
	return k.SetRegistrarMandate(ctx, updated, "", 0, 0)
}

// AuthoriseCommittee checks exact signer, term, and active window, returning the live mandate.
// Handlers and priority-lane admission share these checks.
func (k *Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.GrantsMandate, error) {
	appointment, err := k.GrantsMandate.Get(ctx)
	if err != nil {
		return types.GrantsMandate{}, fmt.Errorf("getting grants mandate: %w", err)
	}
	if err := appointment.Authorise(committee, expectedTerm, chain.BlockSeconds(ctx)); err != nil {
		return types.GrantsMandate{}, fmt.Errorf("%s: %w", types.GrantsMandateLabel, err)
	}
	return appointment, nil
}

// SetRegistrarMandate replaces or disables the registrar appointment under committee, the live grants
// mandate, for a window inside the committee's. Every replacement advances the term; the open member
// tranche is the registrar's only bound.
func (k *Keeper) SetRegistrarMandate(ctx context.Context, committee types.GrantsMandate, registrar string, activationTime, expiryTime uint64) error {
	current, err := k.RegistrarMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting registrar mandate: %w", err)
	}
	envelope, address, err := mandate.Next(current.Envelope, registrar, activationTime, expiryTime)
	if err != nil {
		return err
	}
	// A disabling has no registrar to look up.
	if !envelope.IsDisabled() {
		envelope.Observe(k.accountKeeper.GetAccount(ctx, address), k.wasmKeeper.HasContractInfo(ctx, address))
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	updated := types.NewDisabledRegistrarMandate(envelope.Term)
	updated.Envelope = envelope
	if registrar != "" {
		if err := updated.Validate(); err != nil {
			return err
		}
		if err := updated.WithinCommittee(committee); err != nil {
			return err
		}
		// Governance already holds the unbounded path; the committee may name itself.
		if updated.Committee == k.authority {
			return errors.New("registrar must be distinct from the disbursement authority")
		}
		// An expired window authorises nothing and would only displace the incumbent and advance the
		// term. Genesis skips this check, so an export taken after expiry round-trips.
		if now := chain.BlockSeconds(ctx); updated.ExpiryTime <= now {
			return fmt.Errorf(
				"%s expires at time %d, which is not after the current block time %d",
				types.RegistrarMandateLabel,
				updated.ExpiryTime,
				now,
			)
		}
	}
	if err := k.RegistrarMandate.Set(ctx, updated); err != nil {
		return fmt.Errorf("setting registrar mandate: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventRegistrarMandateSet{
		Term:           updated.Term,
		Registrar:      updated.Committee,
		ActivationTime: updated.ActivationTime,
		ExpiryTime:     updated.ExpiryTime,
		CommitteeShape: updated.CommitteeShape,
	}); err != nil {
		return fmt.Errorf("emitting registrar mandate: %w", err)
	}
	return nil
}

// AuthoriseRegistrar checks exact signer, term, and active window, returning the live registrar
// appointment. Registration rides the normal lane, so only the handler runs it.
func (k *Keeper) AuthoriseRegistrar(ctx context.Context, registrar string, expectedTerm uint64) (types.RegistrarMandate, error) {
	appointment, err := k.RegistrarMandate.Get(ctx)
	if err != nil {
		return types.RegistrarMandate{}, fmt.Errorf("getting registrar mandate: %w", err)
	}
	if err := appointment.Authorise(registrar, expectedTerm, chain.BlockSeconds(ctx)); err != nil {
		return types.RegistrarMandate{}, fmt.Errorf("%s: %w", types.RegistrarMandateLabel, err)
	}
	return appointment, nil
}
