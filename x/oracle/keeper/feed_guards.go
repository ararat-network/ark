package keeper

import (
	"context"
	"fmt"
	"strings"

	errorsmod "cosmossdk.io/errors"

	"github.com/ararat-network/ark/x/oracle/types"
)

// FeedReferentGuard derives a consumer's removal-blocking claims from its authoritative state at
// query time, without a mirrored dependency index.
type FeedReferentGuard interface {
	// FeedReferents returns every claim the consumer holds on denom, or an
	// empty slice when it holds none. A consumer with several reasons reports
	// all of them: governance sees every blocker at once rather than
	// discovering them one rejected proposal at a time.
	FeedReferents(ctx context.Context, denom string) ([]types.FeedReferent, error)
}

// SetFeedReferentGuards installs the app-owned foreign-consumer guard set, one entry per consumer.
// Oracle checks its own reference directly; post-construction wiring avoids reverse dependency
// imports.
func (k *Keeper) SetFeedReferentGuards(guards ...FeedReferentGuard) {
	k.feedReferentGuards = guards
}

// feedReferents serves both removal validation and operator queries. Active and in-flight feeds
// exist; absent feeds return ErrFeedNotFound. Oracle's reference claim precedes foreign-consumer
// claims.
func (k Keeper) feedReferents(ctx context.Context, denom string) ([]types.FeedReferent, error) {
	phase, err := k.FeedPhase(ctx, denom)
	if err != nil {
		return nil, err
	}
	if phase == types.FeedPhaseOff {
		return nil, errorsmod.Wrap(types.ErrFeedNotFound, denom)
	}

	var referents []types.FeedReferent
	referenceDenom, err := k.GetReferenceDenom(ctx)
	if err != nil {
		return nil, err
	}
	if referenceDenom == denom {
		referents = append(referents, types.FeedReferent{
			Consumer: types.ModuleName,
			Referent: "protocol reference denom",
		})
	}

	for _, guard := range k.feedReferentGuards {
		claims, err := guard.FeedReferents(ctx, denom)
		if err != nil {
			return nil, fmt.Errorf("checking feed %s referents: %w", denom, err)
		}
		referents = append(referents, claims...)
	}

	return referents, nil
}

// requireFeedUnreferenced rejects absent feeds and any consumer claim. Repeated removal requests
// remain idempotent while removal is in flight, but typos and already-removed feeds fail visibly.
func (k Keeper) requireFeedUnreferenced(ctx context.Context, denom string) error {
	referents, err := k.feedReferents(ctx, denom)
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

	return errorsmod.Wrapf(
		types.ErrFeedReferenced,
		"%s: %s",
		denom,
		strings.Join(claims, "; "),
	)
}

// FeedReferents returns every standing claim on a feed. A feed with none may
// be removed; one with any may not, which is what RemoveFeed enforces.
func (k Keeper) FeedReferents(ctx context.Context, denom string) ([]types.FeedReferent, error) {
	return k.feedReferents(ctx, denom)
}
