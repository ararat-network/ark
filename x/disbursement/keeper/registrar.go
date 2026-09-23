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

// SetRegistrarMandate replaces or disables the registrar appointment. Every replacement advances the
// term. Nothing is reset: the issuance window bounds the chain's registration rate, not the
// appointment's, and suspensions stay effective until governance voids their term.
func (k *Keeper) SetRegistrarMandate(ctx context.Context, authority, committee string, activationHeight, expiryHeight uint64) error {
	current, err := k.RegistrarMandate.Get(ctx)
	if err != nil {
		return err
	}
	envelope, address, err := mandate.Next(current.Envelope, committee, activationHeight, expiryHeight)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if !envelope.IsDisabled() {
		envelope.Observe(k.account.GetAccount(ctx, address), k.wasm.HasContractInfo(ctx, address))
		// Governance holds the unbounded path already, so appointing itself delegates nothing.
		if envelope.Committee == k.authority || envelope.Committee == authority {
			return errors.New("registrar must be distinct from the disbursement authority")
		}
		// A window already behind the chain authorises nothing. Genesis applies no such check: an
		// export taken after expiry must round-trip.
		if height := uint64(sdkCtx.BlockHeight()); envelope.ExpiryHeight <= height {
			return fmt.Errorf("registrar mandate expires at height %d, not above the current height %d", envelope.ExpiryHeight, height)
		}
	}
	updated := types.RegistrarMandate{Envelope: envelope}
	if err := updated.Validate(); err != nil {
		return err
	}
	if err := k.RegistrarMandate.Set(ctx, updated); err != nil {
		return err
	}
	if err := k.record(ctx, 0, "registrar_mandate", authority, chain.NoahCoin(math.ZeroInt()), updated); err != nil {
		return err
	}
	return sdkCtx.EventManager().EmitTypedEvent(&types.EventRegistrarMandateSet{
		Term:             updated.Term,
		Committee:        updated.Committee,
		ActivationHeight: updated.ActivationHeight,
		ExpiryHeight:     updated.ExpiryHeight,
		CommitteeShape:   updated.CommitteeShape,
	})
}

// AuthoriseCommittee checks exact signer, term, and active window, returning the live mandate.
// Handlers and priority-lane admission share these checks.
func (k *Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.RegistrarMandate, error) {
	appointment, err := k.RegistrarMandate.Get(ctx)
	if err != nil {
		return types.RegistrarMandate{}, err
	}
	if err := appointment.Authorise(committee, expectedTerm, uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())); err != nil {
		return types.RegistrarMandate{}, fmt.Errorf("%s: %w", types.RegistrarMandateLabel, err)
	}
	return appointment, nil
}
