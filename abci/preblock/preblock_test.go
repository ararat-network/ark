package preblock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"ark/abci/codec"
	"ark/abci/preblock"
	abcitestutil "ark/abci/testutil"
	arkabcitypes "ark/abci/types"
	oracletypes "ark/x/oracle/types"
)

func TestWrappedPreBlockerRejectsNilRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	fake := &fakeModule{name: "fake"}
	handler := preblock.NewHandler(
		abcitestutil.NewMockOracleKeeper(ctrl),
		abcitestutil.NewMockAssetKeeper(ctrl),
		abcitestutil.NewMockTreasuryKeeper(ctrl),
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith(fake))(abcitestutil.NewSDKContext(3, 2, sdk.ExecModeFinalize), nil)

	require.ErrorIs(t, err, arkabcitypes.ErrNilRequest)
	require.Zero(t, fake.called)
}

func TestWrappedPreBlockerWrapsModuleManagerError(t *testing.T) {
	ctrl := gomock.NewController(t)
	moduleErr := errors.New("module preblock failed")
	fake := &fakeModule{name: "fake", err: moduleErr}
	handler := preblock.NewHandler(
		abcitestutil.NewMockOracleKeeper(ctrl),
		abcitestutil.NewMockAssetKeeper(ctrl),
		abcitestutil.NewMockTreasuryKeeper(ctrl),
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith(fake))(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.ErrorIs(t, err, arkabcitypes.ErrWrappedHandler)
	require.ErrorIs(t, err, moduleErr)
}

func TestWrappedPreBlockerSkipsVoteExtensionsWithoutPreviousCommit(t *testing.T) {
	ctrl := gomock.NewController(t)
	fake := &fakeModule{
		name:     "fake",
		response: preBlockResponse{consensusParamsChanged: true},
	}
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	keeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(nil)
	// No CompleteLifecycle expectation: nothing aggregated, so the strict mock
	// asserts the completions hook is skipped entirely.
	assetKeeper := abcitestutil.NewMockAssetKeeper(ctrl)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(nil)
	handler := preblock.NewHandler(
		keeper,
		assetKeeper,
		treasuryKeeper,
		codec.NewVoteExtensionCodec(),
	)

	res, err := handler.WrappedPreBlocker(managerWith(fake))(
		abcitestutil.NewSDKContext(100, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, cometabci.CommitInfo{})),
		&cometabci.RequestFinalizeBlock{Height: 100},
	)

	require.NoError(t, err)
	require.True(t, res.IsConsensusParamsChanged())
	require.Equal(t, 1, fake.called)
}

func TestWrappedPreBlockerWrapsAdvanceFeedsError(t *testing.T) {
	ctrl := gomock.NewController(t)
	advanceErr := errors.New("advance failed")
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	keeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(advanceErr)
	// No PrimeLiabilitySnapshot expectation: the strict mock asserts priming is
	// not reached when vote-target advancement fails.
	handler := preblock.NewHandler(
		keeper,
		abcitestutil.NewMockAssetKeeper(ctrl),
		abcitestutil.NewMockTreasuryKeeper(ctrl),
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith())(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.ErrorIs(t, err, arkabcitypes.ErrOracleKeeper)
	require.ErrorIs(t, err, advanceErr)
	require.Contains(t, err.Error(), "advance feeds for height 1")
}

func TestWrappedPreBlockerAppliesPricesAndAdvancesVoteTargetsWhenVoteExtensionsEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"ausd"},
	}
	assetKeeper := abcitestutil.NewMockAssetKeeper(ctrl)
	assetKeeper.EXPECT().CompleteLifecycle(gomock.Any(), gomock.Any()).Return(nil)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(nil)
	handler := preblock.NewHandler(
		keeper,
		assetKeeper,
		treasuryKeeper,
		codec.NewVoteExtensionCodec(),
	)
	val1 := sdk.ConsAddress("validator1")
	val2 := sdk.ConsAddress("validator2")
	voteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"ausd": math.LegacyNewDec(100),
	})
	ve1Bz := abcitestutil.MustEncodeVoteExtension(t, voteExtension)
	ve2Bz := abcitestutil.MustEncodeVoteExtension(t, voteExtension)
	commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{
			abcitestutil.NewExtendedVoteInfo(val1, 1, ve1Bz),
			abcitestutil.NewExtendedVoteInfo(val2, 1, ve2Bz),
		},
	})
	keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
	keeper.EXPECT().GetFeeds(gomock.Any(), int64(100)).Return(voteTargets, nil)
	keeper.EXPECT().
		SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
			require.Equal(t, "ausd", exchangeRate.Denom)
			require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
			return nil
		})
	keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(1), true, true).Return(nil)
	keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.NewInt(1), true, true).Return(nil)
	keeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(nil)

	lastCommit := cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, 2)}
	_, err := handler.WrappedPreBlocker(managerWith())(
		abcitestutil.NewSDKContext(101, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, lastCommit)),
		&cometabci.RequestFinalizeBlock{
			Height:            101,
			Txs:               [][]byte{commitBz},
			DecidedLastCommit: lastCommit,
		},
	)

	require.NoError(t, err)
}

// TestWrappedPreBlockerCompletesLifecycleWithAggregatedRates pins where the
// completions hook sits: after feed promotion, before liability priming, and
// seeing exactly the rates this block aggregated.
func TestWrappedPreBlockerCompletesLifecycleWithAggregatedRates(t *testing.T) {
	ctrl := gomock.NewController(t)
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	assetKeeper := abcitestutil.NewMockAssetKeeper(ctrl)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	gomock.InOrder(
		keeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(nil),
		assetKeeper.EXPECT().
			CompleteLifecycle(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, rates oracletypes.RateSet) error {
				require.Len(t, rates, 1)
				require.True(t, math.LegacyNewDec(100).Equal(rates["ausd"]))
				return nil
			}),
		treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(nil),
	)
	ctx, req := aggregatingBlock(t, keeper)
	handler := preblock.NewHandler(
		keeper,
		assetKeeper,
		treasuryKeeper,
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith())(ctx, req)

	require.NoError(t, err)
}

// TestWrappedPreBlockerSkipsCompletionsWithoutAggregatedRates covers the block
// that aggregates nothing: completions ride on consensus evidence produced in
// this block, so with no evidence there is nothing to consider.
func TestWrappedPreBlockerSkipsCompletionsWithoutAggregatedRates(t *testing.T) {
	ctrl := gomock.NewController(t)
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	keeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(nil)
	// No CompleteLifecycle expectation: the strict mock fails the test if the
	// hook runs at all.
	assetKeeper := abcitestutil.NewMockAssetKeeper(ctrl)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(nil)
	handler := preblock.NewHandler(
		keeper,
		assetKeeper,
		treasuryKeeper,
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith())(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.NoError(t, err)
}

func TestWrappedPreBlockerWrapsCompleteLifecycleError(t *testing.T) {
	ctrl := gomock.NewController(t)
	completionErr := errors.New("completion failed")
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	keeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(nil)
	assetKeeper := abcitestutil.NewMockAssetKeeper(ctrl)
	assetKeeper.EXPECT().CompleteLifecycle(gomock.Any(), gomock.Any()).Return(completionErr)
	// No PrimeLiabilitySnapshot expectation: a failed completion halts the block
	// like every other preblock failure.
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	ctx, req := aggregatingBlock(t, keeper)
	handler := preblock.NewHandler(
		keeper,
		assetKeeper,
		treasuryKeeper,
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith())(ctx, req)

	require.ErrorIs(t, err, arkabcitypes.ErrAssetKeeper)
	require.ErrorIs(t, err, completionErr)
	require.Contains(t, err.Error(), "complete asset lifecycle for height 101")
}

func TestWrappedPreBlockerPrimesTreasuryLiability(t *testing.T) {
	ctrl := gomock.NewController(t)
	oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	gomock.InOrder(
		oracleKeeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(nil),
		treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(nil),
	)
	handler := preblock.NewHandler(
		oracleKeeper,
		abcitestutil.NewMockAssetKeeper(ctrl),
		treasuryKeeper,
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith())(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.NoError(t, err)
}

func TestWrappedPreBlockerWrapsTreasuryPrimeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().AdvanceFeeds(gomock.Any()).Return(nil)
	treasuryKeeper := abcitestutil.NewMockTreasuryKeeper(ctrl)
	primeErr := errors.New("prime failed")
	treasuryKeeper.EXPECT().PrimeLiabilitySnapshot(gomock.Any()).Return(primeErr)
	handler := preblock.NewHandler(
		oracleKeeper,
		abcitestutil.NewMockAssetKeeper(ctrl),
		treasuryKeeper,
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith())(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.ErrorIs(t, err, arkabcitypes.ErrTreasuryKeeper)
	require.ErrorIs(t, err, primeErr)
	require.Contains(t, err.Error(), "prime liability snapshot for height 1")
}

// aggregatingBlock sets up the oracle expectations for a block where two
// validators agree on one rate, and returns the context and request that drive
// it.
func aggregatingBlock(
	t *testing.T,
	keeper *abcitestutil.MockOracleKeeper,
) (sdk.Context, *cometabci.RequestFinalizeBlock) {
	t.Helper()

	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	feeds := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"ausd"},
	}
	voteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"ausd": math.LegacyNewDec(100),
	})
	commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{
			abcitestutil.NewExtendedVoteInfo(
				sdk.ConsAddress("validator1"),
				1,
				abcitestutil.MustEncodeVoteExtension(t, voteExtension),
			),
			abcitestutil.NewExtendedVoteInfo(
				sdk.ConsAddress("validator2"),
				1,
				abcitestutil.MustEncodeVoteExtension(t, voteExtension),
			),
		},
	})
	keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
	keeper.EXPECT().GetFeeds(gomock.Any(), int64(100)).Return(feeds, nil)
	keeper.EXPECT().SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).Return(nil)
	keeper.EXPECT().
		RecordVoteAccounting(gomock.Any(), gomock.Any(), math.NewInt(1), true, true).
		Times(2).
		Return(nil)

	lastCommit := cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, 2)}

	return abcitestutil.NewSDKContext(101, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, lastCommit)),
		&cometabci.RequestFinalizeBlock{
			Height:            101,
			Txs:               [][]byte{commitBz},
			DecidedLastCommit: lastCommit,
		}
}

type fakeModule struct {
	name     string
	called   int
	response appmodule.ResponsePreBlock
	err      error
}

func (f *fakeModule) IsOnePerModuleType() {}

func (f *fakeModule) IsAppModule() {}

func (f *fakeModule) Name() string { return f.name }

func (f *fakeModule) PreBlock(context.Context) (appmodule.ResponsePreBlock, error) {
	f.called++
	if f.err != nil {
		return nil, f.err
	}
	if f.response == nil {
		return preBlockResponse{}, nil
	}
	return f.response, nil
}

type preBlockResponse struct {
	consensusParamsChanged bool
}

func (r preBlockResponse) IsConsensusParamsChanged() bool {
	return r.consensusParamsChanged
}

func managerWith(modules ...*fakeModule) *module.Manager {
	mm := &module.Manager{
		Modules:          map[string]any{},
		OrderPreBlockers: make([]string, 0, len(modules)),
	}
	for _, mod := range modules {
		mm.Modules[mod.Name()] = mod
		mm.OrderPreBlockers = append(mm.OrderPreBlockers, mod.Name())
	}
	return mm
}
