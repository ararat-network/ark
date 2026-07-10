package proposals_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/proposals"
	abcitestutil "ark/abci/testutil"
	"ark/abci/ve"
)

func TestPrepareProposalHandler(t *testing.T) {
	appTx1 := []byte("tx1")
	appTx2 := []byte("tx2")
	commitBz := []byte("commit")
	validVote := abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("ve1"))

	testCases := []struct {
		name                 string
		ctx                  sdk.Context
		req                  *cometabci.RequestPrepareProposal
		setup                func(*testing.T, *abcitestutil.MockVoteExtensionCodec, *abcitestutil.MockExtendedCommitCodec)
		prepare              sdk.PrepareProposalHandler
		opts                 []proposals.Option
		expectErr            bool
		expectNilResponse    bool
		expectedTxs          [][]byte
		expectedPrepareTxs   [][]byte
		expectedMaxTxBytes   int64
		expectedNilRequest   bool
		expectedWrappedCalls int
	}{
		{
			name: "nil request returns error",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req:  nil,
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:          true,
			expectNilResponse:  true,
			expectedNilRequest: true,
		},
		{
			name: "vote extensions disabled passes app txs through",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cometabci.RequestPrepareProposal{
				Height:     1,
				Txs:        [][]byte{appTx1, appTx2},
				MaxTxBytes: 100,
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   100,
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled injects encoded commit info",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req: &cometabci.RequestPrepareProposal{
				Height: 3,
				LocalLastCommit: cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{validVote},
				},
				Txs:        [][]byte{appTx1, appTx2},
				MaxTxBytes: 100,
			},
			setup: func(_ *testing.T, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				veCodec.EXPECT().Decode(validVote.VoteExtension).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
				extCommitCodec.EXPECT().Encode(cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{validVote},
				}).Return(commitBz, nil)
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{commitBz, appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   94,
			expectedWrappedCalls: 1,
		},
		{
			name: "retain option passes injected commit info to wrapped handler",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req: &cometabci.RequestPrepareProposal{
				Height: 3,
				LocalLastCommit: cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{validVote},
				},
				Txs:        [][]byte{appTx1},
				MaxTxBytes: 100,
			},
			setup: func(_ *testing.T, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				veCodec.EXPECT().Decode(validVote.VoteExtension).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
				extCommitCodec.EXPECT().Encode(cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{validVote},
				}).Return(commitBz, nil)
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			opts:                 []proposals.Option{proposals.RetainOracleDataInWrappedProposalHandler()},
			expectedTxs:          [][]byte{commitBz, appTx1},
			expectedPrepareTxs:   [][]byte{commitBz, appTx1},
			expectedMaxTxBytes:   94,
			expectedWrappedCalls: 1,
		},
		{
			name: "encoded commit info larger than max tx bytes returns error",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req: &cometabci.RequestPrepareProposal{
				Height: 3,
				LocalLastCommit: cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{validVote},
				},
				Txs:        [][]byte{appTx1},
				MaxTxBytes: 5,
			},
			setup: func(_ *testing.T, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				veCodec.EXPECT().Decode(validVote.VoteExtension).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
				extCommitCodec.EXPECT().Encode(cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{validVote},
				}).Return(commitBz, nil)
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:   true,
			expectedTxs: [][]byte{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			extCommitCodec := abcitestutil.NewMockExtendedCommitCodec(ctrl)
			if tc.setup != nil {
				tc.setup(t, veCodec, extCommitCodec)
			}

			wrappedCalls := 0
			prepare := sdk.PrepareProposalHandler(func(ctx sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				wrappedCalls++
				if tc.expectedPrepareTxs != nil {
					require.Equal(t, tc.expectedPrepareTxs, req.Txs)
				}
				if tc.expectedMaxTxBytes != 0 {
					require.Equal(t, tc.expectedMaxTxBytes, req.MaxTxBytes)
				}
				return tc.prepare(ctx, req)
			})

			handler := proposals.NewHandler(
				log.NewTestLogger(t),
				prepare,
				acceptProcessProposal,
				ve.NoOpValidateVoteExtensions,
				veCodec,
				extCommitCodec,
				tc.opts...,
			)

			resp, err := handler.PrepareProposalHandler()(tc.ctx, tc.req)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.expectNilResponse {
				require.Nil(t, resp)
			} else {
				require.NotNil(t, resp)
				require.Equal(t, tc.expectedTxs, resp.Txs)
			}
			require.Equal(t, tc.expectedWrappedCalls, wrappedCalls)
		})
	}
}

func TestProcessProposalHandler(t *testing.T) {
	appTx := []byte("tx")
	commitBz := []byte("commit")
	commitInfo := cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{
			abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("ve1")),
		},
	}

	testCases := []struct {
		name                 string
		ctx                  sdk.Context
		req                  *cometabci.RequestProcessProposal
		setup                func(*testing.T, *abcitestutil.MockVoteExtensionCodec, *abcitestutil.MockExtendedCommitCodec)
		process              sdk.ProcessProposalHandler
		validate             ve.ValidateVoteExtensionsFn
		opts                 []proposals.Option
		expectErr            bool
		expectNilResponse    bool
		expectedStatus       cometabci.ResponseProcessProposal_ProposalStatus
		expectedProcessTxs   [][]byte
		expectedFinalTxs     [][]byte
		expectedWrappedCalls int
	}{
		{
			name:              "nil request returns error",
			ctx:               abcitestutil.NewSDKContext(3, 2),
			req:               nil,
			process:           rejectUnexpectedProcessProposal(t),
			validate:          ve.NoOpValidateVoteExtensions,
			expectErr:         true,
			expectNilResponse: true,
		},
		{
			name: "vote extensions disabled passes app txs through",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cometabci.RequestProcessProposal{
				Height: 1,
				Txs:    [][]byte{appTx},
			},
			process: func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
			},
			validate:             ve.NoOpValidateVoteExtensions,
			expectedStatus:       cometabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled rejects missing injected commit info",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req: &cometabci.RequestProcessProposal{
				Height: 3,
				Txs:    nil,
			},
			process:        rejectUnexpectedProcessProposal(t),
			validate:       ve.NoOpValidateVoteExtensions,
			expectErr:      true,
			expectedStatus: cometabci.ResponseProcessProposal_REJECT,
		},
		{
			name: "vote extensions enabled removes injected commit info before wrapped handler and restores it after",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req: &cometabci.RequestProcessProposal{
				Height: 3,
				Txs:    [][]byte{commitBz, appTx},
			},
			setup: func(_ *testing.T, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				extCommitCodec.EXPECT().Decode(commitBz).Return(commitInfo, nil)
				veCodec.EXPECT().Decode([]byte("ve1")).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
			},
			process: func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
			},
			validate:             ve.NoOpValidateVoteExtensions,
			expectedStatus:       cometabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{commitBz, appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "retain option keeps injected commit info visible to wrapped handler",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req: &cometabci.RequestProcessProposal{
				Height: 3,
				Txs:    [][]byte{commitBz, appTx},
			},
			setup: func(_ *testing.T, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				extCommitCodec.EXPECT().Decode(commitBz).Return(commitInfo, nil)
				veCodec.EXPECT().Decode([]byte("ve1")).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
			},
			process: func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
			},
			validate:             ve.NoOpValidateVoteExtensions,
			opts:                 []proposals.Option{proposals.RetainOracleDataInWrappedProposalHandler()},
			expectedStatus:       cometabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{commitBz, appTx},
			expectedFinalTxs:     [][]byte{commitBz, appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "validation failure rejects proposal before wrapped handler",
			ctx:  abcitestutil.NewSDKContext(3, 2),
			req: &cometabci.RequestProcessProposal{
				Height: 3,
				Txs:    [][]byte{commitBz, appTx},
			},
			setup: func(_ *testing.T, _ *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				extCommitCodec.EXPECT().Decode(commitBz).Return(commitInfo, nil)
			},
			process: rejectUnexpectedProcessProposal(t),
			validate: func(sdk.Context, cometabci.ExtendedCommitInfo) error {
				return errors.New("invalid commit")
			},
			expectErr:      true,
			expectedStatus: cometabci.ResponseProcessProposal_REJECT,
			expectedFinalTxs: [][]byte{
				commitBz,
				appTx,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			extCommitCodec := abcitestutil.NewMockExtendedCommitCodec(ctrl)
			if tc.setup != nil {
				tc.setup(t, veCodec, extCommitCodec)
			}

			wrappedCalls := 0
			process := sdk.ProcessProposalHandler(func(ctx sdk.Context, req *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				wrappedCalls++
				if tc.expectedProcessTxs != nil {
					require.Equal(t, tc.expectedProcessTxs, req.Txs)
				}
				return tc.process(ctx, req)
			})
			handler := proposals.NewHandler(
				log.NewTestLogger(t),
				passThroughPrepareProposal,
				process,
				tc.validate,
				veCodec,
				extCommitCodec,
				tc.opts...,
			)

			resp, err := handler.ProcessProposalHandler()(tc.ctx, tc.req)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.expectNilResponse {
				require.Nil(t, resp)
			} else {
				require.NotNil(t, resp)
				require.Equal(t, tc.expectedStatus, resp.Status)
			}
			if tc.req != nil && tc.expectedFinalTxs != nil {
				require.Equal(t, tc.expectedFinalTxs, tc.req.Txs)
			}
			require.Equal(t, tc.expectedWrappedCalls, wrappedCalls)
		})
	}
}

func passThroughPrepareProposal(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
	return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
}

func acceptProcessProposal(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
	return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
}

func rejectUnexpectedProcessProposal(t *testing.T) sdk.ProcessProposalHandler {
	t.Helper()

	return func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
		require.Fail(t, "wrapped process handler should not be called")
		return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_REJECT}, nil
	}
}
