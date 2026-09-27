package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// SetGrantsMandate replaces or disables the committee appointment. Every replacement advances the
// term, and awards are counted by term, so a new appointment starts with nothing used. Suspensions
// stay effective until governance voids their term.
func (k *Keeper) SetGrantsMandate(ctx context.Context, msg *types.MsgSetGrantsMandate) error {
	current, err := k.GrantsMandate.Get(ctx)
	if err != nil {
		return err
	}
	envelope, address, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	updated := types.NewDisabledGrantsMandate(envelope.Term)
	if !envelope.IsDisabled() {
		envelope.Observe(k.account.GetAccount(ctx, address), k.wasm.HasContractInfo(ctx, address))
		// Governance holds the unbounded path already, so appointing itself delegates nothing.
		if envelope.Committee == k.authority || envelope.Committee == msg.Authority {
			return errors.New("committee must be distinct from the disbursement authority")
		}
		// A window already behind the chain authorises nothing. Genesis applies no such check: an
		// export taken after expiry must round-trip.
		if height := uint64(sdkCtx.BlockHeight()); envelope.ExpiryHeight <= height {
			return fmt.Errorf("grants mandate expires at height %d, not above the current height %d", envelope.ExpiryHeight, height)
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return err
		}
		// An allowance the chain could never award in is a stillborn delegation.
		for _, c := range msg.CompensationAllowance {
			if err := k.validateCompensation(ctx, c.Denom, params); err != nil {
				return fmt.Errorf("compensation allowance: %w", err)
			}
		}
		updated.CompensationAllowance, updated.MinFirstPeriod = msg.CompensationAllowance, msg.MinFirstPeriod
	}
	updated.Envelope = envelope
	if err := updated.Validate(); err != nil {
		return err
	}
	if err := k.GrantsMandate.Set(ctx, updated); err != nil {
		return err
	}
	if err := k.record(ctx, 0, "grants_mandate", msg.Authority, chain.NoahCoin(math.ZeroInt()), updated); err != nil {
		return err
	}
	return sdkCtx.EventManager().EmitTypedEvent(&types.EventGrantsMandateSet{
		Term:             updated.Term,
		Committee:        updated.Committee,
		ActivationHeight: updated.ActivationHeight,
		ExpiryHeight:     updated.ExpiryHeight,
		CommitteeShape:   updated.CommitteeShape,
	})
}

// AuthoriseCommittee checks exact signer, term, and active window, returning the live mandate.
// Handlers and priority-lane admission share these checks.
func (k *Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.GrantsMandate, error) {
	appointment, err := k.GrantsMandate.Get(ctx)
	if err != nil {
		return types.GrantsMandate{}, err
	}
	if err := appointment.Authorise(committee, expectedTerm, uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())); err != nil {
		return types.GrantsMandate{}, fmt.Errorf("%s: %w", types.GrantsMandateLabel, err)
	}
	return appointment, nil
}

// termUsage sums and counts a term's committee awards, cancelled ones included.
func (k *Keeper) termUsage(ctx context.Context, term uint64) (sdk.Coins, uint64, error) {
	used, count := sdk.NewCoins(), uint64(0)
	err := k.TermGrants.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](term), func(_ collections.Pair[uint64, uint64], amount sdk.Coin) (bool, error) {
		used = used.Add(amount) // At most MaxTermGrants 128-bit amounts.
		count++
		return false, nil
	})
	return used, count, err
}
