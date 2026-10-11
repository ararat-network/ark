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
	"github.com/ararat-network/ark/x/disbursement/types"
)

// getMember reads the grant a member address holds, reporting an address with none as the
// module's not-found. The index pointing at a missing grant is an invariant failure, not a not-found.
func (k *Keeper) getMember(ctx context.Context, address sdk.AccAddress) (types.Grant, error) {
	id, err := k.Members.Get(ctx, address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Grant{}, errorsmod.Wrap(types.ErrMemberNotFound, address.String())
		}
		return types.Grant{}, fmt.Errorf("getting member %s: %w", address, err)
	}
	g, err := k.Grants.Get(ctx, id)
	if err != nil {
		return types.Grant{}, fmt.Errorf("getting member %s grant %d: %w", address, id, err)
	}
	return g, nil
}

// liveMember reads a member grant that is still running: not cancelled, with principal remaining.
func (k *Keeper) liveMember(ctx context.Context, address sdk.AccAddress) (types.Grant, error) {
	g, err := k.getMember(ctx, address)
	if err != nil {
		return types.Grant{}, err
	}
	if g.Cancelled() || g.Remaining.IsZero() {
		return types.Grant{}, errors.New("member grant is closed")
	}
	return g, nil
}

// suspend pauses a live member's payments and takes it off the payment queue.
func (k *Keeper) suspend(ctx context.Context, actor string, address sdk.AccAddress) error {
	g, err := k.liveMember(ctx, address)
	if err != nil {
		return err
	}
	if g.Suspended() {
		return errors.New("member already suspended")
	}
	g.SuspendedTime = chain.BlockSeconds(ctx)
	if err := k.setDueTime(ctx, &g, 0); err != nil {
		return err
	}
	return k.writeGrant(ctx, g, types.GrantEntry{
		GrantId: g.Id,
		Action:  types.GrantAction_GRANT_ACTION_SUSPENDED,
		Actor:   actor,
		Amount:  sdk.NewCoin(g.Amount.Denom, math.ZeroInt()),
	})
}

// reinstate clears a member's suspension and queues the arrears for this block, or for the end of
// its hold if it has not ended; the committee and governance share it under their own authorisation.
func (k *Keeper) reinstate(ctx context.Context, actor string, address sdk.AccAddress) error {
	g, err := k.liveMember(ctx, address)
	if err != nil {
		return err
	}
	if !g.Suspended() {
		return errors.New("member is not suspended")
	}
	g.SuspendedTime = 0
	if err := k.setDueTime(ctx, &g, max(chain.BlockSeconds(ctx), g.CreatedTime+g.Schedule.FirstLength)); err != nil {
		return err
	}
	return k.writeGrant(ctx, g, types.GrantEntry{
		GrantId: g.Id,
		Action:  types.GrantAction_GRANT_ACTION_REINSTATED,
		Actor:   actor,
		Amount:  sdk.NewCoin(g.Amount.Denom, math.ZeroInt()),
	})
}
