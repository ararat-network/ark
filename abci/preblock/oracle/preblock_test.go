package oracle_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	preblock "noah/abci/preblock/oracle"
	abcitestutil "noah/abci/testutil"
	oracletypes "noah/x/oracle/types"
)

func TestWrappedPreBlockerRejectsNilRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	fake := &fakeModule{name: "fake"}
	handler := preblock.NewOraclePreBlockHandler(
		log.NewTestLogger(t),
		abcitestutil.NewMockOracleKeeper(ctrl),
		abcitestutil.NewMockVoteExtensionCodec(ctrl),
		abcitestutil.NewMockExtendedCommitCodec(ctrl),
	)

	_, err := handler.WrappedPreBlocker(managerWith(fake))(abcitestutil.NewSDKContext(3, 2, sdk.ExecModeFinalize), nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "received nil RequestFinalizeBlock")
	require.Zero(t, fake.called)
}

func TestWrappedPreBlockerCallsModuleManagerWhenVoteExtensionsDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	fake := &fakeModule{
		name:     "fake",
		response: preBlockResponse{consensusParamsChanged: true},
	}
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	handler := preblock.NewOraclePreBlockHandler(
		log.NewTestLogger(t),
		keeper,
		abcitestutil.NewMockVoteExtensionCodec(ctrl),
		abcitestutil.NewMockExtendedCommitCodec(ctrl),
	)

	res, err := handler.WrappedPreBlocker(managerWith(fake))(
		abcitestutil.NewSDKContext(1, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{Height: 1},
	)

	require.NoError(t, err)
	require.True(t, res.IsConsensusParamsChanged())
	require.Equal(t, 1, fake.called)
}

func TestWrappedPreBlockerAppliesPricesAndSyncsTobinTaxWhenVoteExtensionsEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
	extCommitCodec := abcitestutil.NewMockExtendedCommitCodec(ctrl)
	keeper := abcitestutil.NewMockOracleKeeper(ctrl)
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
	}
	handler := preblock.NewOraclePreBlockHandler(
		log.NewTestLogger(t),
		keeper,
		veCodec,
		extCommitCodec,
	)
	val1 := sdk.ConsAddress("validator1")
	val2 := sdk.ConsAddress("validator2")
	commitBz := []byte("commit")
	ve1Bz := []byte("ve1")
	ve2Bz := []byte("ve2")
	voteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"uusd": math.LegacyNewDec(100),
	})

	extCommitCodec.EXPECT().Decode(commitBz).Return(cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{
			abcitestutil.NewExtendedVoteInfo(val1, 1, ve1Bz),
			abcitestutil.NewExtendedVoteInfo(val2, 1, ve2Bz),
		},
	}, nil)
	veCodec.EXPECT().Decode(ve1Bz).Return(voteExtension, nil)
	veCodec.EXPECT().Decode(ve2Bz).Return(voteExtension, nil)
	keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
	keeper.EXPECT().GetVoteTargets(gomock.Any()).Return(voteTargets, nil)
	keeper.EXPECT().
		SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
			require.Equal(t, "uusd", exchangeRate.Denom)
			require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
			return nil
		})
	keeper.EXPECT().AddScoreWeight(gomock.Any(), val1, uint64(1)).Return(nil)
	keeper.EXPECT().AddScoreWeight(gomock.Any(), val2, uint64(1)).Return(nil)
	keeper.EXPECT().SyncTobinTax(gomock.Any(), voteTargets).Return(nil)

	_, err := handler.WrappedPreBlocker(managerWith())(
		abcitestutil.NewSDKContext(3, 2, sdk.ExecModeFinalize),
		&cometabci.RequestFinalizeBlock{
			Height: 3,
			Txs:    [][]byte{commitBz},
		},
	)

	require.NoError(t, err)
}

type fakeModule struct {
	name     string
	called   int
	response appmodule.ResponsePreBlock
}

func (f *fakeModule) IsOnePerModuleType() {}

func (f *fakeModule) IsAppModule() {}

func (f *fakeModule) Name() string { return f.name }

func (f *fakeModule) PreBlock(context.Context) (appmodule.ResponsePreBlock, error) {
	f.called++
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
