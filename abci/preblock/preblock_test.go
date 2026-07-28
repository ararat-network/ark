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
	keeper.EXPECT().AdvanceVoteTargets(gomock.Any()).Return(nil)
	handler := preblock.NewHandler(
		keeper,
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

func TestWrappedPreBlockerWrapsAdvanceVoteTargetsError(t *testing.T) {
	ctrl := gomock.NewController(t)
	advanceErr := errors.New("advance failed")
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	keeper.EXPECT().AdvanceVoteTargets(gomock.Any()).Return(advanceErr)
	handler := preblock.NewHandler(
		keeper,
		codec.NewVoteExtensionCodec(),
	)

	_, err := handler.WrappedPreBlocker(managerWith())(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.ErrorIs(t, err, arkabcitypes.ErrOracleKeeper)
	require.ErrorIs(t, err, advanceErr)
	require.Contains(t, err.Error(), "advance vote targets for height 1")
}

func TestWrappedPreBlockerAppliesPricesAndAdvancesVoteTargetsWhenVoteExtensionsEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := oracletypes.VoteTargetSet{
		Version: oracletypes.InitialVoteTargetVersion,
		Denoms:  []string{"ausd"},
	}
	handler := preblock.NewHandler(
		keeper,
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
	keeper.EXPECT().GetVoteTargets(gomock.Any(), int64(100)).Return(voteTargets, nil)
	keeper.EXPECT().
		SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
			require.Equal(t, "ausd", exchangeRate.Denom)
			require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
			return nil
		})
	keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(1), true, true).Return(nil)
	keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.NewInt(1), true, true).Return(nil)
	keeper.EXPECT().AdvanceVoteTargets(gomock.Any()).Return(nil)

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
