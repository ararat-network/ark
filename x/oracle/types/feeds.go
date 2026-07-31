package types

import (
	"fmt"
	"slices"

	chain "ark/pkg/chain"
)

// FeedSet is the feed epoch selected for one vote-extension height.
type FeedSet struct {
	Version uint64
	Denoms  []string
}

// FeedPhase identifies a feed's relationship to the active set and its own
// scheduled transition.
type FeedPhase uint8

const (
	FeedPhaseOff FeedPhase = iota
	FeedPhaseAdding
	FeedPhaseActive
	FeedPhaseRemoving
)

// NewFeeds returns initial canonical feed state for denoms.
func NewFeeds(denoms []string) Feeds {
	denoms = slices.Clone(denoms)
	slices.Sort(denoms)
	return Feeds{
		Denoms:  denoms,
		Version: InitialFeedVersion,
	}
}

// AtHeight returns the feed epoch validators must report for voteHeight. It
// folds every transition that has activated by voteHeight into the active set,
// advancing the version once per distinct activation height. Transitions
// activating later are excluded, which is what lets a voter at height V and the
// tally of V agree despite reading different committed states.
func (f Feeds) AtHeight(voteHeight int64) FeedSet {
	// The value receiver is scratch space; the clone keeps the fold off the
	// caller's active set.
	f.Denoms = slices.Clone(f.Denoms)
	// Activation heights are validated positive, so zero is a safe sentinel for
	// "no batch applied yet".
	batchHeight := int64(0)
	for _, transition := range f.Transitions {
		if transition.ActivationVoteHeight > voteHeight {
			break
		}
		if transition.ActivationVoteHeight != batchHeight {
			f.Version++
			batchHeight = transition.ActivationVoteHeight
		}
		f.ApplyTransition(transition)
	}

	return FeedSet{Version: f.Version, Denoms: f.Denoms}
}

// ApplyTransition folds one transition into the active feed set in place,
// keeping it sorted. The receiver's denoms are mutated, so callers holding a
// set they must not disturb clone first. Version advancement stays with the
// caller, since one version covers a whole activation batch. It is exported
// because batch promotion in the keeper applies the same rule.
func (f *Feeds) ApplyTransition(transition FeedTransition) {
	index, found := slices.BinarySearch(f.Denoms, transition.Denom)
	switch {
	case transition.Direction == FeedDirection_FEED_DIRECTION_ADD && !found:
		f.Denoms = slices.Insert(f.Denoms, index, transition.Denom)
	case transition.Direction == FeedDirection_FEED_DIRECTION_REMOVE && found:
		f.Denoms = slices.Delete(f.Denoms, index, index+1)
	}
}

// Phase returns the denom's relationship to the active feed set and its own
// scheduled transition, if it has one. Transitions scheduled for other feeds
// never affect this feed's phase.
func (f Feeds) Phase(denom string) FeedPhase {
	for _, transition := range f.Transitions {
		if transition.Denom != denom {
			continue
		}
		if transition.Direction == FeedDirection_FEED_DIRECTION_ADD {
			return FeedPhaseAdding
		}

		return FeedPhaseRemoving
	}

	if _, active := slices.BinarySearch(f.Denoms, denom); active {
		return FeedPhaseActive
	}

	return FeedPhaseOff
}

// Validate checks active feed and scheduled transition invariants.
func (f Feeds) Validate() error {
	if f.Version == 0 {
		return fmt.Errorf("feed version must be positive")
	}

	if len(f.Denoms) > MaxFeeds {
		return fmt.Errorf(
			"active feed count %d exceeds maximum feeds %d",
			len(f.Denoms),
			MaxFeeds,
		)
	}
	for i, denom := range f.Denoms {
		if i > 0 && denom <= f.Denoms[i-1] {
			return fmt.Errorf("active feeds must be sorted by unique denom")
		}
		if err := chain.ValidatePricedDenom(denom); err != nil {
			return fmt.Errorf("active feed %w", err)
		}
	}

	additions := 0
	scheduled := make(map[string]struct{}, len(f.Transitions))
	for i, transition := range f.Transitions {
		if err := transition.Validate(); err != nil {
			return err
		}
		if i > 0 {
			previous := f.Transitions[i-1]
			if transition.ActivationVoteHeight < previous.ActivationVoteHeight ||
				(transition.ActivationVoteHeight == previous.ActivationVoteHeight &&
					transition.Denom <= previous.Denom) {
				return fmt.Errorf(
					"feed transitions must be sorted by activation height and denom",
				)
			}
		}
		if _, duplicate := scheduled[transition.Denom]; duplicate {
			return fmt.Errorf(
				"feed %s has more than one scheduled feed transition",
				transition.Denom,
			)
		}
		scheduled[transition.Denom] = struct{}{}

		_, active := slices.BinarySearch(f.Denoms, transition.Denom)
		switch transition.Direction {
		case FeedDirection_FEED_DIRECTION_ADD:
			if active {
				return fmt.Errorf(
					"scheduled feed addition %s is already an active feed",
					transition.Denom,
				)
			}
			additions++
		case FeedDirection_FEED_DIRECTION_REMOVE:
			if !active {
				return fmt.Errorf(
					"scheduled feed removal %s is not an active feed",
					transition.Denom,
				)
			}
		}
	}

	// The cap must hold at every future height, not only now, so pending
	// additions count against it.
	if scheduledCount := len(f.Denoms) + additions; scheduledCount > MaxFeeds {
		return fmt.Errorf(
			"scheduled feed count %d exceeds maximum feeds %d",
			scheduledCount,
			MaxFeeds,
		)
	}

	return nil
}

// Validate checks one scheduled transition in isolation.
func (t FeedTransition) Validate() error {
	if err := chain.ValidatePricedDenom(t.Denom); err != nil {
		return fmt.Errorf("feed transition %w", err)
	}
	if t.Direction != FeedDirection_FEED_DIRECTION_ADD &&
		t.Direction != FeedDirection_FEED_DIRECTION_REMOVE {
		return fmt.Errorf(
			"feed transition for %s has an unspecified direction",
			t.Denom,
		)
	}
	if t.ActivationVoteHeight <= 0 {
		return fmt.Errorf(
			"feed transition activation height must be positive: %d",
			t.ActivationVoteHeight,
		)
	}

	return nil
}
