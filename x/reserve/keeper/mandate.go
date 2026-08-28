package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/reserve/types"
)

// authoriseCommittee checks the signer against the live mandate, returning it
// for the caller's own constraints. The signer is deliberately not pre-parsed
// for canonical spelling: the envelope canonicalises before comparing, so any
// letter case of the appointed address is the same authenticated account.
func (k *Keeper) authoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.ReserveMandate, error) {
	reserveMandate, err := k.Mandate.Get(ctx)
	if err != nil {
		return types.ReserveMandate{}, fmt.Errorf("getting Reserve mandate: %w", err)
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if err := reserveMandate.Authorise(committee, expectedTerm, height); err != nil {
		return types.ReserveMandate{}, fmt.Errorf("%s: %w", types.ReserveMandateLabel, err)
	}
	return reserveMandate, nil
}

// addAllowanceUsed consumes term deployment allowance, refusing to exceed it.
func (k *Keeper) addAllowanceUsed(ctx context.Context, reserveMandate types.ReserveMandate, amount math.Int) error {
	used, err := k.AllowanceUsed.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Reserve allowance usage: %w", err)
	}
	next, err := used.SafeAdd(amount)
	if err != nil {
		return fmt.Errorf("adding Reserve allowance usage: %w", err)
	}
	if next.GT(reserveMandate.DeploymentAllowance.Amount) {
		return fmt.Errorf(
			"deployment %s exceeds remaining Reserve allowance %s",
			amount,
			reserveMandate.DeploymentAllowance.Amount.Sub(used),
		)
	}
	if err := k.AllowanceUsed.Set(ctx, next); err != nil {
		return fmt.Errorf("setting Reserve allowance usage: %w", err)
	}
	return nil
}
