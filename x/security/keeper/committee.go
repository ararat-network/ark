package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/x/security/types"
)

// A committee action runs this file in order: authorise the appointment, work
// out whose plan holds the upgrade slot, then dispatch the upstream message as
// the chain authority.

// AuthoriseCommittee checks exact signer, term, and active window, returning the live mandate.
// Handlers and priority-lane admission share these checks.
func (k Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.SecurityMandate, error) {
	securityMandate, err := k.Mandate.Get(ctx)
	if err != nil {
		return types.SecurityMandate{}, fmt.Errorf("getting security mandate: %w", err)
	}
	if err := securityMandate.Authorise(
		committee,
		expectedTerm,
		uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()),
	); err != nil {
		return types.SecurityMandate{}, fmt.Errorf("%s: %w", types.SecurityMandateLabel, err)
	}

	return securityMandate, nil
}

// pendingUpgradePlan returns the plan x/upgrade currently holds, and whether
// there is one at all. A missing plan is an ordinary state, not a failure.
func (k Keeper) pendingUpgradePlan(ctx context.Context) (upgradetypes.Plan, bool, error) {
	plan, err := k.upgradeKeeper.GetUpgradePlan(ctx)
	if err != nil {
		if errors.Is(err, upgradetypes.ErrNoUpgradePlanFound) {
			return upgradetypes.Plan{}, false, nil
		}

		return upgradetypes.Plan{}, false, fmt.Errorf("getting upgrade plan: %w", err)
	}

	return plan, true, nil
}

// isCommitteePlan matches pending plan name and height against the committee record. Any mismatch
// is treated as governance-owned and cannot be replaced or cancelled by the committee.
func (k Keeper) isCommitteePlan(ctx context.Context, plan upgradetypes.Plan) (bool, error) {
	record, err := k.CommitteePlan.Get(ctx)
	if err != nil {
		return false, fmt.Errorf("getting committee plan record: %w", err)
	}

	return record.Matches(plan.Name, plan.Height), nil
}

// effectiveAuthority follows sdk.ValidateAuthority: use consensus authority when present, otherwise
// the module fallback. Targets independently validate the injected signer.
func (k Keeper) effectiveAuthority(ctx context.Context) string {
	consensusParams := sdk.UnwrapSDKContext(ctx).ConsensusParams()
	if consensusParams.Authority != nil && consensusParams.Authority.Authority != "" {
		return consensusParams.Authority.Authority
	}

	return k.authority
}

// dispatch routes one message this module constructed to its upstream handler.
// The router runs the target's ValidateBasic and full handler, so the message
// is checked exactly as it would be arriving in a transaction, and a failure
// aborts this one atomically.
func (k Keeper) dispatch(ctx context.Context, msg sdk.Msg) error {
	handler := k.router.Handler(msg)
	if handler == nil {
		return fmt.Errorf("no handler registered for %s", sdk.MsgTypeURL(msg))
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	res, err := handler(sdkCtx, msg)
	if err != nil {
		return fmt.Errorf("dispatching %s: %w", sdk.MsgTypeURL(msg), err)
	}
	// The router gives the target its own event manager and returns what it
	// emitted on the result, so events not forwarded here are lost: a client
	// recovery would be invisible to every relayer watching for it.
	sdkCtx.EventManager().EmitEvents(res.GetEvents())

	return nil
}
