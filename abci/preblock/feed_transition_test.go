package preblock_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	"github.com/ararat-network/ark/abci/preblock"
	abcitestutil "github.com/ararat-network/ark/abci/testutil"
	"github.com/ararat-network/ark/abci/voteextension"
	"github.com/ararat-network/ark/pricefeed/api"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestFeedTransitionAcrossVoteAndFinaliseHeights(t *testing.T) {
	const activationVoteHeight int64 = 12

	keeper := &transitionOracleKeeper{
		params: oracletypes.DefaultParams(),
		feeds: oracletypes.Feeds{
			Denoms:  []string{"ausd"},
			Version: oracletypes.InitialFeedVersion,
			Transitions: []oracletypes.FeedTransition{
				{
					Denom:                "akrw",
					Direction:            oracletypes.FeedDirection_FEED_DIRECTION_ADD,
					ActivationVoteHeight: activationVoteHeight,
				},
			},
		},
	}
	// The sidecar has no price yet for the feed being added, so the builder
	// omits it: an abstention on that feed alone.
	oracleClient := staticPriceFeedClient{prices: map[string][]byte{
		"ausd": abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100)),
	}}
	logger := log.NewTestLogger(t)
	extendVote := voteextension.NewHandler(
		logger,
		oracleClient,
		keeper,
		time.Second,
	).ExtendVoteHandler()

	// The activation-height vote extension is produced before FinalizeBlock at
	// that height. The scheduled transition therefore has to be
	// height-addressable before it is folded into the materialised set.
	oldResponse, err := extendVote(
		abcitestutil.NewSDKContext(activationVoteHeight-1, 1),
		&cometabci.RequestExtendVote{Height: activationVoteHeight - 1},
	)
	require.NoError(t, err)
	oldVoteExtension, err := codec.DecodeVoteExtension(oldResponse.VoteExtension)
	require.NoError(t, err)
	require.Equal(t, oracletypes.InitialFeedVersion, oldVoteExtension.TargetVersion)
	require.Equal(t, []string{"ausd"}, sortedKeys(oldVoteExtension.Rates))

	newResponse, err := extendVote(
		abcitestutil.NewSDKContext(activationVoteHeight, 1),
		&cometabci.RequestExtendVote{Height: activationVoteHeight},
	)
	require.NoError(t, err)
	newVoteExtension, err := codec.DecodeVoteExtension(newResponse.VoteExtension)
	require.NoError(t, err)
	require.Equal(t, oracletypes.InitialFeedVersion+1, newVoteExtension.TargetVersion)
	require.Equal(t, []string{"ausd"}, sortedKeys(newVoteExtension.Rates))

	preBlocker := preblock.NewHandler(keeper).WrappedPreBlocker(managerWith())
	validator := sdk.ConsAddress("validator")

	// FinalizeBlock A aggregates the extension for A-1 against the old epoch,
	// then promotes the due batch.
	oldRequest := finalizeRequest(t, activationVoteHeight, validator, oldResponse.VoteExtension)
	_, err = preBlocker(
		abcitestutil.NewSDKContext(activationVoteHeight, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, oldRequest.DecidedLastCommit)),
		oldRequest,
	)
	require.NoError(t, err)
	require.Equal(t, oracletypes.InitialFeedVersion+1, keeper.feeds.Version)
	require.Empty(t, keeper.feeds.Transitions)

	// FinalizeBlock A+1 aggregates the already-produced extension for A against
	// the new epoch. The newly added feed is absent from the report, which is
	// an abstention on that feed alone.
	newRequest := finalizeRequest(t, activationVoteHeight+1, validator, newResponse.VoteExtension)
	_, err = preBlocker(
		abcitestutil.NewSDKContext(activationVoteHeight+1, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, newRequest.DecidedLastCommit)),
		newRequest,
	)
	require.NoError(t, err)
	// Both blocks function (the sole validator participates), and the omitted
	// just-activated feed is an abstention that does not affect attendance
	// while ausd is still priced.
	require.Equal(t, []bool{true, true}, keeper.attended)
}

// TestFeedTransitionsAtConsecutiveHeights is what the per-feed model makes
// possible and the single-pending epoch could not represent. Each batch is its
// own boundary: voter and tally must agree at every height, and the version
// must advance exactly once per activation height.
func TestFeedTransitionsAtConsecutiveHeights(t *testing.T) {
	const firstActivation int64 = 12

	keeper := &transitionOracleKeeper{
		params: oracletypes.DefaultParams(),
		feeds: oracletypes.Feeds{
			Denoms:  []string{"ausd"},
			Version: oracletypes.InitialFeedVersion,
			Transitions: []oracletypes.FeedTransition{
				{
					Denom:                "akrw",
					Direction:            oracletypes.FeedDirection_FEED_DIRECTION_ADD,
					ActivationVoteHeight: firstActivation,
				},
				{
					Denom:                "ajpy",
					Direction:            oracletypes.FeedDirection_FEED_DIRECTION_ADD,
					ActivationVoteHeight: firstActivation + 1,
				},
			},
		},
	}
	oracleClient := staticPriceFeedClient{prices: map[string][]byte{
		"ajpy": abcitestutil.MustEncodeRate(t, math.LegacyNewDec(150)),
		"akrw": abcitestutil.MustEncodeRate(t, math.LegacyNewDec(1300)),
		"ausd": abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100)),
	}}
	extendVote := voteextension.NewHandler(
		log.NewTestLogger(t),
		oracleClient,
		keeper,
		time.Second,
	).ExtendVoteHandler()

	expected := []struct {
		voteHeight int64
		version    uint64
		denoms     []string
	}{
		{voteHeight: firstActivation - 1, version: oracletypes.InitialFeedVersion, denoms: []string{"ausd"}},
		{voteHeight: firstActivation, version: oracletypes.InitialFeedVersion + 1, denoms: []string{"akrw", "ausd"}},
		{voteHeight: firstActivation + 1, version: oracletypes.InitialFeedVersion + 2, denoms: []string{"ajpy", "akrw", "ausd"}},
	}

	extensions := make([][]byte, len(expected))
	for i, want := range expected {
		response, err := extendVote(
			abcitestutil.NewSDKContext(want.voteHeight, 1),
			&cometabci.RequestExtendVote{Height: want.voteHeight},
		)
		require.NoError(t, err)
		extensions[i] = response.VoteExtension

		voteExtension, err := codec.DecodeVoteExtension(response.VoteExtension)
		require.NoError(t, err)
		require.Equal(t, want.version, voteExtension.TargetVersion)
		require.Equal(t, want.denoms, sortedKeys(voteExtension.Rates))
	}

	preBlocker := preblock.NewHandler(keeper).WrappedPreBlocker(managerWith())
	validator := sdk.ConsAddress("validator")

	// Each finalise block tallies the extension produced for the previous
	// height, so an extension signed against fold(V) is always validated
	// against fold(V) even though the batch due at V is promoted in the very
	// same block.
	for i, want := range expected {
		request := finalizeRequest(t, want.voteHeight+1, validator, extensions[i])
		_, err := preBlocker(
			abcitestutil.NewSDKContext(want.voteHeight+1, 1, sdk.ExecModeFinalize).
				WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, request.DecidedLastCommit)),
			request,
		)
		require.NoError(t, err)
	}

	// Two activation heights passed, so the version advanced exactly twice.
	require.Equal(t, oracletypes.InitialFeedVersion+2, keeper.feeds.Version)
	require.Empty(t, keeper.feeds.Transitions)
	require.Equal(t, []string{"ajpy", "akrw", "ausd"}, keeper.feeds.Denoms)
	require.Equal(t, []bool{true, true, true}, keeper.attended)
}

// TestPreblockConsumesVoteExtensionsBeforePromotingFeeds pins the
// consume-before-promote ordering the correctness argument depends on. If
// promotion ran first, the tally for vote height V would validate extensions
// against a set no validator could have seen, and every report would be
// rejected on a version mismatch exactly at an activation height.
func TestPreblockConsumesVoteExtensionsBeforePromotingFeeds(t *testing.T) {
	const activationVoteHeight int64 = 12

	keeper := &orderRecordingOracleKeeper{
		transitionOracleKeeper: transitionOracleKeeper{
			params: oracletypes.DefaultParams(),
			feeds: oracletypes.Feeds{
				Denoms:  []string{"ausd"},
				Version: oracletypes.InitialFeedVersion,
				Transitions: []oracletypes.FeedTransition{
					{
						Denom:                "akrw",
						Direction:            oracletypes.FeedDirection_FEED_DIRECTION_ADD,
						ActivationVoteHeight: activationVoteHeight,
					},
				},
			},
		},
	}
	oracleClient := staticPriceFeedClient{prices: map[string][]byte{
		"ausd": abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100)),
	}}
	extendVote := voteextension.NewHandler(
		log.NewTestLogger(t),
		oracleClient,
		keeper,
		time.Second,
	).ExtendVoteHandler()

	response, err := extendVote(
		abcitestutil.NewSDKContext(activationVoteHeight-1, 1),
		&cometabci.RequestExtendVote{Height: activationVoteHeight - 1},
	)
	require.NoError(t, err)

	// ExtendVote reads feeds too; only the preblock's calls are under test.
	keeper.calls = nil

	preBlocker := preblock.NewHandler(keeper).WrappedPreBlocker(managerWith())
	request := finalizeRequest(t, activationVoteHeight, sdk.ConsAddress("validator"), response.VoteExtension)
	_, err = preBlocker(
		abcitestutil.NewSDKContext(activationVoteHeight, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, request.DecidedLastCommit)),
		request,
	)
	require.NoError(t, err)

	require.Equal(t, []string{"GetFeeds", "AdvanceFeeds"}, keeper.calls)
	// The tally read the pre-promotion fold even though this block promoted the
	// batch due at the tallied height.
	require.Equal(t, oracletypes.InitialFeedVersion, keeper.observedTallyVersion)
	require.Equal(t, oracletypes.InitialFeedVersion+1, keeper.feeds.Version)
}

type staticPriceFeedClient struct {
	prices map[string][]byte
}

func (c staticPriceFeedClient) Prices(
	context.Context,
	*api.PricesRequest,
	...grpc.CallOption,
) (*api.PricesResponse, error) {
	return &api.PricesResponse{Prices: c.prices}, nil
}

type transitionOracleKeeper struct {
	params   oracletypes.Params
	feeds    oracletypes.Feeds
	attended []bool
}

func (k *transitionOracleKeeper) GetFeeds(
	_ context.Context,
	voteHeight int64,
) (oracletypes.FeedSet, error) {
	return k.feeds.AtHeight(voteHeight), nil
}

func (k *transitionOracleKeeper) GetParams(context.Context) (oracletypes.Params, error) {
	return k.params, nil
}

func (k *transitionOracleKeeper) SetExchangeRateWithEvent(context.Context, oracletypes.ExchangeRate) error {
	return nil
}

// RecordVoteAccounting records the attendance credit the keeper would apply:
// participation only counts on a functioning block.
func (k *transitionOracleKeeper) RecordVoteAccounting(
	_ context.Context,
	_ sdk.ConsAddress,
	_ math.Int,
	eligible bool,
	participated bool,
) error {
	k.attended = append(k.attended, eligible && participated)
	return nil
}

// AdvanceFeeds mirrors the keeper's batch promotion: every batch whose
// activation height has arrived folds in, advancing the version once per batch.
func (k *transitionOracleKeeper) AdvanceFeeds(ctx context.Context) error {
	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	for len(k.feeds.Transitions) > 0 &&
		k.feeds.Transitions[0].ActivationVoteHeight <= blockHeight {
		batchHeight := k.feeds.Transitions[0].ActivationVoteHeight
		promoted := oracletypes.Feeds{
			Denoms:  slices.Clone(k.feeds.Denoms),
			Version: k.feeds.Version + 1,
		}
		remaining := k.feeds.Transitions
		for len(remaining) > 0 && remaining[0].ActivationVoteHeight == batchHeight {
			promoted.ApplyTransition(remaining[0])
			remaining = remaining[1:]
		}
		promoted.Transitions = slices.Clone(remaining)
		k.feeds = promoted
	}

	return nil
}

// orderRecordingOracleKeeper records the sequence of feed calls the preblock
// makes, plus the feed version the tally actually observed.
type orderRecordingOracleKeeper struct {
	transitionOracleKeeper

	calls                []string
	observedTallyVersion uint64
}

func (k *orderRecordingOracleKeeper) GetFeeds(
	ctx context.Context,
	voteHeight int64,
) (oracletypes.FeedSet, error) {
	feeds, err := k.transitionOracleKeeper.GetFeeds(ctx, voteHeight)
	if err != nil {
		return oracletypes.FeedSet{}, err
	}
	if len(k.calls) == 0 {
		k.observedTallyVersion = feeds.Version
	}
	k.calls = append(k.calls, "GetFeeds")

	return feeds, nil
}

func (k *orderRecordingOracleKeeper) AdvanceFeeds(ctx context.Context) error {
	k.calls = append(k.calls, "AdvanceFeeds")

	return k.transitionOracleKeeper.AdvanceFeeds(ctx)
}

func finalizeRequest(
	t *testing.T,
	height int64,
	validator sdk.ConsAddress,
	voteExtension []byte,
) *cometabci.RequestFinalizeBlock {
	t.Helper()

	commit, err := codec.EncodeExtendedCommit(cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{
			abcitestutil.NewCommitExtendedVoteInfo(validator, 1, voteExtension),
		},
	})
	require.NoError(t, err)

	return &cometabci.RequestFinalizeBlock{
		Height:            height,
		Txs:               [][]byte{commit},
		DecidedLastCommit: cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, 1)},
	}
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
