package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"ark/x/security/types"
)

// A committee action runs this file in order: authorise the appointment, work
// out whose plan holds the upgrade slot, then dispatch the upstream message as
// the chain authority.

// authoriseCommittee runs the check every committee action shares: the exact
// signer, the exact term, and the active window. It returns the live mandate
// so a caller can record the acting term.
func (k Keeper) authoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.SecurityMandate, error) {
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

// isCommitteePlan reports whether the pending plan is the committee's own, by
// matching this module's record against it on both name and height. Anything
// stale therefore reads as governance's, which is the conservative direction:
// the committee is told to wait rather than reaching a plan the chain voted
// for.
func (k Keeper) isCommitteePlan(ctx context.Context, plan upgradetypes.Plan) (bool, error) {
	record, err := k.CommitteePlan.Get(ctx)
	if err != nil {
		return false, fmt.Errorf("getting committee plan record: %w", err)
	}

	return record.Matches(plan.Name, plan.Height), nil
}

// effectiveAuthority resolves the address a dispatched message must be signed
// by, mirroring sdk.ValidateAuthority exactly: consensus params first when they
// name an authority, the module's own fallback otherwise. Resolving it any
// other way would not create a privilege — every target re-validates the
// signer — but would break every committee power on a chain that has rotated
// its authority.
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
