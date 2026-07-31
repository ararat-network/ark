package keeper

import (
	"context"
	"fmt"
	"strings"

	sdkerrors "cosmossdk.io/errors"

	"ark/x/oracle/types"
)

// FeedReferentGuard reports the claims a consumer holds on a feed.
//
// Guards derive their answer from the consumer's own authoritative state at
// call time; nothing is indexed here. That is the deliberate difference from
// the deleted asset-lock pattern: an index needs a write on every consumer
// transition and can drift from the state it summarises, while a derived check
// costs one read on the rare governance path that removes a feed.
type FeedReferentGuard interface {
	// FeedReferents returns every claim the consumer holds on denom, or an
	// empty slice when it holds none. A consumer with several reasons reports
	// all of them: governance sees every blocker at once rather than
	// discovering them one rejected proposal at a time.
	FeedReferents(ctx context.Context, denom string) ([]types.FeedReferent, error)
}

// SetFeedReferentGuards registers the complete removal-guard set. App wiring
// owns the set and it holds exactly the consumers that exist: today x/asset,
// which answers for both the registry and the protocol reference; basket and
// reserve guards join with their specs. One entry per consumer, not per claim,
// because a consumer knows its own reasons. Consumers depend on x/oracle, so
// the reverse edge is injected at wiring rather than imported.
func (k *Keeper) SetFeedReferentGuards(guards ...FeedReferentGuard) {
	k.feedReferentGuards = guards
}

// FeedReferents collects every consumer claim on a feed. It is the single
// source of truth for both the removal check and Query/FeedReferents, so what
// operators inspect is exactly what governance is judged against.
//
// A denom with no feed is ErrFeedNotFound rather than an empty claim list.
// There is nothing for a consumer to hold a claim on, so asking the guards is
// pointless work, and an empty answer would read as "safe to remove" for what
// is really a typo or an already-removed feed. Adding and Removing both count
// as existing: an asset awaiting activation legitimately pins a feed that has
// not activated yet, and the referents of an in-flight removal are worth being
// able to ask for.
func (k Keeper) FeedReferents(ctx context.Context, denom string) ([]types.FeedReferent, error) {
	phase, err := k.FeedPhase(ctx, denom)
	if err != nil {
		return nil, err
	}
	if phase == types.FeedPhaseOff {
		return nil, sdkerrors.Wrap(types.ErrFeedNotFound, denom)
	}

	var referents []types.FeedReferent
	for _, guard := range k.feedReferentGuards {
		claims, err := guard.FeedReferents(ctx, denom)
		if err != nil {
			return nil, fmt.Errorf("checking feed %s referents: %w", denom, err)
		}
		referents = append(referents, claims...)
	}

	return referents, nil
}

// requireFeedUnreferenced rejects removal of a feed a consumer still depends
// on. Removal is recoverable but expensive: a re-added feed waits out the
// activation delay and then re-warms freshness, and for the feed pricing the
// protocol reference that stalls conversion chain-wide meanwhile.
//
// It also rejects removal of a feed that does not exist, inherited from the
// collector. ScheduleFeedTransition would treat that as a no-op, which passes
// a governance proposal that silently does nothing; a proposal naming a typo
// or an already-removed feed should fail where it can be seen. Re-submitting
// while a removal is still in flight stays idempotent — that feed is in phase
// Removing, so it exists here and no-ops at scheduling as before.
func (k Keeper) requireFeedUnreferenced(ctx context.Context, denom string) error {
	referents, err := k.FeedReferents(ctx, denom)
	if err != nil {
		return err
	}
	if len(referents) == 0 {
		return nil
	}

	claims := make([]string, len(referents))
	for i, referent := range referents {
		claims[i] = fmt.Sprintf("%s: %s", referent.Consumer, referent.Referent)
	}

	return sdkerrors.Wrapf(
		types.ErrFeedReferenced,
		"%s: %s",
		denom,
		strings.Join(claims, "; "),
	)
}
