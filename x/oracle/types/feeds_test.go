package types_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func TestValidatePricedDenom(t *testing.T) {
	tests := []struct {
		name      string
		denom     string
		expectErr string
	}{
		{
			name:  "launch denom",
			denom: "ausd",
		},
		{
			name:  "digits after the prefix",
			denom: "abrent2026",
		},
		{
			name:  "longest permitted key",
			denom: "abcdefghijklmnop",
		},
		{
			name:      "empty",
			denom:     "",
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			// A feed key is also a denomination, so it must clear the SDK's
			// three-character minimum: rates travel as DecCoins.
			name:      "shorter than the SDK denomination minimum",
			denom:     "au",
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			// The prefix is what makes a key a denomination the chain could
			// carry, so a bare symbol is not a feed key.
			name:      "no native prefix",
			denom:     "usd",
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name:      "leading digit",
			denom:     "1usd",
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name:      "uppercase",
			denom:     "aUSD",
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name:      "punctuation",
			denom:     "ausd/eur",
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name:      "seventeen characters",
			denom:     "abcdefghijklmnopq",
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			// The numeraire is the unit every rate is quoted against, so it
			// never has a feed of its own.
			name:      "reserved numeraire",
			denom:     chain.NoahBaseDenom,
			expectErr: "is the numeraire and is never priced",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.ValidatePricedDenom(tt.denom)
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}

func TestNewFeedsCanonicalisesFeedIDs(t *testing.T) {
	input := []string{"ausd", "agold"}

	feeds := types.NewFeeds(input)
	input[0] = "mutated"

	require.Equal(t, uint64(1), feeds.Version)
	require.Equal(t, []string{"agold", "ausd"}, feeds.Denoms)
}

func TestFeedsAtHeight(t *testing.T) {
	add := types.FeedDirection_FEED_DIRECTION_ADD
	remove := types.FeedDirection_FEED_DIRECTION_REMOVE

	tests := []struct {
		name        string
		transitions []types.FeedTransition
		voteHeight  int64
		wantVersion uint64
		wantFeedIDs []string
	}{
		{
			name:        "no transitions",
			voteHeight:  100,
			wantVersion: 4,
			wantFeedIDs: []string{"agold", "ausd"},
		},
		{
			name: "before activation",
			transitions: []types.FeedTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
			},
			voteHeight:  19,
			wantVersion: 4,
			wantFeedIDs: []string{"agold", "ausd"},
		},
		{
			name: "at activation",
			transitions: []types.FeedTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantFeedIDs: []string{"agold", "asilver", "ausd"},
		},
		{
			name: "removal at activation",
			transitions: []types.FeedTransition{
				{Denom: "agold", Direction: remove, ActivationVoteHeight: 20},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantFeedIDs: []string{"ausd"},
		},
		{
			name: "two records one batch bump version once",
			transitions: []types.FeedTransition{
				{Denom: "agold", Direction: remove, ActivationVoteHeight: 20},
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantFeedIDs: []string{"asilver", "ausd"},
		},
		{
			name: "consecutive heights bump version twice",
			transitions: []types.FeedTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
				{Denom: "azinc", Direction: add, ActivationVoteHeight: 21},
			},
			voteHeight:  21,
			wantVersion: 6,
			wantFeedIDs: []string{"agold", "asilver", "ausd", "azinc"},
		},
		{
			name: "later batch excluded at earlier height",
			transitions: []types.FeedTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
				{Denom: "azinc", Direction: add, ActivationVoteHeight: 21},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantFeedIDs: []string{"agold", "asilver", "ausd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feeds := types.Feeds{
				Denoms:      []string{"agold", "ausd"},
				Version:     4,
				Transitions: tt.transitions,
			}

			got := feeds.AtHeight(tt.voteHeight)

			require.Equal(t, tt.wantVersion, got.Version)
			require.Equal(t, tt.wantFeedIDs, got.Denoms)
		})
	}
}

func TestFeedsAtHeightDoesNotAliasState(t *testing.T) {
	feeds := types.Feeds{
		Denoms:  []string{"agold"},
		Version: 4,
		Transitions: []types.FeedTransition{
			{
				Denom:                "asilver",
				Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
				ActivationVoteHeight: 20,
			},
		},
	}

	before := feeds.AtHeight(19)
	at := feeds.AtHeight(20)
	before.Denoms[0] = "mutated"
	at.Denoms[0] = "mutated"

	require.Equal(t, []string{"agold"}, feeds.Denoms)
	require.Equal(t, "asilver", feeds.Transitions[0].Denom)
}

func TestFeedsPhase(t *testing.T) {
	feeds := types.Feeds{
		Denoms:  []string{"active", "removing"},
		Version: 1,
		Transitions: []types.FeedTransition{
			{
				Denom:                "adding",
				Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
				ActivationVoteHeight: 10,
			},
			{
				Denom:                "removing",
				Direction:            types.FeedDirection_FEED_DIRECTION_REMOVE,
				ActivationVoteHeight: 10,
			},
		},
	}
	tests := []struct {
		name  string
		denom string
		phase types.FeedPhase
	}{
		{
			name:  "off",
			denom: "off",
			phase: types.FeedPhaseOff,
		},
		{
			name:  "adding",
			denom: "adding",
			phase: types.FeedPhaseAdding,
		},
		{
			name:  "active while other feeds transition",
			denom: "active",
			phase: types.FeedPhaseActive,
		},
		{
			name:  "removing",
			denom: "removing",
			phase: types.FeedPhaseRemoving,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.phase, feeds.Phase(tt.denom))
		})
	}
}

func TestFeedsValidate(t *testing.T) {
	add := types.FeedDirection_FEED_DIRECTION_ADD
	remove := types.FeedDirection_FEED_DIRECTION_REMOVE
	valid := func() types.Feeds {
		return types.Feeds{
			Denoms:  []string{"agold", "ausd"},
			Version: 1,
		}
	}
	tooMany := make([]string, types.MaxFeeds+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("asset%03d", i)
	}

	tests := []struct {
		name      string
		mutate    func(*types.Feeds)
		expectErr string
	}{
		{
			name: "valid active",
		},
		{
			name: "valid empty active",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = nil
			},
		},
		{
			name: "valid denom-shaped feed ids",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = []string{"aeur", "ausd"}
			},
		},
		{
			name: "valid addition",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
				}
			},
		},
		{
			name: "valid same-height batch",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "agold", Direction: remove, ActivationVoteHeight: 10},
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
				}
			},
		},
		{
			name: "valid consecutive heights",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 11},
				}
			},
		},
		{
			name: "zero version",
			mutate: func(feeds *types.Feeds) {
				feeds.Version = 0
			},
			expectErr: "feed version must be positive",
		},
		{
			name: "too many active feeds",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = slices.Clone(tooMany)
			},
			expectErr: "exceeds maximum feeds",
		},
		{
			name: "unsorted active feeds",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = []string{"ausd", "agold"}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "duplicate active feed",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = []string{"agold", "agold"}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "invalid active feed id",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = []string{"aGOLD"}
			},
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name: "reserved active feed id",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = []string{chain.NoahBaseDenom}
			},
			expectErr: "is the numeraire and is never priced",
		},
		{
			name: "invalid transition feed id",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "SILVER", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name: "reserved transition feed id",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: chain.NoahBaseDenom, Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "is the numeraire and is never priced",
		},
		{
			name: "unspecified direction",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "asilver", ActivationVoteHeight: 10},
				}
			},
			expectErr: "unspecified direction",
		},
		{
			name: "non-positive activation height",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "asilver", Direction: add},
				}
			},
			expectErr: "activation height must be positive",
		},
		{
			name: "duplicate feed transitions",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
					{Denom: "asilver", Direction: remove, ActivationVoteHeight: 11},
				}
			},
			expectErr: "more than one scheduled feed transition",
		},
		{
			name: "unsorted transitions by height",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 11},
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "must be sorted by activation height and denom",
		},
		{
			name: "unsorted transitions by feed id within a batch",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 10},
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "must be sorted by activation height and denom",
		},
		{
			name: "addition of an active feed",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "agold", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "is already an active feed",
		},
		{
			name: "removal of an absent feed",
			mutate: func(feeds *types.Feeds) {
				feeds.Transitions = []types.FeedTransition{
					{Denom: "asilver", Direction: remove, ActivationVoteHeight: 10},
				}
			},
			expectErr: "is not an active feed",
		},
		{
			name: "additions exceed the feed cap",
			mutate: func(feeds *types.Feeds) {
				feeds.Denoms = slices.Clone(tooMany[:types.MaxFeeds])
				feeds.Transitions = []types.FeedTransition{
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "exceeds maximum feeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feeds := valid()
			if tt.mutate != nil {
				tt.mutate(&feeds)
			}

			err := feeds.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}
