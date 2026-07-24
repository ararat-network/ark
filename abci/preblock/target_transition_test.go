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

	"ark/abci/codec"
	"ark/abci/preblock"
	abcitestutil "ark/abci/testutil"
	"ark/abci/voteextension"
	transporttypes "ark/oracle/types"
	arkencoding "ark/pkg/encoding"
	oracletypes "ark/x/oracle/types"
)

func TestVoteTargetTransitionAcrossVoteAndFinaliseHeights(t *testing.T) {
	const activationVoteHeight int64 = 12

	keeper := &transitionOracleKeeper{
		params: oracletypes.DefaultParams(),
		targets: oracletypes.VoteTargets{
			Denoms:  []string{"uusd"},
			Version: oracletypes.InitialVoteTargetVersion,
			Pending: &oracletypes.PendingVoteTargets{
				Denoms:               []string{"ukrw", "uusd"},
				Version:              oracletypes.InitialVoteTargetVersion + 1,
				ActivationVoteHeight: activationVoteHeight,
			},
		},
	}
	oracleClient := staticOracleClient{prices: map[string][]byte{
		"ukrw": abcitestutil.MustEncodeRate(t, math.LegacyZeroDec()),
		"uusd": abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100)),
	}}
	logger := log.NewTestLogger(t)
	voteExtensionCodec := codec.NewVoteExtensionCodec()
	extendVote := voteextension.NewHandler(
		logger,
		oracleClient,
		keeper,
		voteExtensionCodec,
		time.Second,
	).ExtendVoteHandler()

	// The activation-height vote extension is produced before FinalizeBlock at
	// that height. The pending epoch therefore has to be height-addressable
	// before it is promoted to active state.
	oldResponse, err := extendVote(
		abcitestutil.NewSDKContext(activationVoteHeight-1, 1),
		&cometabci.RequestExtendVote{Height: activationVoteHeight - 1},
	)
	require.NoError(t, err)
	oldVoteExtension, err := voteExtensionCodec.Decode(oldResponse.VoteExtension)
	require.NoError(t, err)
	require.Equal(t, oracletypes.InitialVoteTargetVersion, oldVoteExtension.TargetVersion)
	require.Equal(t, []string{"uusd"}, sortedKeys(oldVoteExtension.Rates))

	newResponse, err := extendVote(
		abcitestutil.NewSDKContext(activationVoteHeight, 1),
		&cometabci.RequestExtendVote{Height: activationVoteHeight},
	)
	require.NoError(t, err)
	newVoteExtension, err := voteExtensionCodec.Decode(newResponse.VoteExtension)
	require.NoError(t, err)
	require.Equal(t, oracletypes.InitialVoteTargetVersion+1, newVoteExtension.TargetVersion)
	require.Equal(t, []string{"ukrw", "uusd"}, sortedKeys(newVoteExtension.Rates))
	krwRate, err := arkencoding.DecodeLegacyDec(newVoteExtension.Rates["ukrw"])
	require.NoError(t, err)
	require.True(t, krwRate.IsZero())

	preBlocker := preblock.NewHandler(
		keeper,
		voteExtensionCodec,
	).WrappedPreBlocker(managerWith())
	validator := sdk.ConsAddress("validator")

	// FinalizeBlock A aggregates the extension for A-1 against the old epoch,
	// then promotes the pending epoch.
	oldRequest := finalizeRequest(t, activationVoteHeight, validator, oldResponse.VoteExtension)
	_, err = preBlocker(
		abcitestutil.NewSDKContext(activationVoteHeight, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, oldRequest.DecidedLastCommit)),
		oldRequest,
	)
	require.NoError(t, err)
	require.Equal(t, oracletypes.InitialVoteTargetVersion+1, keeper.targets.Version)
	require.Nil(t, keeper.targets.Pending)

	// FinalizeBlock A+1 aggregates the already-produced extension for A against
	// the new epoch. The newly added target is carried as an explicit zero and is
	// therefore accountable as a non-positive report rather than silently
	// dropped during the target transition.
	newRequest := finalizeRequest(t, activationVoteHeight+1, validator, newResponse.VoteExtension)
	_, err = preBlocker(
		abcitestutil.NewSDKContext(activationVoteHeight+1, 1, sdk.ExecModeFinalize).
			WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, newRequest.DecidedLastCommit)),
		newRequest,
	)
	require.NoError(t, err)
	require.Equal(t, []bool{false, true}, keeper.missed)
}

type staticOracleClient struct {
	prices map[string][]byte
}

func (c staticOracleClient) Prices(
	context.Context,
	*transporttypes.OraclePricesRequest,
	...grpc.CallOption,
) (*transporttypes.OraclePricesResponse, error) {
	return &transporttypes.OraclePricesResponse{Prices: c.prices}, nil
}

type transitionOracleKeeper struct {
	params  oracletypes.Params
	targets oracletypes.VoteTargets
	missed  []bool
}

func (k *transitionOracleKeeper) GetVoteTargets(
	_ context.Context,
	voteHeight int64,
) (oracletypes.VoteTargetSet, error) {
	return k.targets.AtHeight(voteHeight), nil
}

func (k *transitionOracleKeeper) GetParams(context.Context) (oracletypes.Params, error) {
	return k.params, nil
}

func (k *transitionOracleKeeper) SetExchangeRateWithEvent(context.Context, oracletypes.ExchangeRate) error {
	return nil
}

func (k *transitionOracleKeeper) RecordVoteAccounting(
	_ context.Context,
	_ sdk.ConsAddress,
	_ math.Int,
	missed bool,
) error {
	k.missed = append(k.missed, missed)
	return nil
}

func (k *transitionOracleKeeper) AdvanceVoteTargets(ctx context.Context) error {
	targets := k.targets
	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if targets.Pending == nil || blockHeight < targets.Pending.ActivationVoteHeight {
		return nil
	}
	targets.Denoms = slices.Clone(targets.Pending.Denoms)
	targets.Version = targets.Pending.Version
	targets.Pending = nil
	k.targets = targets
	return nil
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
